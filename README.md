# SOJ — 在线判题系统

面向班级与社团的轻量级 Online Judge：一个 Go 程序 + MongoDB + go-judge 沙箱，三个容器就是一个完整的 OJ。

支持班级作业（即时出分）、班级考试（OI 赛制）、主题库比赛、解题排名；权限采用**可配置角色 + 权限位**（对标 Hydro 的权限位/角色解耦、按单站点简化），root 可在管理后台自定义角色。

**说明：本 OJ 属于个人开发项目，不提供对历史版本的兼容支持。如你已安装旧版本，请自行处理升级兼容问题，或选择全新安装最新版本。**

| 模块 | 技术 |
| --- | --- |
| 主程序 | Go 1.26 + Gin（前端页面 embed 进二进制，内置判题线程池） |
| 数据库 | MongoDB 7（单节点副本集） |
| 判题沙箱 | go-judge（C / C++14（对标 NOI/CSP-J/S）/ Python3） |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus |
| 部署 | Docker Compose 三容器：soj / mongo / sandbox |

```
浏览器 ──> soj:80（页面 + API 同端口）
             └─ 判题线程池：按优先级从 MongoDB 原子领取 Pending 提交（入队即唤醒，近零延迟）
                 ├─ 题目测试数据：按需逐点读取（/data/testdata 卷）
                 └─ go-judge 沙箱（localhost:5050）编译运行并比较输出
```

## 功能亮点

| 模块 | 能力一览 |
| --- | --- |
| 教学闭环 | 班级作业（即时出分、逐人完成情况）· 考试（OI 赛制 + 赛后隐藏期）· 主题库比赛 · 成绩表 · 班内/全站排名 · 个人主页（难度完成图 + 首 AC 足迹） |
| 判题 | go-judge 沙箱 + 优先级判题池（学生提交优先、批量重测不插队）；比对模式按题可配（全文 / 宽容 token / SPJ）、子任务、文件 IO；比赛锁题、赛时可隐藏难度与知识点 |
| 题型 | 传统题 · 客观题族（单选/多选/判断/填空，提交即判、整卷满分解锁解析）· 函数题（驱动器合编 + 模板预填）· 交互题（双进程管道） |
| 建题与题包 | 网页建题（Markdown + KaTeX + 粘贴传图）· SOJ 统一题包 · Hydro 包导入适配 · 批量导入实时进度 · 整包下载 · 学生版试卷打印 |
| 知识与激励 | 知识树 + 标签树（五级层级色）· 十档难度（锚定 NOI/CSP 教学进度）· 经验等级 + 炫彩徽章 · 难度完成图 · 挑战模式（限时手速 PK + 题内三榜）· 题解 + 视频答案解析 |
| 在线编程 | 全屏工作台（题面分栏可拖）· CodeMirror 6 + 三层补全 · 字体字号即改即存 · 补全按等级解锁 · 自测直跑（自定义输入 + 期望输出比对，判题队列隔离） |
| 讨论与通知 | 三大讨论区（学习分享 / 题内思路 + 代码分数门槛 / 比赛赛时答疑升格）· 楼主采纳 + 经验激励 · 举报折叠 / 禁言 / 管理队列 · 站内通知中心（铃铛 + 消息中心）· 作业/考试/比赛发布与开赛提醒 · 全站广播 |
| 权限与账号 | 23 个权限位 + 自定义角色 · 注册管控 · 登录锁定 / IP 限流 / 改密全端下线 / 安全审计 |
| 运维与安全 | 三容器一条命令部署 · 秒级热替换发版 · HTTPS 部署包 · age 加密备份一键恢复 · ICP 备案号页脚 · 双主题（页脚切换） |

## 与主流 OJ 对比

SOJ 的定位是**班级与社团的教学场景**，与社区型/竞赛型 OJ 不完全同赛道。下表基于公开资料与主观使用归纳，仅供选型参考（各项目都在演进）：

