# 分布式探活监控系统 — 设计文档

> 独立项目。工作代号：**bigbrother**。
> 设计目标：真实需求、可演示成果、分布式深度、AI 辅助分析。

---

## 1. 项目定位

一个**分布式可用性监控系统**：从多个网络位置（云节点 + 家庭节点）定时探测一批网站/服务的可用性、响应延迟、SSL 证书状态；并通过 SSH 对自有服务器做健康检查（CPU/内存/磁盘/服务状态）。结果聚合展示在状态页上，异常时触发告警。

**一句话定位：**

> 一个分布式探活监控系统。Coordinator 调度探测任务，部署在不同网络位置的探针节点（云服务器 + 家庭机器）并行探测，结果聚合到状态页，异常自动告警；告警触发后，把多节点探测证据喂给 LLM 生成辅助根因分析。支持两类监控：无侵入的端点监控（HTTP/TCP/ping/SSL）和需要鉴权的自有服务器监控（SSH，多探针冗余）。重点解决多点探测、探针高可用、时序聚合、密钥管理、告警与 AI 辅助根因分析。

---

## 2. 业务需求与真实痛点

每个团队都有一堆要盯的服务/网站，真实痛点：

- 服务挂了**靠用户投诉才发现**，没有主动监控
- 没有**历史延迟数据**，出了性能问题没法复盘
- **SSL 证书过期**没人提醒，到期当天才发现
- 监控本身是**单点**——监控方网络一抽风，自己也瞎了
- **自有服务器（云主机/内网机）只有"能不能访问"不够**，还得知道 CPU/内存/磁盘/关键服务是否健康，但不想在被监控机上装 agent
- 告警只有一句"挂了"，**为什么挂要值班人自己人肉排查**——多节点视角其实已经包含答案，缺的是把它翻译出来的那一层（→ §7.6 AI 辅助根因分析）

规模真实可控：监控几十个目标，每分钟探一次，**不依赖吹量**。Uptime Kuma / Pingdom / status.github.com 都是同类需求，证明市场普遍。

---

## 3. 为什么必须分布式（核心卖点）

监控这件事**天然需要多点探测**，这是本项目"分布式"最硬的合法性来源：

1. **单点监控有盲区**：监控方自己网络/机房挂了，它也瞎，分不清"服务挂了"还是"监控挂了"
2. **多点反映真实可用性**：云服务器 = 公网机房视角，家庭机器 = 真实用户网络视角。同一服务从 3 个点看，能区分"全局故障" vs "某条线路抽风"
3. **高可用监控不能单点**：监控系统的探针层自身要冗余，这是分布式系统的经典命题

→ 这三点合起来，"为什么需要多机"不言自明。

---

## 4. 系统架构

### 4.1 角色

| 角色              | 部署位置                            | 职责                                                              |
| ----------------- | ----------------------------------- | ----------------------------------------------------------------- |
| **Coordinator**   | 云 Ubuntu（公网、常驻）             | 配置管理、调度探测任务、接收上报、聚合存储、状态页、告警判定      |
| **Probe（探针）** | Windows 主机 / Arch 笔记本 / 云节点 | 主动连 Coordinator，领探测任务，执行 HTTP/TCP/ping 探测，上报结果 |

### 4.2 拓扑

```
                 ┌──────────────────────────────┐
                 │  Coordinator (云 Ubuntu)      │
                 │  gin 状态页 + 配置 + 告警      │
                 │  调度器 + 聚合器              │
                 │  mysql(元数据/历史) + redis   │
                 └───────────────┬──────────────┘
                      grpc 长连接 | 探针主动反连
            ┌────────────────────┼────────────────────┐
            ▼                    ▼                    ▼
      Probe(Windows)        Probe(Arch)          Probe(云/同机)
      家庭网络视角           家庭网络视角          公网机房视角
      探测目标 → 上报        探测目标 → 上报       探测目标 → 上报
```

> 家庭探针在 NAT 后，**主动出站连接 Coordinator**（长连接/定时拉取），绕开穿透问题。这一点本身就是经典的分布式网络问题（NAT 穿透的反向思路）。

---

## 5. 技术栈选型与理由

