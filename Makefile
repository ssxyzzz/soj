.PHONY: up down restart logs ps backup

up: ## 构建并启动全部服务（3 容器）
	docker compose up -d --build

down: ## 停止并移除容器（数据卷保留）
	docker compose down

restart: ## 重启全部服务
	docker compose restart

logs: ## 跟踪全部服务日志
	docker compose logs -f --tail=100

ps: ## 查看服务状态
	docker compose ps

backup: ## 备份 MongoDB 与测试数据到 ./backups/
	./scripts/backup.sh

bin: ## 用容器编译独立二进制 server/bin/soj（本机无需装 Go/Node）
	docker run --rm -v $(CURDIR)/web:/src -w /src -u "$$(id -u):$$(id -g)" -e HOME=/tmp node:22-alpine sh -c "npm install --no-audit --no-fund && npm run build"
	rm -rf server/internal/webui/dist && cp -r web/dist server/internal/webui/dist
	docker run --rm -v $(CURDIR)/server:/src -w /src -u "$$(id -u):$$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath -e GOPROXY=https://goproxy.cn,direct golang:1.26-alpine sh -c "CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/soj ./cmd/soj"
	@echo "==> 产物: server/bin/soj（前端已嵌入，单文件可执行）"

import: ## 导入题目目录: make import DIR=data/seed/1_A_Plus_B
	cd server && go run ./cmd/soj -import $(DIR)

# ---------- 本地开发（基础设施用 compose 起，业务代码本机跑） ----------
# docker compose up -d mongo sandbox
webui-dist: ## 把前端构建产物复制到 embed 目录（go build 前执行）
	cd web && npm run build
	rm -rf server/internal/webui/dist && cp -r web/dist server/internal/webui/dist

soj-dev: ## 本机运行主程序（需要本机 mongo + sandbox，或先 compose up -d mongo sandbox 并改 SOJ_MONGO_URI）
	cd server && go run ./cmd/soj

web-dev: ## 前端热更新开发（vite :5173，/api 代理到 :8080）
	cd web && npm install && npm run dev

help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
