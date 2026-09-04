# bigbrother

分布式探活监控系统 — 设计文档见 [docs/DESIGN.md](docs/DESIGN.md)。

## 快速开始

```bash
make run    # 等价于 BIGBROTHER_TARGETS=configs/targets.json go run ./cmd/coordinator
make test   # go test -race ./...
```

## 配置

监控目标清单默认读 `configs/targets.json`(JSON 数组),
首次 `make run` 会自动从 `configs/targets.example.json` 复制一份;
也可用环境变量 `BIGBROTHER_TARGETS` 指向任意路径(必填,不设启动即报错)。
`targets.json` 是每人自己的清单,不提交进 git。

每项含四个字段:

| 字段         | 说明                                 |
| ------------ | ------------------------------------ |
| `type`       | 探测类型:`http` / `https` / `tcp`(大小写不敏感) |
| `target`     | 探测目标,格式由 type 决定,见下表     |
| `interval_ms` | 探测间隔(毫秒),必须为正             |
| `timeout_ms` | 单次探测超时(毫秒),必须为正         |

各类型的 `target` 格式与判定规则:

| type   | target 格式   | 成功判定                                        |
| ------ | ------------- | ----------------------------------------------- |
| `http` | `http://...`  | GET 请求,状态码 2xx/3xx                        |
| `https`| `https://...` | 同上,另校验证书剩余有效期(< 14 天判为失败)     |
| `tcp`  | `host:port`   | 在超时内完成 TCP 三次握手                       |

示例:

```json
[
    {
        "type": "https",
        "target": "https://www.zhihu.com/",
        "interval_ms": 30000,
        "timeout_ms": 5000
    },
    {
        "type": "tcp",
        "target": "baidu.com:443",
        "interval_ms": 10000,
        "timeout_ms": 2000
    }
]
```

校验在启动时一次性完成:type 不支持、target 格式非法、数值不合法都会让进程直接退出(fail fast)——配置错误不会以"目标 down"的形态混进监控结果。
若从旧版升级:原 `url` 字段已拆分为 `type` + `target`,启动报 `unknown prober type` 时按上表改写即可。
