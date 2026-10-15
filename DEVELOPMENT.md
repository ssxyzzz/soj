# SOJ 开发文档

> 本文档是需求的唯一记录：每次需求讨论后更新对应章节并在「更新记录」加一行；与旧内容冲突时，以最新为准。

## 1. 更新记录

| 日期 | 内容 |
| --- | --- |
| 2026-10-15 | 需求与架构定稿（v2 轻量方案）。同日完成的初版骨架（MySQL + Redis + RabbitMQ，7 容器）方案废弃，仅代码留作实现参照 |
| 2026-10-15 | 权限设计调整：去掉 Hydro 的「域」概念，改为全局角色 + 「班级」实体（参考 QDUOJ group / 洛谷团队的常见做法），主页直接展示班级列表 |
| 2026-10-15 | 题目采用**引用实现**（讨论结论）：题目全局共享，班级与比赛通过引用关联题目、不各自建题；比赛可设隐藏，赛时隐藏提交记录与通过情况 |
| 2026-10-15 | **v0.2 换底盘开发完成并验收通过**：MongoDB（单节点副本集）、数据库原子领队判题（删除 RabbitMQ/Redis）、前端 embed 进 Go 二进制（删除 Nginx）、测试数据文件化 + 按需读取（LocalFS）、3 容器 compose、备份脚本；实测 C/C++/Python 判题全链路与备份。实现要点见 5.1 |
| 2026-10-15 | **v0.3 权限与班级开发完成并验收通过**：角色体系按最新设计落地（guest / default / admin / root）、班级实体（建班/邀请码/名单导入/退出/重置邀请码）、题目引用进班级、主页与班级页卡片、管理后台（建号/改角色/重置密码）。实测全链路通过，要点见 5.2 |
| 2026-10-15 | 可见性决策（讨论结论）：「仅本班成员可见」挂在**班级的引用**上（引用时打 hidden 标记，非成员在班级页看不到该条目）；**全局题库恒公开可见**，不做题目级门禁。已落地并验收（游客/成员/管理三视角）。题目级可见性（真藏题）如日后需要再议 |
| 2026-10-15 | 比赛锁题模型定稿（v0.4 需求讨论）：新增题库级「题目隐藏」（default 恒不可见不可交）；主题库比赛（root 创建）全局锁题、所有入口指引到比赛页；班级比赛只对本班学生隐藏与指引，多班同题不冲突不校验；防抄旧代码（default 赛前提交对其本人隐藏）；隐藏期三恢复（题目/榜单/历史代码）；比赛提交延迟计入总榜。详见第 5 章「题目」「比赛」节 |
| 2026-10-15 | **v0.4 比赛开发完成并验收通过**：比赛实体（主题库/班级双作用域）、OI 赛制（赛时不出分/子任务部分分/并列排名）、锁题与指引、隐藏期三恢复、延迟计榜、default 限速（root 可调）、班级多管理员。全流程实测：办赛→锁题→赛时提交不出分→自动发布出榜→恢复，全部符合定稿。要点见 5.3 |

## 2. 项目定位

- 中小型、轻量化 OJ：班级 / 社团 / 小机构使用，几十人常态、上限几百人。
- 目的：自己写 + 学习 + 小机构实用 + 开源，要求稳定可靠。
- 不做：万人并发、微服务、重型中间件。

## 3. 需求汇总（讨论结论）

| 项 | 结论 |
| --- | --- |
| 部署路径 | 先本机 WSL2，后上云服务器 |
| 必要功能 | 核心判题、用户权限管理、比赛/作业 |
| 权限系统 | Hydro 式权限位 + 全局角色；不用 Hydro 的「域」，改用直白的「班级」实体并在主页展示 |
| 比赛 | 先做 OI 赛制（赛时不出分，赛后按总分排名） |
| 数据库 | MongoDB（不再用 MySQL） |
| 评测数据 | 按需抓取：库只存元数据，判题时逐测试点读取 |
| 部署 | 保持 Docker（compose） |