| 技术                    | 用途                              | 为什么是它                                 |
| ----------------------- | --------------------------------- | ------------------------------------------ |
| Go                      | 全栈语言                          | 目标技术栈，单二进制部署到三机             |
| gin                     | 状态页 / 配置 API / 告警 webhook  | HTTP 层，REST 友好                         |
| grpc                    | Coordinator ↔ Probe 通信          | 强类型 proto、长连接、双向流，适合探针上报 |
| mysql (或 postgres)     | 目标配置、探测历史、告警记录      | 需要事务（状态机）+ 聚合查询               |
| redis                   | 告警防风暴（静默期/去重）、连续失败计数 | 阶段 2 正式落地，轻量                      |
| LLM API（OpenAI 兼容）  | 告警辅助根因分析（§7.6）     | DeepSeek（充值 10 元够全程）或本地 ollama（免费） |
| docker / docker-compose | 探针镜像化部署                    | 扔到三台机器即跑，环境一致                 |

**起步期可暂不上 redis**：阶段 0-1 用 mysql + 内存队列即可；**阶段 2 引入**——告警防风暴（静默期、连续失败计数）是它第一个真实落点。

---

## 6. 核心数据模型（mysql）

```sql
-- 监控目标
CREATE TABLE monitor_target (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    name        VARCHAR(128) NOT NULL,
    kind        VARCHAR(16)  NOT NULL,        -- http | tcp | ping | dns
    endpoint    VARCHAR(512) NOT NULL,        -- url / host:port
    interval_s  INT NOT NULL DEFAULT 60,      -- 探测间隔
    timeout_ms  INT NOT NULL DEFAULT 5000,
    expect_code INT,                          -- http 期望状态码
    enabled     TINYINT NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL,
    KEY idx_enabled (enabled)
);

-- 探针节点
CREATE TABLE probe_node (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    node_id     VARCHAR(64) NOT NULL UNIQUE,
    name        VARCHAR(128),
    region      VARCHAR(64),                  -- cloud / home-win / home-arch
    capabilities VARCHAR(256),                -- 支持的探测类型
    last_seen   DATETIME,                     -- 心跳
    status      VARCHAR(16) NOT NULL DEFAULT 'online'
);

-- 探测结果（时序，量大，注意索引与分区）
CREATE TABLE probe_result (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    target_id   BIGINT NOT NULL,
    node_id     VARCHAR(64) NOT NULL,
    success     TINYINT NOT NULL,
    latency_ms  INT,
    status_code INT,
    metrics     JSON,                        -- ssh 等指标探测的数值（cpu/disk/mem 等），供阈值告警
    err_msg     VARCHAR(256),
    checked_at  DATETIME NOT NULL,
    KEY idx_target_time (target_id, checked_at),
    KEY idx_node_time (node_id, checked_at)
);

-- 告警事件
CREATE TABLE alert_event (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    target_id   BIGINT NOT NULL,
    node_id     VARCHAR(64),                  -- NULL = 多节点综合判定
    level       VARCHAR(16) NOT NULL,         -- down | slow | cert
    message     VARCHAR(512),
    fired_at    DATETIME NOT NULL,
    resolved_at DATETIME,
    KEY idx_target_fired (target_id, fired_at)
);

-- AI 根因分析结果（阶段 2.7，见 §7.6）
CREATE TABLE alert_analysis (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    alert_id    BIGINT NOT NULL,
    model       VARCHAR(64) NOT NULL,        -- deepseek-chat / ollama:qwen2.5
    input_ctx   JSON NOT NULL,               -- 喂给模型的上下文快照（可复现、可审计）
    output      JSON,                        -- 结构化分析结果（格式见 §7.6）
    status      VARCHAR(16) NOT NULL,        -- pending | done | failed
    embedding   BLOB,                        -- v2（阶段 3）：上下文向量，历史相似告警检索用
    created_at  DATETIME NOT NULL,
    KEY idx_alert (alert_id)
);

-- SSL 证书记录
CREATE TABLE cert_record (
    target_id   BIGINT PRIMARY KEY,
    not_before  DATETIME,
    not_after   DATETIME,
    issuer      VARCHAR(256),
    checked_at  DATETIME
);

-- SSH 监控目标扩展配置（kind='ssh' 的 target 用）
CREATE TABLE ssh_target_config (
    target_id     BIGINT PRIMARY KEY,
    host          VARCHAR(256) NOT NULL,        -- host:port
    user          VARCHAR(64)  NOT NULL,
    command       VARCHAR(1024) NOT NULL,       -- 在远端执行的命令
    check_type    VARCHAR(16)  NOT NULL,        -- exit_code | contains | regex | numeric
    check_expect  VARCHAR(256),                 -- 期望值/正则/阈值表达式
    metric_name   VARCHAR(64),                  -- 抽取出的指标名（如 disk_pct）
    metric_extract VARCHAR(256),               -- 抽取方式（regex 等）
    probe_key_map JSON                          -- {node_id: pubkey_fingerprint} 哪些探针的公钥已登记（主备候选池）
    primary_node VARCHAR(64),                   -- 当前主探针 node_id（故障转移时由 Coordinator 更新）
);
```

