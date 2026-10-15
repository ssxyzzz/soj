.PHONY: up down restart logs ps
up: ## 构建并启动全部服务
	docker compose up -d --build

down: ## 停止并移除容器（数据卷保留）
	docker compose down

restart: ## 重启全部服务
	docker compose restart

logs: ## 跟踪全部服务日志
	docker compose logs -f --tail=100

ps: ## 查看服务状态
	docker compose ps

# ---------- 本地开发（基础设施用 compose 起，业务代码本机跑） ----------
# docker compose up -d mysql redis rabbitmq sandbox
server-dev: ## 本地运行 Web API（默认监听 :8080）
	cd server && go run ./cmd/web

judge-dev: ## 本地运行判题 worker
	cd server && go run ./cmd/judge

web-dev: ## 本地运行前端（vite，默认 :5173，/api 代理到 :8080）
	cd web && npm install && npm run dev

help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
