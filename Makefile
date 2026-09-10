BIN_DIR := bin

# 密钥不进 git:.env 被 .gitignore 忽略;首次使用 cp .env.example .env 并填入密码
-include .env
export BIGBROTHER_DSN BIGBROTHER_TEST_DSN BIGBROTHER_TARGETS
BIGBROTHER_TARGETS ?= configs/targets.json

.PHONY: help run build test fmt vet tidy clean

help: ## 显示所有可用目标
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-8s\033[0m %s\n", $$1, $$2}'

run: ## 开发模式运行 coordinator(.env 里配了 BIGBROTHER_DSN 则走 MySQL,否则内存)
	@test -f $(BIGBROTHER_TARGETS) || cp configs/targets.example.json $(BIGBROTHER_TARGETS)
	go run ./cmd/coordinator

build: ## 编译到 bin/
	go build -o $(BIN_DIR)/coordinator ./cmd/coordinator

test: ## 跑全部测试,-race 开启竞态检测;集成测试读 .env 的 BIGBROTHER_TEST_DSN,未配置则自动 SKIP
	go test -race ./...

fmt: ## 格式化全部代码
	gofmt -w .

vet: ## 静态检查(和 gofmt 互补,查的是逻辑层面的问题)
	go vet ./...

tidy: ## 清理 go.mod 依赖(新增/删除 import 后跑一次)
	go mod tidy

clean: ## 删除编译产物
	rm -rf $(BIN_DIR)