> **密钥不入库。** SSH 私钥只存在各探针本地（见 §7.5），Coordinator 不持有任何私钥；`probe_key_map` 仅记录"哪个探针的公钥指纹已登记到目标服务器"，用于调度判断。

> `probe_result` 是时序大表：阶段 2 后考虑按时间分区或冷热分离（旧数据降采样聚合）。这本身是个重要的工程点。

---

## 7. 关键流程

### 7.1 探针注册与心跳

1. Probe 启动 → grpc 调 `Register(node_id, capabilities)` → Coordinator 写/更新 `probe_node`
2. Probe 每隔 N 秒 `Heartbeat` → 更新 `last_seen`
3. Coordinator 后台扫 `last_seen` 超时 → 标 `offline`，其未完成任务重派（阶段 2）

### 7.2 探测调度与执行

1. Coordinator 按 `target.interval_s` 生成探测任务，按"目标需要哪些 region 的探针"分发
2. Probe 领任务（grpc 流 `StreamTasks` 或定时 `FetchTasks`）
3. Probe 执行探测：HTTP（状态码/延迟/关键字）、TCP（连通+延迟）、ping、TLS 握手取证书、**SSH（执行命令取状态/指标，见 §7.5）**
4. Probe grpc `Report(result)` 上报 → Coordinator 写 `probe_result`

### 7.3 聚合与状态判定

1. 聚合器：对每个 target，综合多节点最近 N 次结果 → 判定状态（up/down/degraded）
2. 规则示例：≥1 节点 up 即 up；全部 down 才 down；部分 down = degraded
3. 状态翻转时生成 `alert_event`

### 7.4 告警

1. 状态 down / 延迟超阈值 / 证书 < N 天 → 触发告警
2. 去抖：连续 K 次失败才告警，避免抖动误报
3. 渠道：webhook（飞书/钉钉/企业微信）/ 邮件（起步只做 webhook）
4. 告警生成的同时投递 AI 根因分析任务（异步，不阻塞告警链路，见 §7.6）

### 7.5 SSH 服务器监控（主探针 + 故障转移）

SSH 监控采用**主备模式**：每个 SSH 目标指定**一个主探针**负责探测，并维护一条**备用探针链**；主探针正常时只有它在工作，主探针异常（离线/心跳超时/连续失败）时由 Coordinator 提升下一个备用探针接管。

**为什么主备而非全量多点：**

- SSH 指标（CPU/磁盘/服务状态）是**客观值，与探测点无关**，多探针同时 SSH 同一台服务器跑相同命令是纯浪费（N 倍连接与执行、结果一致）
- 主备模式在 **1/N 开销下拿到等价的容错保障**：主挂了备用顶上，监控层不变成单点
- 这是分布式系统经典的主备/故障转移模式（类比数据库主从切换），讲得出资源与可靠性的权衡

**调度与故障转移流程：**

