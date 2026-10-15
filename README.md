# SOJ — 在线判题系统

中小型、轻量化的 Online Judge：一个 Go 程序 + MongoDB + go-judge 沙箱，三个容器就是一个完整的 OJ。
设计决策与迭代路线见 [DEVELOPMENT.md](DEVELOPMENT.md)。

| 模块 | 技术 |
| --- | --- |
| 主程序 | Go 1.26 + Gin（前端页面 embed 进二进制，内置判题线程池） |
| 数据库 | MongoDB 7（单节点副本集） |
| 判题沙箱 | go-judge（gcc / g++ / python3） |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus |
| 部署 | Docker Compose 三容器：soj / mongo / sandbox |

```
浏览器 ──> soj:8080（页面 + API 同端口）
             └─ 判题线程池：每秒从 MongoDB 原子领取 Pending 提交
                 ├─ 题目测试数据：按需逐点读取（/data/testdata 卷）
                 └─ go-judge 沙箱（localhost:5050）编译运行并比较输出
```

## 快速启动

```bash
docker compose up -d --build
```

打开 **http://localhost**（主机侧只暴露 80 端口，容器内仍是 8080），默认管理员 `admin / admin123`。
两道示例题（A + B Problem、Hello, SOJ!）在首次启动时自动导入，登录后即可提交体验完整判题流程。

常用命令：

```bash
make ps        # 服务状态
make logs      # 跟踪日志
make backup    # 备份 MongoDB + 测试数据到 ./backups/
make down      # 停止（数据保留）
```

## 本地开发

```bash
docker compose up -d mongo sandbox   # 只起基础设施

cd web && npm install && npm run dev        # 前端 :5173（/api 代理到 :8080）
cd server && go run ./cmd/soj               # 主程序 :8080（连本机 mongo/sandbox）
cd server && go run ./cmd/soj --judge-only  # 或：纯判题进程
```

### 出一道题

按下面的目录结构准备好题目，然后 `make import DIR=路径`（或在启动时放进 `data/seed/` 自动导入）：

```
my-problem/
├── problem.yaml      # title / timeLimitMs / memoryLimitKb / samples: [1]
├── statement.md      # 题面
└── cases/
    ├── 1/in  1/ans   # 每个测试点一对输入/答案
    └── 2/in  2/ans
```

目录名前缀数字作为题目 ID（如 `7_我的题目` → pid 7），省略则自动分配。

## 配置

全部走环境变量（见 `docker-compose.yml` 与 `server/internal/config/config.go`），常用的：

| 变量 | 说明 | 默认 |
| --- | --- | --- |
| SOJ_ADDR | 监听地址 | :8080 |
| SOJ_MONGO_URI | MongoDB 连接串 | mongodb://127.0.0.1:27017 |
| SOJ_DATA_DIR | 测试数据根目录 | ./data/testdata |
| SOJ_SEED_DIR | 启动时自动导入的种子题目录 | （空） |
| SOJ_SANDBOX_ADDR | go-judge 地址 | http://127.0.0.1:5050 |
| SOJ_JUDGE_WORKERS | 判题并发数 | 2 |
| SOJ_JWT_SECRET | JWT 密钥（生产必改） | soj-dev-secret |

## API 一览（/api 前缀）

| 方法 | 路径 | 说明 | 权限 |
| --- | --- | --- | --- |
| GET | /health | 健康检查（db / sandbox） | 公开 |
| POST | /auth/register · /auth/login | 注册 / 登录（JWT） | 公开 |
| GET | /me · /me/classes | 当前用户 / 我的班级 | 登录 |
| GET | /problems · /problems/:id | 题库列表 / 详情（含样例；按视角过滤隐藏与比赛锁题） | 公开 |
| PATCH | /problems/:id/hidden | 设置主题库隐藏 | root / 题目导入者 |
| POST | /submissions | 提交代码（default 限速 1 分钟 3 次，root 可调） | 登录 |
| GET | /submissions · /submissions/:id | 提交列表 / 详情（代码仅本人与管理员可见） | 公开 |
| GET | /rank | 解题排行榜（30s 缓存；比赛提交隐藏期结束才计入） | 公开 |
| GET | /classes · /classes/:id | 班级列表 / 详情（成员与邀请码仅管理身份可见） | 公开 |
| POST | /classes | 创建班级 | admin+ |
| POST | /classes/:id/join · /leave | 邀请码加入 / 退出 | 登录 |
| POST/DELETE | /classes/:id/problems[/:pid] | 班级引用/移除题目 | 建班人 / 班级管理员 / root |
| POST | /classes/:id/invite · /members · /members/:uid/role | 邀请码 / 名单导入 / 成员角色 | 建班人 / 班级管理员 / root |
| GET | /contests · /contests/:id | 比赛列表 / 详情（赛前题目隐藏） | 公开 |
| POST | /contests | 创建比赛（主题库=root；班级=班级管理员） | 登录 |
| POST | /contests/:id/problems/:pid/submissions | 比赛提交（赛时唯一提交通道，不出分） | 参赛者 |
| GET | /contests/:id/submissions/mine · /leaderboard | 我的比赛提交 / 榜单（赛时仅组织者可见） | 参赛者 |
| GET/POST/PATCH | /admin/users[/:id] · /admin/settings | 用户管理 / 系统设置 | root |

## 备份与恢复

```bash
make backup   # 产出 backups/<时间>/mongo.archive.gz + testdata.tar.gz
```

恢复：`mongorestore --archive=mongo.archive.gz --gzip`，测试数据解包回 testdata 卷即可。

## 路线图

v0.2 换底盘 ✅ → v0.3 权限与班级 ✅ → v0.4 OI 比赛 ✅ → v0.5 体验优化（SSE 实时判题、zip 上题、Hydro/QDUOJ 导入、重测）。
细节与设计决策见 [DEVELOPMENT.md](DEVELOPMENT.md)。
