.PHONY: up down restart logs ps backup bin image web-building server-building import webui-dist soj-dev web-dev help

up: ## 构建并启动全部服务（3 容器；先编译前端产物，镜像缺失时自动构建）
	./scripts/web-building.sh
	docker compose up -d

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
	docker compose up -d --build soj

bin: ## 编译前后端并产出 server/bin/soj（等价于 web-building + server-building）
	./scripts/web-building.sh
	./scripts/server-building.sh

web-building: ## 只编译前端（产物 web/dist + 同步 embed 目录）
	./scripts/web-building.sh

server-building: ## 只编译后端（需先 web-building，产物 server/bin/soj）
	./scripts/server-building.sh

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
