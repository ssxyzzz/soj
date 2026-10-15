# SOJ — 在线判题系统

面向班级与社团的轻量级 Online Judge：一个 Go 程序 + MongoDB + go-judge 沙箱，三个容器就是一个完整的 OJ。
支持班级作业（即时出分）、班级考试（OI 赛制）、主题库比赛、解题排名与完整的班级权限体系。
设计决策与迭代记录见 [plan.md](plan.md)（本地文档，不入库）。

| 模块 | 技术 |
| --- | --- |
| 主程序 | Go 1.26 + Gin（前端页面 embed 进二进制，内置判题线程池） |
| 数据库 | MongoDB 7（单节点副本集） |
| 判题沙箱 | go-judge（C / C++14（对标 NOI/CSP-J/S）/ Python3） |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus |
| 部署 | Docker Compose 三容器：soj / mongo / sandbox |

```
浏览器 ──> soj:80（页面 + API 同端口）
             └─ 判题线程池：每秒从 MongoDB 原子领取 Pending 提交
                 ├─ 题目测试数据：按需逐点读取（/data/testdata 卷）
                 └─ go-judge 沙箱（localhost:5050）编译运行并比较输出
```

## 功能总览

- **题库**：题目全局共享；支持主题库隐藏（default 不可见）、子任务分组给分（problem.yaml 可选）；比赛锁题期间全站指引到赛事页提交。
- **班级**：老师建班、邀请码/名单导入拉学生；班级作业（题目清单，随时做、即时出分，老师可看每人各题完成情况）；班级考试（OI 赛制：考时不出分只记次数，考后出榜，支持赛后隐藏期）；班级多管理员。
- **比赛**：主题库比赛由 root 发起，面向全体登录用户；与考试共用锁题规则。
- **排名**：解题数排行（比赛提交在隐藏期结束后才计入，防赛中推测成绩）。
- **权限**：全局角色 guest / default（学生）/ admin（班级管理）/ root（系统管理）。
- **防刷**：default 用户提交限速 1 分钟 3 次（root 后台可调）。
- **性能与安全**：响应 gzip 压缩；带哈希的静态资源一年期 immutable 缓存、其余每次回源校验；Vditor 懒加载分包不拖累首屏；题面/说明 256KB 上限（服务端拦截 + 查看端拒渲染）+ 渲染后 DOMPurify 消毒。

## 快速启动

```bash
docker compose up -d --build
```

打开 **http://localhost**（主机侧只暴露 80 端口，容器内仍是 8080），默认管理员 `admin / admin123`（root）。
示例题（A + B、Hello SOJ 等）首次启动自动导入，登录后即可提交体验完整判题流程。

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

cd web && npm install && npm run dev        # 前端 :5173，vite 热更新（改代码即时生效）
cd server && go run ./cmd/soj               # 主程序 :8080（连本机 mongo/sandbox）
cd server && go run ./cmd/soj --judge-only  # 或：纯判题进程
```

开发前端时的推荐姿势：起好基础设施后开两个终端——`npm run dev`（热更新）+ `go run ./cmd/soj`，改前端代码保存即生效，无需任何构建。

## 手动编译（本机 Node/Go 工具链）

两个脚本用本机工具链编译（Go 与 Node 装在用户目录 `~/.local/opt/` 免 sudo，安装方法见 plan.md 第 6 章；模块代理 goproxy.cn / npmmirror 已内置），带实时读秒计时、日志落盘（失败自动展示末 20 行）：

```bash
./scripts/web-building.sh      # 编译前端 → 同步 embed 目录 → 重建并重启容器
./scripts/server-building.sh   # 编译后端 → 热替换进容器并重启（秒级发版，需先跑过前者）
make bin                       # 等价于上面两步连做
```

发版节奏：只改后端跑 `server-building.sh` 即可秒级更新；改了前端需跑 `web-building.sh`（前端嵌入镜像，走一次镜像重建）。
产物：`server/bin/soj`（静态单文件，前端已嵌入），可直接拷到任何 Linux 服务器运行。

## 出一道题

按下面的目录结构准备好题目，然后 `make import DIR=路径`（或放进 `data/seed/` 随启动自动导入）：

```
my-problem/
├── problem.yaml      # title / timeLimitMs / memoryLimitKb / samples / hidden / subtasks
├── statement.md      # 题面
└── cases/
    ├── 1/in  1/ans   # 每个测试点一对输入/答案
    └── 2/in  2/ans