| 维度 | SOJ | Hydro | HustOJ | QDUOJ | 洛谷（托管） |
| --- | --- | --- | --- | --- | --- |
| 自建部署 | ✅ 三容器一条命令，单机可跑 | ⚠️ 可自建，组件较多 | ✅ 老牌一键脚本 | ✅ docker-compose | ❌ 不可自建 |
| 教学闭环（作业/考试/成绩/排名） | ✅ 一体化，作业即时出分 | ⚠️ 有作业/比赛，教学非核心 | ⚠️ 基础作业，无 OI 赛制细节 | ❌ 纯比赛向 | ⚠️ 团队作业，定制受限 |
| 题型覆盖 | ✅ 传统 + 客观题族 + 函数 + 交互 | ✅ 较全 | ⚠️ 传统 + SPJ 为主 | ⚠️ 传统 + SPJ | ✅ 全 |
| 界面与主题 | ✅ 现代化 + 双主题切换 | ✅ 现代 | ❌ 陈旧 | ⚠️ 简单 | ✅ |
| 游戏化激励（等级/勋章/完成图） | ✅ | ❌ | ❌ | ❌ | ⚠️ 有等级/装扮 |
| 生态与社区 | ❌ 个人项目，生态小 | ✅ 插件与社区成熟 | ⚠️ 老牌但维护趋缓 | ⚠️ 维护低频 | ✅ 成熟 |
| 横向扩展 / 多租户 | ❌ 单站点设计 | ✅ 支持域与集群 | ❌ | ⚠️ 可加判题机 | ✅ 平台级 |

- **优势**：把「建题 → 作业 → 考试 → 成绩 → 激励」的班级教学闭环装进一个开箱即用的轻量部署；客观题族自动判分适合校内课程；十档难度 + 等级徽章 + 难度完成图对学生激励直观；运维面（热替换发版 / 加密备份 / HTTPS 包 / 安全基线）开箱齐备。
- **劣势（诚实版）**：单站点设计，无多租户与横向扩展（限流内存态、单 Mongo，多实例需自行换 Redis）；语言仅 C / C++14 / Python3；个人项目无插件生态与社区题库，大版本升级不保证兼容；讨论区无好友私信与自由广场（纯学习导向的挂载式讨论，v0.24 上线）。

## 快速启动

```bash
make up   # 自动先编译前端（缺 Node/Go 会自动装到 ~/.local/opt 免 sudo），再起 3 容器
```

打开 **http://localhost**（主机侧只暴露 80 端口），默认管理员 `root / sojroot123`（root），登录后即可体验完整判题流程。

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

推荐姿势：起好基础设施后开两个终端——`npm run dev` + `go run ./cmd/soj`，改前端代码保存即生效，无需任何构建。

## 编译与发版

脚本用本机工具链编译（缺 Go/Node 自动装到用户目录免 sudo，模块代理已内置），支持**热替换秒级发版**（前端嵌入二进制，docker cp 进容器即重启，无需重建镜像）：

```bash
make bin      # 前端构建 → 嵌入 → 热替换重启（前后端改动都走这一条）
make image    # 重建镜像并重启（首次部署或基础镜像更新时）
make lint     # ESLint + go vet（+ staticcheck 如已安装）
```

产物 `server/bin/soj` 是静态单文件（前端已嵌入），可直接拷到任何 Linux 服务器运行。

## 出题

三种方式：**网页建题**（表单写题面，zip 只收 `data/` 数据与配置）；**「导入题目」页批量导题包**（题库页入口，按来源选 SOJ / Hydro 适配器，单题包与多题包混装均可）；或目录式题包 `make import DIR=路径`（或放进 `data/seed/`——仅首次部署播种一次，之后启动不再播种，加题用导入或网页建题）。

统一 SOJ 题包格式（一个目录 = 一道题；导入页与 `make import` 同格式）：

```
7_我的题目/           # 目录名前缀数字仅为命名习惯，题号一律自动分配
├── problem.md        # 题面（唯一题面来源；样例直接写在正文里）
├── problem.yaml      # title / type / difficulty / tag（来源四字段已随双树退役，yaml 里的历史键被忽略）
│                     #   客观题另带 questions 小题定义（不含答案）
├── answer.yaml       # 〔客观题〕纯答案（single/judge=选项下标、multiple=下标列表、blank=每空可接受串）
├── answer.md         # 〔客观题〕解析（按「### 第 N 题」小节）
└── data/             # 数据与配置都在这里，文件名保持原样
    ├── config.yaml   # time / memory / filename(文件 IO) / subtasks / checker
    ├── test1.in / test1.ans、test2.in / test2.out   # 数据对（.ans 或 .out 均可）
    ├── gen.py / std.cpp        # 〔可选〕造数据脚本等附属文件，与数据对同目录
    ├── checker.cpp             # 〔可选〕SPJ（config.yaml 的 checker 声明）
    ├── driver.cpp / template.cpp   # 〔函数题〕驱动器与学生模板
    └── interactor.cpp          # 〔交互题〕交互器
```