1. Coordinator 为每个 SSH 目标从"持有该目标密钥的探针池"中选一个主探针，其余为备用（按优先级排序）
2. 仅向主探针下发该目标的 SSH 任务；备用探针不主动探测
3. Coordinator 监测主探针健康：心跳超时 → 标 offline；或主探针对该目标连续失败达阈值
4. 触发转移：从备用链提升下一个探针为新主，向其下发任务；旧主恢复后可重新纳入备用池
5. 探针执行：领到 `kind=ssh` 任务 → 加载本地对应私钥 → SSH 连接 → 执行 `command` → 取输出/退出码 → 判定 + 抽取指标 → 上报 `probe_result.metrics`

**密钥管理（核心设计点）：采用"每探针独立密钥"，不设中心化主密钥。**

> 故障转移能生效的前提：备用探针也持有目标服务器的密钥。因此密钥方案仍是每探针独立持有，Coordinator 只记录"哪些探针的公钥已登记到目标服务器"（即可作为主/备候选），不接触私钥。

| 方案          | 做法                                                                                | 取舍                                                                |
| ------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| **A（采用）** | 每个探针本地生成自己的 SSH 密钥对；把候选探针的公钥加到目标服务器 `authorized_keys` | ✅ 无主密钥、私钥永不上网络、主备皆可接管。代价：服务器要加多条公钥 |
| B（不采用）   | 单一密钥存 Coordinator，探针经 grpc/mTLS 向其申请                                   | 服务器只加一条公钥，但私钥要在网上传输+缓存，泄露面更大             |

**安全增强：** 被监控服务器用专用受限账号；`authorized_keys` 用 `command="..."` 限制可执行命令；探针→Coordinator 通信用 mTLS（阶段 3）。

**侵入性边界：** SSH 监控适合**自己有控制权的服务器**（自有云主机/内网机），不适合第三方/客户机器。项目定位写清楚"自有服务器监控"。

### 7.6 AI 辅助根因分析（告警触发，阶段 2.7）

**定位：辅助分析，不是自动修复。** 输出是给值班人的第一手判断依据；对外口径统一为"AI 辅助根因分析"，不承诺准确率；分析任务异步执行，失败绝不阻塞告警链路。

**为什么这个功能在本系统里成立、放到单点监控里就是玩具：** 多节点证据矩阵正是根因推理最值钱的输入——

| 证据模式                        | 合理推断                                          |
| ------------------------------ | ------------------------------------------------- |
| 云探针超时，家庭探针正常        | 服务在线；机房出口/线路劣化，或目标对机房 IP 限流 |
| 三个位置同时失败                | 全局故障；优先怀疑服务本身或 DNS                   |
| 仅家庭节点失败、延迟同步升高    | 本地网络/运营商问题，与服务无关                    |
| 端点正常但 SSH 指标越限         | 服务"没挂但快挂了"，容量类问题（依赖阶段 2.5）     |

单点系统只能回答"挂了吗"，本系统能回答"**从哪里看挂了**"——这是喂给 LLM 的本质差异，也是该功能的差异化卖点。

**流程：**

1. 告警生成（§7.3/§7.4）→ 投递异步分析任务（Coordinator 内置 worker + 内存队列起步，不进告警主链路）
2. **上下文组装（工程核心，输入质量决定输出质量）**：
   - 目标静态配置（name/kind/endpoint/interval/expect）
   - 多节点证据矩阵：各节点最近 N 次的成败、延迟、状态码、err_msg
   - 附加证据：SSL 证书剩余天数（如适用）、该目标近 7 天故障/恢复摘要
3. 调 LLM（OpenAI 兼容接口），**强制结构化 JSON 输出**：

   ```json
   {
     "pattern": "cloud_only_failure",
     "confidence": "medium",
     "causes": [
       {"desc": "机房到目标线路劣化", "evidence": "仅云探针连续 3 次超时，家庭节点延迟正常", "probability": 0.6},
       {"desc": "目标对机房 IP 限流", "evidence": "失败集中在单一视角", "probability": 0.3}
     ],
     "suggested_actions": ["从云节点 traceroute 目标", "查目标侧访问日志中机房 IP 的 5xx"],
     "needs_human": true
   }
   ```

4. 解析入库（`alert_analysis`）；JSON 解析失败 → 携带错误信息重试一次 → 仍失败标 `failed`，状态页显示"分析失败"而非空白
5. 展示：状态页告警详情 + webhook 消息附一段分析摘要