```

- 目录名前缀数字作为题目 ID（如 `7_我的题目` → pid 7），省略则自动分配；
- `hidden: true` 设为主题库隐藏（default 不可见，适合做考试题）；
- `subtasks` 可选子任务分组给分（组内测试点全过才得分，分值之和须为 100）：

```yaml
subtasks:
  - cases: [1, 2]
    score: 40
  - cases: [3]
    score: 60
```

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
| GET | /problems · /problems/:id | 题库列表 / 详情（按视角过滤隐藏与赛事锁题） | 公开 |
| PATCH | /problems/:id/hidden | 设置主题库隐藏 | root / 题目导入者 |
| POST | /submissions | 提交代码（default 限速 1 分钟 3 次，root 可调） | 登录 |
| GET | /submissions · /submissions/:id | 提交列表 / 详情（代码仅本人与管理员可见） | 公开 |
| GET | /rank | 解题排名（30s 缓存；比赛/考试提交隐藏期结束才计入） | 公开 |
| GET | /classes · /classes/:id | 班级列表 / 详情 | 公开 |
| POST | /classes | 创建班级 | admin+ |
| POST | /classes/:id/join · /leave | 邀请码加入 / 退出 | 登录 |
| POST | /classes/:id/assignments | 布置作业 | 班级管理员 |
| GET | /classes/:id/assignments · /classes/:id/exams | 作业列表 / 考试列表（仅本班成员） | 登录 |
| GET | /assignments/:id · /assignments/:id/progress | 作业详情 / 完成情况 | 成员 / 班级管理员 |
| POST/DELETE | /assignments/:id/problems[/:pid] | 作业加题 / 移除题 | 班级管理员 |
| GET | /contests · /contests/:id | 主题库比赛列表 / 详情 | 公开 |
| POST | /contests | 创建主题库比赛 | root |
| GET | /contests/:id/submissions/mine · /contests/:id/leaderboard | 比赛我的提交 / 榜单 | 参赛者 / 组织者 |
| POST | /exams/:id/problems/:pid/submissions | 考试提交（考时唯一通道，不出分） | 本班成员 |
| GET | /exams/:id · /exams/:id/submissions/mine · /exams/:id/leaderboard | 考试详情 / 我的提交 / 榜单 | 本班成员 |
| POST | /classes/:id/members/:uid/role | 设置班级成员角色 | 建班人 / 班级管理员 / root |
| GET/POST/PUT | /admin/users[/:id] · /admin/settings | 用户管理 / 系统设置 | root |

## 备份与恢复

```bash
make backup   # 产出 backups/<时间>/mongo.archive.gz + testdata.tar.gz
```

恢复：`mongorestore --archive=mongo.archive.gz --gzip`，测试数据解包回 testdata 卷即可。

## 路线图

v0.4.1 作业与管理入口 ✅ → v0.4.2 班级考试 ✅ → v0.4.3 代码可见性 ✅ → v0.4.4 富文本 Vditor ✅ → v0.4.5 上云前性能与安全（gzip/缓存/分包 + 长度上限/渲染消毒 + 编译部署改造）✅ → 下一批 v0.5a 题面图片上传 → v0.5b 题目管理/题解/Hydro 导入；标签等暂缓。
细节与排期见 [plan.md](plan.md)（本地文档，不入库）。