## 4. 技术选型

| 模块 | 选择 |
| --- | --- |
| 前端 | Vue 3 + TypeScript + Vite + Element Plus，构建产物嵌入 Go 二进制 |
| 后端 | Go 1.26 + Gin，单进程 |
| 数据库 | MongoDB（单容器单节点**副本集**） |
| 判题调度 | 库内原子领取（findOneAndUpdate），无消息队列 |
| 缓存/排行 | 进程内缓存（TTL 30s），无 Redis |
| 沙箱 | go-judge（REST），安全底线不可省 |
| 部署 | docker compose 三容器：soj / mongo / sandbox；上云需要 HTTPS 时加 Caddy |

选型要点：

- OJ 业务（题面、比赛配置、逐点判题结果）天然是嵌套文档，比关系表贴合；配置常演进，无 schema 改动代价小。
- 数据全落 MongoDB 一处，**备份系统 = mongodump + 测试数据目录**。
- 事务只在副本集可用，因此 mongo 按单节点副本集跑；判题领取是单文档原子操作，standalone 下也安全，整体设计尽量不用多文档事务。

## 5. 系统架构

```
浏览器 ──> SOJ 主程序（Go 单进程，容器 soj）
             ├─ 托管前端页面（embed）
             ├─ HTTP API（登录/题库/提交/比赛/排行/管理）
             └─ 判题线程池（2~4 并发，默认与 Web 同进程；保留 --judge-only 独立模式）
                     │ 原子领取 Pending 提交
                     ├─> MongoDB（唯一数据库）
                     └─> go-judge 沙箱（localhost:5050）
                             └─ 按需读取 /data/testdata/<pid>/ 测试数据
```

**判题调度**：线程循环 `findOneAndUpdate(submissions, {status: Pending}, {status: Judging, judgeId}, sort _id)` 原子领取 → 评测 → 回写。多线程/多进程不会重复领取；单线程 panic 只标记该提交 System Error，不影响服务。

**权限系统（全局角色 + 班级）**：

- **全局角色（决定能做什么）**：权限位（int64 位标志）挂全局角色，不做 Hydro 的「域」。初始角色：
  - guest（访客）：看公开题；
  - default（默认权限）：提交代码，可加入班级；
  - admin：+建题/改题/建班/管班/办赛/看本班全部提交/重测；
  - root：全部（建用户、封禁、全局管理）。
- **班级（决定能看到什么）**：参考 QDUOJ group / 洛谷团队的常见设定。属性：名称、管理员、学生、加入方式（邀请码 / 管理员导入名单）。
  - 题目、比赛归属某个班级，可见性两档：**公开（所有人）/ 仅本班成员**；
  - 可见性落点（2026-10-15 决策）：题目的「仅本班」挂在**班级引用**上（只影响班级页展示，全局题库恒公开）；比赛的隐藏见「比赛」节；
  - 管理员在自己班里布置练习、举办比赛，学生只在主页看到自己所在班级的内容；
  - 主页**直接展示班级卡片列表**（班级名、简介、学生数、题数、进行中的比赛），未登录显示公开班级；点进班级页看其题目 / 比赛 / 学生 / 排行。
  - 班级的练习题目和比赛题目直接引用主题库的题目，不能新建题目。
- 判定规则：操作类权限查全局角色；查看类先看资源可见性——公开直接放行，非公开则要求 admin / 资源所属班级成员。

**题目**
- **题目隐藏**：题目如果在主题库设置隐藏，则始终隐藏，default无法看到此题目，也无法递交。
  - （2026-10-15 定稿）设置权限：root 可设任意题；admin 只能设**自己导入**的题（题目全局共享，防止互相打架）；
  - 被主题库比赛引用时，赛中临时解锁可见（见「比赛」节）。


**比赛（OI 赛制） 和 练习**：