**v2（阶段 3 选做，RAG-lite）：** 把告警上下文做 embedding 存表（`alert_analysis.embedding`），新告警到来时暴力余弦检索 Top-K 相似历史事件，一并拼进 prompt（"上次同模式最终定位是 DNS 问题"）。当前数据量级（万级以内）暴力计算足够，**不上向量库**本身就是个有意识的取舍。

---

## 8. grpc 协议草案（proto）

```proto
service ProbeService {
  rpc Register (RegisterReq) returns (RegisterResp);
  rpc Heartbeat (HeartbeatReq) returns (HeartbeatResp);
  rpc StreamTasks (TaskStreamReq) returns (stream ProbeTask);   // server 推送任务
  rpc Report (stream ProbeResult) returns (ReportAck);          // 探针批量上报
}

message RegisterReq  { string node_id = 1; string name = 2; string region = 3; repeated string capabilities = 4; }
message ProbeTask    { int64 target_id = 1; string kind = 2; string endpoint = 3; int32 timeout_ms = 4; map<string,string> opts = 5; }   // kind: http|tcp|ping|ssh ; ssh 的命令/判定规则放 opts
message ProbeResult  { int64 target_id = 1; string node_id = 2; bool success = 3; int32 latency_ms = 4; int32 status_code = 5; string metrics = 6; string err_msg = 7; int64 checked_at = 8; }  // metrics: JSON，ssh 指标用
```

> 起步可先用最简单的 **定时 `FetchTasks` + `Report`**（一问一答），跑通后再升级为 `StreamTasks`/双向流。控制初期复杂度。

---

## 9. HTTP API（gin）

```
GET    /api/status                 状态页数据（各 target 多节点状态 + 延迟）
GET    /api/targets                列出监控目标
POST   /api/targets                新增目标
PUT    /api/targets/:id            修改
DELETE /api/targets/:id            删除
GET    /api/targets/:id/metrics    单目标延迟历史（折线图数据）
GET    /api/nodes                  探针节点列表 + 在线状态
GET    /api/alerts                 告警事件列表
POST   /api/alert/channels         配置告警 webhook
GET    /api/alerts/:id/analysis    查看该告警的 AI 根因分析
POST   /api/alerts/:id/reanalyze   手动重新分析
```

状态页前端起步用模板渲染或极简单页（HTML + 一点 JS/ECharts 画延迟曲线），不投入重前端。

---

## 10. 分阶段开发计划

**原则：每阶段结束都能独立演示、独立交付。不憋大招。**

### 阶段 0：单机探活跑通（约 1~2 周）

- gin：增删改查 monitor_target；后台用 goroutine 定时探测
- HTTP/TCP/ping 探测器；结果入 mysql；最小状态页
- **验收**：本地加一个目标，页面能看到绿/红 + 延迟
- 此阶段无分布式，验证业务核心与探测/存储链路

### 阶段 1：分布式 MVP（约 3~4 周）⭐ 核心

- 抽出 Probe 为独立进程；定义 grpc proto
- Coordinator：注册/心跳/任务分发/结果接收
- Probe：反连 Coordinator → 领任务 → 探测 → 上报
- 2 个 Probe 跑起来（先本机多实例或同局域网）
- 聚合器：多节点结果 → 综合状态
- **验收/demo**：状态页每个目标显示来自多个节点的状态；三机并行探测可见
- **到这里已是完整的分布式项目**

### 阶段 2：可靠性 + 告警 + Redis 防风暴（约 2 周，强烈建议）

- 心跳超时检测、探针离线标记、任务重派
- 状态翻转检测 + 去抖规则 → 生成 alert_event
- webhook 告警（飞书/钉钉）
- 告警防风暴：redis 落地——同告警静默期内不重发（SET NX EX）、连续失败计数（INCR + TTL）
- SSL 证书过期检测
- **验收/demo**：故意停掉一个被监控服务 → 告警秒触发；杀掉一个 Probe → 任务漂移到其他节点
- **把项目从"能跑"提到"有深度"的关键一步，性价比最高**

### 阶段 2.5：SSH 服务器监控（约 2~3 周，强烈建议；时间紧可降级为单主探针、不做故障转移）