`type` 缺省为传统题，可设 `objective` / `function` / `interactive`；`hidden: true` 设为主题库隐藏（适合考试题）。Hydro 官方「题库导出」zip 在导入页选「从 Hydro 导入」原样上传即可（题面 problem_zh.md、testdata/、config.yaml 自动识别，目录名数字 ID 不采用）。旧版格式（statement.md + cases/）不再支持，站内任意题「下载数据」导出的包即格式模板。

## 配置

全部走环境变量（见 `docker-compose.yml` 与 `server/internal/config/config.go`），常用的：`SOJ_MONGO_URI`（Mongo 连接串）、`SOJ_DATA_DIR`（测试数据根目录）、`SOJ_SANDBOX_ADDR`（沙箱地址）、`SOJ_JUDGE_WORKERS`（判题并发，默认 2）、`SOJ_JWT_SECRET`（不设置则首启自动生成随机密钥并持久化到密钥卷，多实例部署务必显式设置随机长字符串）、`SOJ_TRUSTED_PROXIES`（可信代理 IP/CIDR，逗号分隔，ClientIP 只从可信直连方解析 X-Forwarded-For；`none` = 不信任任何代理）。

## HTTPS 与域名上线

公网部署强烈建议 HTTPS（明文 HTTP 下学生密码与登录 token 在公网裸奔）。SOJ 应用本身保持纯 HTTP，TLS 在入口层用 Caddy 终止、证书走 Let's Encrypt DNS-01 自动签发续期；配套的 Caddyfile、反代与隧道参考配置和分步手册（子账号 → Caddy 安装 → 自测 → 切换日清单 → 回退）见 [deploy/https/](deploy/https/)。ICP 备案号在管理后台「系统设置」直接填写，页脚自动悬挂并链接工信部备案系统（留空不展示）。

## 备份与恢复

管理后台「备份」面板一键完成：mongodump + 测试数据打包 → age 加密 → 密文归档下载，服务器同步留存；命令行等价 `./scripts/backup.sh`（cron 可直接调）。恢复：

```bash
./scripts/restore.sh <归档> --pull-key        # Tier 0：私钥在服务器密钥卷
./scripts/restore.sh <归档> <私钥文件>         # Tier 1：用启用时一次性保存的私钥
```

恢复会覆盖现有库并还原测试数据卷，默认二次确认（`--yes` 跳过）。

## 长期运行巡检（v0.27）

磁盘是唯一会无声写满的资源：测试数据只增不减（题目无删除）、上传靠每用户配额兜底但无清理、容器日志已随 compose 统一轮转（json-file 10m×3）。磁盘写满的表象是全站 500（Mongo 先死）且判题回写丢失（磁盘恢复后自动重判），建议装上水位巡检：

```bash
./scripts/disk-patrol.sh --install-cron   # 每日 08:30 检查，超 85% 写告警进 backups/cron.log、退出码 2
```

`/api/health` 在数据库或沙箱任一不通时返回 **503**（健康 200，响应体不变），uptime 监控可直接按状态码报警；soj 容器自身 healthcheck 也走该端点。另两条长跑口径：**沙箱容器故障期间**的提交会批量落 System Error（队列不死锁），沙箱恢复后用后台「按题 / 按赛重测」回扫即可；**发版重启**后 Bootstrap 立即把遗留 Judging 重排队（无需等 120s 租约超时），在途判题自动重判，启动时还会清扫上次异常退出残留的导入 / 备份临时文件。

## 部署到服务器

开发机是唯一事实源，服务器只是运行副本——**代码一律在开发机改完再推**，不要直接改服务器上的文件（下次同步会被覆盖；万一两侧分叉，把服务器副本 rsync 拉回临时目录 diff 合并后再推）。同步两步：

