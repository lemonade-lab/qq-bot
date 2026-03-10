.PHONY: help build run dev test clean install deps lint format swagger docker-build docker-run

# 默认目标
.DEFAULT_GOAL := help

# 帮助信息
help: ## 显示帮助信息
	@echo "bubble 可用命令:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# 开发相关命令
dev: ## 启动开发模式
	@echo "启动开发模式..."
	@echo "确保依赖已安装 (go mod tidy)"
	go mod tidy
	go run .

test: ## 运行测试
	@echo "运行测试..."
	go test ./...

build: ## 构建项目
	@echo "构建项目..."
	go build -o bubble main.go

deps: ## 安装依赖
	go mod tidy

format: ## go fmt
	go fmt ./...

lint: ## go vet
	go vet ./...