- 赛制：赛时正常评测但选手看不到得分与状态（只显示提交次数）；每题满分默认 100，按测试点部分分累计；赛后统一出榜，按总分降序。
- **给分粒度（已定 2026-10-15）**：默认单测试点等分；problem.yaml 可选配置子任务分组（一组测试点全过才得该组分），两套判题逻辑都实现。
- **并列排名（已定 2026-10-15）**：总分相同比「最后拿到当前分数的提交时间」，早者靠前；再相同并列同名次。
- **提交限制（已定 2026-10-15）**：比赛期间每题不限次；所有 default 角色用户提交全局限速 **1 分钟 3 次**（练习与比赛通用），上限值 root 后台可改；admin/root 不限速。
- **时长与隐藏期（已定 2026-10-15）**：比赛时长最长 **7 天**；可单独设赛后隐藏期（最长 **7 天**），隐藏期内榜单与提交记录继续不可见，到期自动公开。
- **主题库比赛**（root 创建，2026-10-15 定稿）：在主题库设置的比赛题目，比赛开始前隐藏，比赛中可见。所有布置该题的界面（题库 / 班级引用 / 题单 / 题目详情）都将指引到此比赛界面做，其他界面与提交接口一律拒绝——**全局唯一权威**，后续任何引用都指向它。同时隐藏 default 查看自己之前时刻做的代码情况（持续到隐藏期结束）。面向全体登录用户开放，无需报名。
- **班级比赛**（建班人创建，2026-10-15 定稿）：如果在班级内设置的比赛题目，则对该班级学生比赛开始前隐藏比赛中可见，该学生如果回到主题库查找，也是指引到此班级比赛界面做，其他界面不可提交。**只影响本班学生**：其他班与游客完全不受影响、照常做题、无需指引——因此同一题被多个班的比赛同时引用**不冲突，不做重叠校验**。防抄旧代码规则同上，但只对本班参赛学生生效。
- **赛后恢复（已定 2026-10-15）**：隐藏期（可选，≤7 天）内题目继续锁定、榜单与提交记录继续隐藏、历史代码继续隐藏；隐藏期结束三者一起恢复。
- **全局排行榜（已定 2026-10-15）**：比赛提交不实时计入解题数，隐藏期结束统一并入（防止赛中靠解题数 +1 推测成绩）。

- 练习：即班级引用题目随时做、即时出分（现状，不新增实体）；带截止时间的作业以后有需要再加。
- 参赛范围（2026-10-15 定稿）：主题库比赛面向全体登录用户（无需报名）；班级比赛仅本班成员可参赛。
- 榜单：聚合管道按 (contest, uid, pid) 取最高分求和，进程内缓存，管理端可强制刷新；赛时选手不可见榜单。
- 班级多管理员随 v0.4 一起实现（classmembers 增加角色字段）。

**评测数据按需抓取**：

- 库内只存元数据（文件清单、大小、sha256、子任务结构），本体在文件卷 `/data/testdata/<problemId>/`（problem.yaml + cases/N/in、N/ans）。
- 判题时逐测试点流式读取，比完释放；读取层抽象为 `StorageProvider` 接口，v0.2 实现 LocalFS，日后可换 S3/MinIO。
- 好处：库不膨胀、大题无压力、备份简单。

**MongoDB 集合草案**：

| 集合 | 要点 | 索引 |
| --- | --- | --- |
| users | uname, pwdHash, nickname, mail | uniq uname |
| classes | name, intro, ownerId, joinMode | ownerId |
| classmembers | classId, uid, joinedAt | uniq (classId, uid), (uid) |
| problems | 全局共享（引用实现），title, statement(md), limits, visibility, testdata 元数据, tags；班级/比赛持引用不持有题目 | — |
| submissions | uid, pid, contestId?, lang, code, status, score, timeMs, memKb, detail[], judgedAt | (status,_id), uid, pid, (contestId,uid) |
| contests | classId, type, title, start, end, problems[{pid,score}]（引用）, policy | (classId,start) |

排行榜不建集合：实时聚合 + 进程缓存。

