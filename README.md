# SOJ — 在线判题系统

从零搭建的 Online Judge 第一版骨架：Vue 3 前端 + Gin API + RabbitMQ 异步判题 + go-judge 沙箱。

当前为 v0.1：镜像构建、docker compose 启动、判题链路冒烟测试均已实际跑通（WSL2 + Docker 环境）。

## 技术选型

| 模块      | 技术                                       |
| --------- | ------------------------------------------ |
| 前端      | Vue 3 + TypeScript + Vite + Element Plus   |
| Web 后端  | Gin (Go 1.26)                              |
| 数据库    | MySQL 8                                    |
| 缓存/排行 | Redis（排行榜用 zset）                     |
| 消息队列  | RabbitMQ                                   |
| 沙箱      | go-judge (REST)                            |
| 部署      | Docker + docker-compose + Nginx            |

## 架构

```
浏览器 ──> Nginx (web:80)
             │ /        -> Vue 静态资源
             │ /api/    -> Gin API (server:8080)
                             │ 提交代码 -> MySQL 落库
                             │ 投递消息 -> RabbitMQ (soj.submissions)
judge worker (cmd/judge) <── 消费队列
             │ 读取题目/测试点 (MySQL)
             │ 编译+运行    (go-judge 沙箱 :5050)
             │ 结果回写     (MySQL)
             │ AC 更新排行  (Redis zset soj:rank:ac)
```

## 目录结构

```
soj/
├── docker-compose.yml        # 全套服务编排
├── .env.example              # 环境变量模板（复制为 .env 使用）
├── Makefile                  # up / down / logs / dev 快捷命令
├── deploy/
│   ├── mysql/init/           # 首次启动自动执行的建表与种子 SQL
│   └── sandbox/Dockerfile    # go-judge + gcc/g++/python3 沙箱镜像（已内置清华 apt 源）
├── server/                   # Go 后端
│   ├── cmd/web/              # API 服务入口
│   ├── cmd/judge/            # 判题 worker 入口
│   ├── cmd/hashpw/           # bcrypt 密码生成小工具（种子数据用）
│   └── internal/
│       ├── config/           # 环境变量配置
│       ├── storage/          # MySQL / Redis 连接（带就绪重试）
│       ├── model/            # 数据表结构与 SQL 访问
│       ├── auth/             # bcrypt + JWT
│       ├── lang/             # 判题语言在沙箱内的编译/运行定义
│       ├── mq/               # RabbitMQ 生产者/消费者
│       ├── sandbox/          # go-judge REST 客户端
│       ├── judge/            # 判题核心流程
│       └── server/           # Gin 路由 / 中间件 / handler
└── web/                      # Vue 3 前端
    └── src/
        ├── api/              # axios 封装与各业务 API
        ├── router/           # 路由与登录守卫
        ├── stores/           # Pinia 登录态
        ├── layouts/ views/   # 页面
        └── utils/            # 状态文案/时间格式化
```

## 快速启动（Docker Compose）

```bash
cp .env.example .env        # 按需修改密码/密钥
docker compose up -d --build
```

启动后访问：

| 地址                        | 说明                                      |
| --------------------------- | ----------------------------------------- |
| http://localhost            | SOJ 站点（Nginx）                         |
| http://localhost:8080/api/health | API 健康检查（db/redis 连接状态）    |
| http://localhost:15672      | RabbitMQ 控制台（soj / soj123）           |

默认管理员：`admin / admin123`（首次登录后请修改）。

常用命令（或用 `make up / down / logs / ps`）：

```bash
docker compose ps        # 服务状态
docker compose logs -f   # 跟踪全部日志
docker compose down      # 停止（数据卷保留）
```

> **网络受限环境（国内）**：仓库内 Dockerfile 已内置国内优化——Go 模块默认走 goproxy.cn（`server/Dockerfile`，海外环境用 `--build-arg GOPROXY=https://proxy.golang.org,direct` 覆盖），沙箱镜像构建时自动把 Debian 源替换为清华镜像。仅基础镜像需要访问 Docker Hub，拉取失败时二选一：
>
> 1. 给 Docker 配置加速器：`/etc/docker/daemon.json` 的 `registry-mirrors` 设为如 `["https://docker.m.daocloud.io"]`，改后 `sudo systemctl restart docker`；
> 2. 或手动经镜像源拉取并打回标准 tag：
>    ```bash
>    for img in mysql:8.0 redis:7-alpine rabbitmq:3-management golang:1.26-alpine node:22-alpine nginx:1.27-alpine alpine:3.21; do
>      docker pull docker.m.daocloud.io/$img && docker tag docker.m.daocloud.io/$img $img
>    done
>    # criyle 命名空间 daocloud 不代理，需走其他源
>    docker pull hub.rat.dev/criyle/go-judge:latest && docker tag hub.rat.dev/criyle/go-judge:latest criyle/go-judge:latest
>    ```

## 本地开发

基础设施用 compose 起，业务代码本机跑：

```bash
docker compose up -d mysql redis rabbitmq sandbox

make server-dev   # API 服务 :8080
make judge-dev    # 判题 worker
make web-dev      # 前端 :5173，/api 代理到 :8080
```

沙箱、判题 worker 与 API 均支持环境变量覆盖，见 `server/internal/config/config.go`。

## API 一览（/api 前缀）

| 方法 | 路径               | 说明                       | 登录 |
| ---- | ------------------ | -------------------------- | ---- |
| GET  | /health            | 健康检查                   | 否   |
| POST | /auth/register     | 注册                       | 否   |
| POST | /auth/login        | 登录，返回 JWT             | 否   |
| GET  | /me                | 当前用户信息               | 是   |
| GET  | /problems          | 题库分页列表               | 否   |
| GET  | /problems/:id      | 题目详情（含样例）         | 否   |
| POST | /submissions       | 提交代码，进入判题队列     | 是   |
| GET  | /submissions       | 提交记录分页（可按题过滤） | 否   |
| GET  | /submissions/:id   | 提交详情（代码仅本人/管理员可见） | 否 |
| GET  | /rank              | 解题排行榜（Redis zset）   | 否   |

统一响应：`{"code":0,"data":...}` 成功；`{"code":1,"message":"..."}` 失败。

## 判题流程

1. 提交落库（`Pending`）→ 投递 `{submission_id}` 到 RabbitMQ 队列 `soj.submissions`；
2. worker 消费（多并发，at-most-once，见 `mq.Consume` 注释可升级为 at-least-once）；
3. 编译型语言先编译，产物以 `copyOutCached` 缓存为 fileId，各测试点 `copyIn` 复用，测完删除；
4. 逐测试点运行：比较输出（忽略行尾空白与文末空行）、累计最大耗时/内存；
5. 回写结果；AC 时 Redis `SADD soj:ac:<uid>` 去重后 `ZINCRBY soj:rank:ac`。

## 已知边界（后续迭代方向）

- 题目/测试点后台管理（当前用 SQL 直接维护，见 `deploy/mysql/init/01-schema.sql`）；
- 判题消息升级 at-least-once + 防重判；
- 特判题（SPJ）、子任务计分、比赛模块；
- 提交详情轮询升级为 WebSocket 推送。
