.PHONY: up down restart logs ps backup bin image web-building server-building import webui-dist soj-dev web-dev check help

# 构建版本（v0.28 发版可追溯）：注入二进制的 soj/internal/server.Version，/api/health 回显
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)

check: ## 全量门禁：lint + go test + 前端 typecheck + build（CI 与 make up 共用）
	$(MAKE) lint
	cd server && go test ./...
	cd web && npm run typecheck
	cd web && npm run build

up: check ## 构建发版并对齐镜像（= check + bin + image：先过全量门禁，热替换后重建 soj 镜像，容器重建/宿主机重启不回退）
	./scripts/web-building.sh
	SOJ_VERSION=$(VERSION) docker compose up -d --build soj

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

image: ## 重建 soj 镜像并重启容器（让镜像内版本追上热替换版本，平时不必跑）
	SOJ_VERSION=$(VERSION) docker compose up -d --build soj

bin: ## 快速编译发版，不过门禁不重建镜像（仅限开发机调试；服务器例行发版一律 make up）
	./scripts/web-building.sh

web-building: ## 只编译前端（产物 web/dist + 同步 embed 目录）
	./scripts/web-building.sh

server-building: ## 只编译后端（需先 web-building，产物 server/bin/soj）
	./scripts/server-building.sh

lint: ## 代码检查：web ESLint + go vet（+ staticcheck 如已安装）
	cd web && npx eslint .
	cd server && go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then cd server && staticcheck ./...; else echo "（未安装 staticcheck，跳过：go install honnef.co/go/tools/cmd/staticcheck@latest）"; fi

import: ## 导入题目目录: make import DIR=data/seed/1_hello_soj
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