**v0.1 代码处置**：保留 judge / sandbox / lang / auth / server 框架 / 前端全部页面；删除 mq 与 Redis；model 层由 MySQL 裸 SQL 重写为 Mongo repository。

### 5.1 v0.2 实现要点（已完成，当前代码即此形态）

- **入口**：唯一二进制 `cmd/soj`。默认 Web+判题同进程；`--judge-only` 纯判题进程；`-import <dir>` 导入题目后退出。启动引导幂等：建索引、users 为空时创建 admin/admin123、遗留 Judging 重置排队、按 `SOJ_SEED_DIR` 自动导入种子题（`data/seed/` 两道示例题）。
- **数据层** `internal/repo`：counters 集合原子取号（uid/pid/sid 自增）；`ClaimSubmission` 用 `findOneAndUpdate(Pending→Judging)` 领队；排行 = 聚合管道（Accepted 按 uid+pid 去重计数 + $lookup 用户）+ 服务端 30s 缓存。
- **题目导入** `repo.Importer`：题目目录 = `problem.yaml`（title/时限/内存/samples）+ `statement.md` + `cases/N/in|ans`，目录名前缀数字即 pid；元数据（清单/大小/sha256/样例标记）入库，文件本体复制进 testdata 存储。
- **存储层** `internal/testdata`：`Provider` 接口 + LocalFS 实现（`<root>/<pid>/<rel>`，防路径穿越）；判题时逐点按需读取。换 S3/MinIO 只需新增实现。
- **前端 embed**：`internal/webui`（`go:embed all:dist`），SPA 回退在 Gin NoRoute 实现；Docker 构建经 compose `additional_contexts` 把 web 目录传入 server 多阶段 Dockerfile。
- **compose**：mongo（单节点副本集，健康检查里自动 rs.initiate）/ sandbox（特权）/ soj，站点入口 `http://localhost:8080`。
- **验收记录**：compose 一键起；admin 登录 → C/C++/Python 提交；AC（100 分）、WA（报首个失败测试点）、RE（段错误退出状态 11）均判定正确；排行按题去重（8 次提交 2 道题 → solved=2）；`make backup` 产出 mongo.archive.gz + testdata.tar.gz。

### 5.2 v0.3 实现要点（已完成，当前代码即此形态）

- **角色**：`users.role` ∈ default / admin / root（guest 即未登录），与第 5 章权限设计一一对应。`RequireRole(...)` 中间件挂路由组：建班与班级管理需 admin+，`/admin` 需 root。JWT 带 role，改角色后需重新登录生效。注册用户默认 default。
- **存量迁移**：v0.3 之前的 user/teacher/admin 体系经两阶段映射（先变 __tmp_* 再转正，防止 teacher→admin 被 admin→root 重复提升）迁移到新体系；meta 集合打标记保证只跑一次。
- **班级**（repo/class.go + server/class.go）：classes（含 Problems []pid 引用列表、8 位去混淆邀请码）+ classmembers（冗余 uname）。建班人自动入册；邀请码加入幂等；建班人不可退出；邀请码可重置；名单导入按用户名批量（不存在者跳过）。v0.3 先实现单建班人，"班级可有多管理员"留到 v0.4 一起做。
- **可见性**：班级列表公开、详情公开（引用题目任何人可看）；成员列表与邀请码仅建班人/root；可见性字段已预留，v0.3 实际全部 public（两档可见性随 v0.4 比赛落地）。
- **管理后台**（server/admin.go）：用户分页（不含密码哈希）、建号（指定角色）、改角色/昵称/重置密码；前端 /admin 按 localStorage 的 role 做入口守卫，后端 RequireRole 才是真闸门。
- **前端新页面**：首页（技术栈标签更新为当前架构 + 班级卡片）、/classes（列表 + 建班 + 邀请码加入）、/classes/:id（详情/成员/引用题目管理/名单导入）、/admin（用户管理）。
- **验收记录**：root 建 admin 角色教师 → 建班得邀请码 → default 学生凭码加入（错误码被拒）→ 学生建班与访问管理接口均 403 → 教师引用题目 → 班级卡片计数正确 → root 改角色/建号正常；存量角色迁移后边界（admin 可建班、default 不可）复验通过。
- **引用可见性（决策后补）**：classes.problems 由 []pid 升级为 []{pid, hidden}（含一次性迁移）；添加引用时可勾选「仅本班成员可见」，非成员在班级详情里看不到该条目（成员与管理员可见并带标记）；全局题库不受影响。三视角验收通过。