```bash
rsync -a --delete --exclude node_modules --exclude .git --exclude backups --exclude .env \
  --exclude .mimosa --exclude .zcode --exclude docker-compose.override.yml \
  --exclude plan.md --exclude private_doc --exclude AGENTS.md --exclude web-design.md --exclude web-design-v3.md --exclude web-design-v3-full.md --exclude notes.md ~/soj 服务器别名:~/
ssh 服务器别名 'cd ~/soj && make up'
```

（`plan.md` / `notes.md` / `private_doc/` / `AGENTS.md` / 各设计文档（web-design*）只留开发机，不随部署上服务器；`--delete` 让开发机成为唯一事实源——服务器上被排除项（override / backups / .env 等）rsync 不会删，未排除又不在开发机的残留文件会被清掉。工具链自举的服务器注意：`make up` 前置的 `make check` 需要 go/npm，非交互 ssh 不走 `.bashrc` 的自举 PATH——Makefile 已内置 `~/.local/opt` 前置，无需手工加 PATH 前缀。）

**发版口径（v0.28 收紧）**：服务器例行发版一律 `make up`（先过全量门禁 lint/test/typecheck/build，再热替换并重建镜像，容器重建/宿主机重启不回退）；`make bin` 仅限开发机快速调试（不过门禁、不重建镜像）。每次构建都会把 `git describe` 版本注入二进制，`curl localhost/api/health` 的 `version` 字段可随时核对线上跑的是哪个提交。

建议封装成本地 `scripts/sync-to-server.sh`（**不入库**，各机自建：目标别名因机而异）——完成上面两步并做 `/api/health` 健康检查；`--no-build` 只同步不构建，`SOJ_HOST` 覆盖目标机。

服务器侧参数放 **`docker-compose.override.yml`**（compose 自动合并，rsync 已排除）：生产判题并发、Mongo 缓存上限，用 `ports: !override []` 清掉开发端口映射（27017/5050 不对外）。基础 compose 保持开发形态入库，两边互不干扰。最后一步 `--build` 让镜像与代码同步，宿主机重启后容器自愈不会回退到旧版本。

## 路线图

- **教学与管理基座**（v0.4–v0.8）：班级 / 作业 / 考试 / 主题库比赛、优先级判题队列、权限位与自定义角色、个人设置、加密备份与备份面板
- **题型扩展**（v0.9–v0.10）：题包 v2 · 客观题族 · 函数题 · 交互题 · 客观题 YAML 与试卷打印
- **激励与安全**（v0.11–v0.16）：十档难度 + 学生等级 + 炫彩徽章 · HTTPS 与域名接入 · 账号安全地基（改密全端下线 / 登录锁定 / IP 限流 / JWT / 安全审计）· 邮箱验证码与自助找回
- **题包与导入**（v0.15 / v0.18）：统一题包格式（problem.md + data/）· Hydro 包适配 · 独立导入页实时进度
- **全站重设计「青格蓝调」**（v0.19–v0.21）：24 屏像素级对标 · 等级 / 难度色轴与徽章特效链 · 网格画布与玻璃顶栏
- **体系化收敛**：双树体系（知识树 + 标签树）· 建题 / 编辑表单重设计 · 题解每人每题一篇 · 用户公开主页 · 题单章节化 · 全站主题双版
- **体验与稳定性批**（v0.22.x）：赛时隐藏难度与知识点 · 判题文件 IO 空输入根修 · 提交 / 编辑体验统一（CodePane 等）
- **竞技化**（v0.23）：挑战模式（限时手速 PK）· 沙箱自测 · 题内三榜 · 答案解析视频卡 · 资料逐字段公开开关 + QQ 头像
- **讨论与通知**（v0.24）：三大讨论区（学习分享 / 题内思路 + 代码分数门槛与剧透确认层 / 比赛赛时答疑升格）· 站内通知中心（铃铛 + 消息中心 + 发布/开赛提醒 + 全站广播）· 举报/禁言/采纳经验激励

逐版详细需求与验收记录维护在本地迭代文档（不入库），此处只保留里程碑概览。

## 许可证

以 [AGPL-3.0](LICENSE) 协议开源。架构设计仅参考主流开源 OJ 的公开思路、未复制其代码；随发行物分发的第三方组件（go-judge、Vditor、Element Plus 等）仍按各自原始协议提供。
