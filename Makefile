BIN_DIR := bin

.PHONY: help run build test fmt vet tidy clean

help: ## 显示所有可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-8s\033[0m %s\n", $$1, $$2}'

run: ## 开发模式运行 coordinator(首次自动从 example 复制目标清单)
	@test -f configs/targets.json || cp configs/targets.example.json configs/targets.json
	BIGBROTHER_TARGETS=configs/targets.json go run ./cmd/coordinator

build: ## 编译到 bin/
	go build -o $(BIN_DIR)/coordinator ./cmd/coordinator

test: ## 跑全部测试,-race 开启竞态检测(本项目重并发,值得常开)
	go test -race ./...

fmt: ## 格式化全部代码
	gofmt -w .

vet: ## 静态检查(和 gofmt 互补,查的是逻辑层面的问题)
	go vet ./...

tidy: ## 清理 go.mod 依赖(新增/删除 import 后跑一次)
	go mod tidy

clean: ## 删除编译产物
	rm -rf $(BIN_DIR)