### 5.3 v0.4 实现要点（已完成，当前代码即此形态）

- **比赛实体**（repo/contest.go + server/contest.go）：contests 集合，ClassID=0 为主题库比赛（root 创建、全体登录用户参赛），否则班级比赛（classManageable 创建、本班成员参赛）。约束：时长 ≤7 天、赛后隐藏期 ≤7 天（hiddenUntil）。创建/删除（仅未开始）/列表/详情 API。
- **锁题评估**：`loadLockMap` 每次请求按视角评估未发布比赛（量小）→ map[pid]lock{Phase: scheduled/running/hidden, Bank}。生效范围：主题库比赛对 root 外所有人、班级比赛对本班成员；主题库锁优先。效果：赛前与隐藏期→题目 404「暂不可见」+练习提交拒绝；赛中→练习提交拒绝并指引「请从比赛页面提交」（前端题库/题目详情/班级列表均渲染锁标记与跳转），仅比赛页接口接受提交。
- **防抄旧代码**：default 视角下，被锁题目的赛前提交（created_at < 比赛 start）详情不返回 code（masked）。
- **赛时不出分**：比赛提交走 `/contests/:id/problems/:pid/submissions`，submissions 带 contestId + published=false；`/contests/:id/submissions/mine` 赛时仅返回提交记录（无状态得分），组织者/root 或发布后返回完整。
- **发布对账**：主循环每 30s `PublishDueContests`——隐藏期结束的比赛标记 published 并把其比赛提交批量置 published。发布时三恢复（题目解锁/榜单可见/历史代码可见）+ 比赛提交计入总榜。
- **榜单**：`ContestLeaderboard` 内存计算（比赛规模小）——每 (uid,pid) 取最高分、总分降序、最后得分提交时间早者靠前、再并列；进程内缓存（进行中 30s，已发布永久）。赛时/隐藏期仅组织者与 root 可见。
- **子任务计分**：problem.yaml 可选 `subtasks: [{cases:[..], score:..}]`（导入校验：点存在、分值和=100）；判题器 evaluateSubtasks——组内全过才得分、组内失败跳剩余点继续下一组、总分 100 判 AC 否则以首个失败点状态为整体状态；无配置走单点等分（历史逻辑）。
- **题目隐藏**：problems.hidden + createdBy（导入者，命令行导入为 0）；default/guest 列表与详情不可见、提交按「题目不存在」拒绝；主题库比赛赛中临时解锁；PATCH /problems/:id/hidden（root 任意题、admin 仅自己导入的题）。
- **限速**：default 角色全部提交 1 分钟 N 次（滑动窗口内存实现），N 存 settings 集合，root 经 /admin/settings 读取与修改（默认 3，1-60）。
- **班级多管理员**：classmembers.role ∈ owner/admin/member（含一次性迁移）；classManageable = 建班人 ∨ 班级管理员 ∨ root，班级管理接口（引用题目/邀请码/导入成员/成员角色/办班赛）均按它判定，不再要求全局 admin 角色。
- **已知修复**：设置更新缺 upsert 导致静默无效（实测发现）；比赛锁题下全局排行聚合按 published 过滤，普通提交恒 published。
- **验收记录**（脚本全过）：root 办主题库赛→default 全站锁题被指引→赛时提交不出分（自己只见次数、榜单 403、root 可见、总榜不并入）→2 分钟后自动发布→榜单公开含自己、练习恢复可交、赛前旧代码恢复可见；班级比赛只锁本班（他班与无班用户照常练习）；赛前隐藏对本班生效、他人无感；子任务溢出题 WA 40 分/long long AC 100；隐藏题 default 404、root 可见、admin 改他人导入题被拒；限速默认 3 次/分、root 改 10 生效；班级管理员（全局 default）可管理班级、外人 403。

