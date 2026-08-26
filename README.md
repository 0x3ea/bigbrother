# bigbrother

分布式探活监控系统 — 设计文档见 [docs/DESIGN.md](docs/DESIGN.md)。

## 快速开始

```bash
make run    # 等价于 BIGBROTHER_TARGETS=configs/targets.json go run ./cmd/coordinator
make test   # go test -race ./...
```

## 配置

监控目标清单默认读 `configs/targets.json`(JSON 数组,每项含 `url` 和 `interval_s`),
首次 `make run` 会自动从 `configs/targets.example.json` 复制一份;
也可用环境变量 `BIGBROTHER_TARGETS` 指向任意路径(必填,不设启动即报错)。
`targets.json` 是每人自己的清单,不提交进 git。