- 新增 `kind=ssh` 探测：探针用本地私钥 SSH 连目标、执行命令、判定 + 抽取指标
- `ssh_target_config` 表 + `probe_result.metrics` 字段
- **主备模式**：每目标指定主探针 + 备用链，仅主探针探测；Coordinator 监测主探针健康，异常时提升备用接管
- 每探针独立密钥（方案 A）：本地生成密钥对，候选探针公钥登记到服务器，`probe_key_map` 记录
- 指标阈值告警（磁盘>90% 等），复用阶段 2 告警框架
- **验收/demo**：加一台自有服务器，状态页显示 CPU/磁盘/服务状态；杀掉主探针 → 备用探针接管，SSH 监控不中断
- **依赖阶段 2 的告警框架先就位**

### 阶段 2.7：AI 辅助根因分析（约 1~2 周，差异化卖点）

- 告警 → 异步分析任务（不进告警主链路）；上下文组装器（多节点证据矩阵，见 §7.6）
- LLM 结构化输出解析 + 容错（JSON 解析失败重试一次，仍失败标 failed）
- 状态页告警详情展示"可能原因排序 + 建议动作"；webhook 消息附分析摘要
- **验收/demo**：停掉被监控服务 → 告警秒触发 → 状态页 30 秒内出现根因分析；只断家庭网络 → 分析指出"仅家庭视角失败，服务在线"
- 依赖阶段 2 告警框架；SSH 指标作为证据增强依赖 2.5（可选）

### 阶段 3：增强（看时间，挑着做，不做也不亏）

- [ ] 接入家里真实机器（NAT 反连验证）—— 演示效果最好
- [ ] mTLS 鉴权（跨公网通信安全）
- [ ] 时序数据降采样 / 分区（probe_result 性能）
- [ ] 状态页历史延迟曲线（ECharts）
- [ ] 探针 docker 化，docker-compose 一键起三节点
- [ ] AI v2：历史相似告警检索（embedding + 暴力余弦，RAG-lite）
- [ ] k3s 部署 Coordinator（Deployment/Service/健康探针，演示滚动更新）——容器编排实践

---

## 11. 三机部署方案

| 机器         | 跑什么                        | 网络                           |
| ------------ | ----------------------------- | ------------------------------ |
| 云 Ubuntu    | Coordinator + mysql (+ redis) | 公网 IP，被探针反连            |
| Windows 主机 | Probe（region=home-win）      | NAT 后，主动出站连 Coordinator |
| Arch 笔记本  | Probe（region=home-arch）     | NAT 后，主动出站，可能休眠断线 |

**docker-compose（开发期模拟三节点）：** 一个 compose 文件起 coordinator + 2~3 个 probe 容器（不同 region 标识），本地就能演示多点。

**生产/演示：** Coordinator 部署到云；两个 Probe 编译成单二进制（或 docker）拷到家庭机器运行，配置 `--coordinator=cloud_ip:port`。笔记本休眠断线 = 阶段 2"探针离线/重连"的真实演练场景。

---

## 12. 风险与取舍

| 风险                                        | 应对                                                  |
| ------------------------------------------- | ----------------------------------------------------- |
| 一上来想做太多（容器化/通用框架/mTLS 全上） | **严格按阶段**，阶段 0-2 不碰容器化与 mTLS            |
| grpc 双向流调试复杂                         | 起步用 `FetchTasks`+`Report` 一问一答，跑通再升级流式 |
| `probe_result` 膨胀                         | 阶段 2 后做降采样/分区；先不过度设计                  |
| 家庭机器不稳定                              | 当成特性而非缺陷——正好演示离线重连与任务漂移          |
| 前端投入失控                                | 状态页只做最小可用，用模板 + ECharts 即可             |
| LLM 输出不稳定 / 幻觉                       | 强制结构化 JSON + 解析失败重试兜底；定位"辅助分析"不承诺准确；分析失败不阻塞告警 |

**核心取舍：定位为"系统"而非"通用框架"。** 把分布式探活监控做扎实、可演示、讲得清，就是一个合格的项目。等核心跑通且有余力，再考虑往"通用监控平台/插件化探测"演进——那时是在能跑的基础上加抽象，风险低。