## 6. 部署运维

- 本地（WSL2）：`docker compose up -d --build` 三容器；mongo 单节点副本集；测试数据挂载卷。
- 上云：三容器原样搬迁；域名 HTTPS 加 Caddy 容器；1~2G 内存云机够用。
- 备份：cron 每日 `mongodump` + `rsync /data/testdata`。
- 升级：换 soj 镜像重启即可；数据演进靠程序内兼容逻辑（无强 schema）。
- 国内网络（Dockerfile 已内置，无需操作）：Go 构建默认 goproxy.cn、沙箱镜像内已换清华 apt 源；基础镜像可经 daocloud 拉取后 tag 回标准名，例外：`criyle/*` 命名空间 daocloud 不代理，走 `hub.rat.dev`。

## 7. 关键技术事实（go-judge 沙箱，实测确认）

- `POST /run` 请求 `cmd[]`（args/files/copyIn/copyOut/copyOutCached + 资源限制），响应为结果数组。
- `status` 实测取值：`Accepted`（退出码 0 且未超限）/ `Time Limit Exceeded` / `Memory Limit Exceeded` / `Output Limit Exceeded` / `Nonzero Exit Status`（注意拼写是 Nonzero） / `Signalled` / `File Error` / `Dangerous Syscall` / `Internal Error`。
- `time` 纳秒、`memory` 字节；输出在 `files` map，文本明文、二进制才 base64，解码函数需兼容两者。
- **files 数组语义（实测）**：含 `content` 的条目会被物化为绑定 fd 的文件；仅 `name+max` 的条目才是内存收集管道。stdin 必须用前者（空输入也要显式 `"content":""`，空对象返回 400），stdout/stderr 必须用后者，否则输出捕获失效。
- 编译产物 `copyOutCached` 得 fileIds，运行时 `copyIn: {fileId}` 复用，用完 `DELETE /file/<id>`。
- 沙箱 bind-mount 镜像自身 /usr，编译器必须装在沙箱镜像内（官方镜像不带 gcc）。

## 8. 迭代路线

| 版本 | 内容 | 验收 |
| --- | --- | --- |
| v0.2 换底盘 ✅ | MongoDB、原子领队判题、删 MQ/Redis、前端 embed、测试数据文件化按需读取、3 容器 compose | 已完成并验收（见 5.1） |
| v0.3 权限与班级 ✅ | 全局角色权限 + 班级（建班/邀请码/名单导入）+ 主页班级展示 + 管理界面，题目采用引用实现，班级不可建新题。 | 已完成并验收（见 5.2） |
| v0.4 比赛 ✅ | OI 赛制 + 榜单，比赛题目也采用引用实现，如设定隐藏（无论主题库还是班级内），则赛时隐藏提交记录和通过情况。设定隐藏的时限最高为7天。 | 已完成并验收（见 5.3） |
| v0.5 体验 | SSE 实时判题状态、数据上传（zip）管理界面、Hydro/QDUOJ 题目导入、重测 | 纯界面完成出题到办赛 |

每版保持可部署可用再进入下一版。

## 9. 待定事项

1. 测试数据上载方式：界面传 zip 与目录直放，先做哪个（倾向界面，v0.5 落地）。
2. License：MIT 或 AGPL（仅参考 Hydro 设计、不抄代码则无 AGPL 传染问题）。
3. Java/Go 等语言扩展时机（镜像与 lang 定义已留扩展位）。
4. 防作弊（代码相似度、多账号）初期不做，记录在案。

已在 2026-10-15 讨论定稿并移入第 5 章的旧待定项：OI 并列排名、子任务粒度、提交限次、隐藏 7 天含义。
