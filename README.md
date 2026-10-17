# SOJ — 在线判题系统

面向班级与社团的轻量级 Online Judge：一个 Go 程序 + MongoDB + go-judge 沙箱，三个容器就是一个完整的 OJ。
支持班级作业（即时出分）、班级考试（OI 赛制）、主题库比赛、解题排名；权限采用**可配置角色 + 权限位**（对标 Hydro 的权限位/角色解耦、按单站点简化），root 可在管理后台自定义角色。
设计决策与实现记录见 [notes.md](notes.md)，排期计划见 [plan.md](plan.md)（均为本地文档，不入库）。

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

- **题库**：题目全局共享；支持主题库隐藏（普通用户不可见不可交）、子任务分组给分（problem.yaml 可选）；**比对模式**每题可配——全文比对 / 宽容比对（token 化 + 浮点容差）/ 自定义校验器 checker（C++，退出码判定，答案不唯一的题也能出）；题包可选 `config.yaml`（时限/内存/比对/子任务/校验器白名单配置，未知字段忽略）；比赛锁题期间全站指引到赛事页提交。
- **文件 IO**：考试/比赛可勾选「本场启用文件 IO」——场内题目按题目配置的文件名（缺省 problem.in/problem.out）以文件读写判题（NOI 惯例，stdout 不参与比对），题面与在线编程页显示文件名提示；题库/练习/作业恒为标准输入输出。
- **班级**：老师建班（可选公开/私密，私密班仅成员可见）、邀请码/名单导入拉学生；班级作业（题目清单，随时做、即时出分，老师可看每人各题完成情况）；班级考试（OI 赛制：考时不出分只记次数，考后出榜，支持赛后隐藏期）；班级多管理员。
- **比赛**：主题库比赛面向全体登录用户；与考试共用锁题规则。
- **排名**：解题数排行（比赛提交在隐藏期结束后才计入，防赛中推测成绩）。
- **权限**：15 个权限位 + 可配置角色——内置 default（学生）/ admin / root，root 可建自定义角色（如备份员、出题老师）；root 为代码级超角色；导航、路由与接口全按权限位判权，降权即时生效；班级内部两级（班级管理员/成员）独立不动。
- **网页建题**：持有「新建题目」权限位者在网页直接建题——题面富文本（粘贴即传图）、时限/内存、测试数据 zip 上传（支持 config.yaml）、比对模式；**编辑题目复用同一表单**，测试数据支持整包替换（旧测试点全删重建，样例章节自动重生成）；**代码编辑器**：提交弹窗与在线编程均为 CodeMirror 6（语法高亮/行号/自动缩进/括号匹配，内置 C++/Python 常用函数补全表，字体字号可调即改即存）；题解区满分开放（多作者，记录作者与时间，支持置顶）。
- **个人设置**：真实姓名（仅管理员后台维护，班级名单对号）+ 性别/学校/年级/QQ（仅自己可见）+ 偏好（默认判题语言、按语言的代码模板、Markdown 编辑器初始模式、代码编辑器字体/字号/自动补全开关），提交弹窗/在线编程/编辑器自动预填。
- **防刷**：提交限速 1 分钟 3 次（持「提交不限速」权限位者豁免，上限 root 后台可调）。
- **性能与安全**：响应 gzip 压缩；带哈希的静态资源一年期 immutable 缓存、其余每次回源校验；Vditor 懒加载分包不拖累首屏；题面/说明 256KB 上限（服务端拦截 + 查看端拒渲染）+ 渲染后 DOMPurify 消毒。

## 快速启动

```bash
make up   # 自动先编译前端（缺 Node/Go 会自动装到 ~/.local/opt 免 sudo），再起 3 容器
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

## 编译与发版（本机 Node/Go 工具链）

脚本用本机工具链编译（缺 Go/Node 时自动安装到用户目录 `~/.local/opt/` 免 sudo，见 `scripts/toolchain.sh`；模块代理 goproxy.cn / npmmirror 已内置），每步实时显示「计时 + 最新日志」，全量日志落 /tmp（失败自动展示末 20 行）：

```bash
./scripts/web-building.sh      # 前端全量构建 → 嵌入二进制 → docker cp 热替换重启（秒级发版）
./scripts/server-building.sh   # 编译后端 → 热替换进容器并重启（秒级，需先跑过前者）
make bin                       # 等价于上面两步连做
make image                     # 重建 soj 镜像并重启容器（让镜像内版本追上热替换版本）
```

发版节奏：前后端改动都走热替换秒级发版。镜像仅在首次部署、容器不在运行（web-building 自动兜底）或基础镜像更新时重建——Dockerfile 不再内置前端构建（直接消费宿主机 `web/dist`），Go 阶段挂 BuildKit 缓存，重建也是秒级（实测前端变更后 `make image` 约 6 秒）。
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
- `hidden: true` 设为主题库隐藏（普通用户不可见，适合做考试题）；
- 可选 `config.yaml`（与 zip 建题共用解析，白名单制、未知字段忽略，config 优先于 problem.yaml）：`time`（秒）/`memory`（MB）/`compare`（exact/tolerant）/`float_tolerance`/`subtasks`/`samples`/`checker`/`file_io*`；测试点支持平铺布局（根目录 `1.in 1.ans`），自动归一为 `cases/1/` 结构；zip 建题/整包替换还容忍顶层单目录包裹（整个包多套一层文件夹，如下载的数据包直接上传）与 macOS 打包杂物（`__MACOSX/`、`.DS_Store`），解压后自动下钻/忽略；
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
| SOJ_UPLOADS_DIR | 题面图片目录 | ./data/uploads |
| SOJ_BACKUP_DIR | 备份密文归档目录（服务器留存） | ./data/backups |
| SOJ_KEYS_DIR | 备份密钥卷目录（与数据卷分离） | ./data/keys |
| SOJ_SEED_DIR | 启动时自动导入的种子题目录 | （空） |
| SOJ_SANDBOX_ADDR | go-judge 地址 | http://127.0.0.1:5050 |
| SOJ_JUDGE_WORKERS | 判题并发数 | 2 |
| SOJ_JWT_SECRET | JWT 密钥（生产必改） | soj-dev-secret |

## API 一览（/api 前缀）

「权限位」列均为可授予角色的权限位（root 恒通过）；无标注的按登录/公开/属主判定。

| 方法 | 路径 | 说明 | 权限 |
| --- | --- | --- | --- |
| GET | /health | 健康检查（db / sandbox） | 公开 |
| POST | /auth/register · /auth/login | 注册 / 登录（JWT 仅含 uid/uname） | 公开 |
| GET | /me · /me/classes · /me/profile | 当前用户（含权限位）/ 我的班级 / 个人信息与偏好（真实姓名只读，性别/学校/年级/QQ/偏好本人可改） | 登录 |
| GET | /problems · /problems/:id | 题库列表 / 详情（按视角过滤隐藏与赛事锁题） | 公开 |
| GET | /problems/:id/submissions | 他人代码（该题满分解锁） | 登录 |
| GET | /problems/:id/my-submissions | 我在该题的提交 | 登录 |
| PATCH | /problems/:id/hidden · /problems/:id/statement | 设置隐藏 / 编辑题面 | problem.edit.all，或 problem.edit.own 限自己导入的题 |
| POST | /problems | 网页建题（problem.yaml + 题面 + 测试数据 zip，支持 config.yaml） | problem.create |
| PATCH | /problems/:id/meta | 编辑题目信息（标题/时限/内存/比对模式/文件 IO 名） | problem.edit.all，或 problem.edit.own 限自己的题 |
| POST | /problems/:id/testdata | 测试数据整包替换（旧测试点全删重建，zip 内 config.yaml 生效） | 同上 |
| POST/DELETE | /problems/:id/checker | 挂载 / 移除自定义校验器（C++，判题懒编译） | 同上 |
| GET/POST/PATCH/DELETE | /problems/:id/solutions[/:sid]（/pinned） | 题解列表（分页置顶优先）/ 发表 / 编辑删除自己的 / 置顶管理；可见门槛 = 该题满分 | 满分用户读写自己；置顶/删他人需 solution.manage |
| POST | /submissions | 提交代码（无 submission.rate_exempt 位者限速 1 分钟 3 次） | 登录 |
| GET | /submissions | 提交记录总表（含隐藏期赛事提交） | submission.view.all |
| GET | /submissions/:id | 提交详情（代码：本人/权限位恒可，他人满分解锁；隐藏期赛事提交仅组织者与持位者） | 公开（按身份返回） |
| POST | /uploads | 题面图片上传（多文件；png/jpg/gif/webp ≤5MB/张，每人累计 200MB） | problem.edit.all/own 或 admin.settings |
| GET | /uploads/:file | 题面图片公开读取（随机名 + immutable 长缓存） | 公开 |
| GET | /rank · /notice | 解题排名（30s 缓存）/ 首页公告 | 公开 |
| GET | /classes · /classes/:id · /classes/:id/assignments | 班级列表 / 详情 / 作业列表（私密班三视角过滤） | 公开 |
| POST | /classes | 创建班级 | class.create |
| POST | /classes/:id/join · /classes/join-by-code · /classes/:id/leave | 加入 / 邀请码加入 / 退出 | 登录 |
| PATCH | /classes/:id/private | 切换公开/私密 | 建班人 / root |
| POST | /classes/:id/invite · /classes/:id/members | 重置邀请码 / 名单导入 | 班级管理员（建班人/root） |
| POST | /classes/:id/members/:uid/role | 设置班级成员角色 | 班级管理员（建班人/root） |
| POST | /classes/:id/assignments · /classes/:id/exams | 布置作业 / 发起考试 | 班级管理员（建班人/root） |
| GET | /assignments/:id · /assignments/:id/progress | 作业详情 / 完成情况 | 成员 / 班级管理员 |
| POST/PUT/DELETE | /assignments/:id/problems[/:pid] | 作业加题 / 整体保存（拖动排序）/ 移除题 | 班级管理员（建班人/root） |
| DELETE | /assignments/:id | 删除作业 | 班级管理员（建班人/root） |
| GET | /contests · /contests/:id | 主题库比赛列表 / 详情 | 公开 |
| POST | /contests | 创建主题库比赛 | contest.bank.create |
| PATCH/DELETE | /contests/:id（/intro） | 说明编辑 / 删除比赛（仅未开始） | 组织者（creator）/ root |
| PATCH | /contests/:id/file-io · /exams/:id/file-io | 「本场启用文件 IO」场景开关（中途可改，只影响之后的提交） | 组织者（creator）/ root |
| GET | /contests/:id/problems/:pid/submissions?uid= · /exams/:id/problems/:pid/submissions?uid= | 榜单下钻：某用户此题全部提交（含代码与测试点明细） | 组织者（creator）/ submission.view.user |
| POST | /contests/:id/problems/:pid/submissions | 比赛提交（赛时唯一通道，不出分） | 参赛者 |
| GET | /contests/:id/submissions/mine · /contests/:id/leaderboard | 比赛我的提交 / 榜单 | 参赛者（赛时榜单仅组织者） |
| GET | /classes/:id/exams · /exams/:id · /exams/:id/submissions/mine · /exams/:id/leaderboard | 考试列表 / 详情 / 我的提交 / 榜单 | 本班成员（榜单按赛制门禁） |
| POST | /exams/:id/problems/:pid/submissions | 考试提交（考时唯一通道，不出分） | 本班成员 |
| PATCH/DELETE | /exams/:id（/intro） | 说明编辑 / 删除考试 | 组织者（creator）/ root |
| GET/POST/PATCH | /admin/users[/:id] | 用户管理（搜索 / 建号 / 改角色 / 重置密码 / 真实姓名） | admin.user.manage |
| GET/PUT | /admin/settings | 系统设置（限速 / 公告） | admin.settings |
| GET | /admin/roles | 角色清单 + 权限位目录 | admin.user.manage |
| POST/PATCH/DELETE | /admin/roles[/:id] | 角色增删改（不可下放） | root |
| GET | /admin/backup | 备份状态（模式 / 上次备份时间） | backup.run |
| POST | /admin/backup/run · /admin/backup/offline-key | 一键备份（密文归档流下载 + 服务器留存）/ 升级 Tier 1 离线私钥（响应一次性，no-store） | backup.run / backup.keys |

## 备份与恢复

管理后台「备份」面板一键完成：mongodump + 测试数据卷打包 → age 加密 → 密文归档下载（`soj-backup-*.v1.tar.age`），服务器同步留存一份。**下载的永远是密文**——误传网盘、丢 U 盘、浏览器缓存都无害。

密钥两级模式（状态存于服务器密钥卷，与数据卷分离）：

| 模式 | 私钥位置 | 防护 |
| --- | --- | --- |
| Tier 0（默认，零操作） | 服务器密钥卷（首次备份自动生成） | 防备份文件单独泄露；防不了整机被入侵 |
| Tier 1（推荐升级） | 仅启用时一次性展示，服务器只存公钥 | 防整机入侵读历史备份；**私钥丢失 = 加密备份永久无法恢复** |

命令行等价（cron 可直接调用）：

```bash
./scripts/backup.sh    # 走服务端同一加密模块，产物复制到 ./backups/
```

恢复（解密在一次性容器内完成，宿主机无需安装 age）：

```bash
./scripts/restore.sh <soj-backup-*.v1.tar.age> <私钥文件>   # Tier 1：用启用时保存的私钥
./scripts/restore.sh <归档> --pull-key                       # Tier 0：私钥从服务器密钥卷取
./scripts/restore.sh <旧版明文备份目录>                       # 兼容 v0.4.9 之前的目录式备份
```

恢复会 `mongorestore --drop` 覆盖现有库并还原测试数据卷，默认二次确认（`--yes` 跳过）。

## 路线图

v0.4.1 作业与管理入口 ✅ → v0.4.2 班级考试 ✅ → v0.4.3 代码可见性 ✅ → v0.4.4 富文本 Vditor ✅ → v0.4.5 上云前性能与安全（gzip/缓存/分包 + 长度上限/渲染消毒 + 编译部署改造）✅ → v0.4.6 全站两列布局改造（PageShell/提交弹窗/在线编程/班级公开私密）✅ → v0.4.7 权限位与可配置角色（单域 RBAC + JWT 降权即时生效）✅ → v0.4.8 个人设置（资料 + 偏好联动）✅ → v0.4.9 备份加密与备份面板（Tier 0/1 + age 密文归档）✅ → v0.4.10 管理后台改版（角色矩阵/用户单表搜索）✅ → v0.5a 题面图片上传 ✅ → v0.5b 网页建题/比对扩展（宽容比对 + checker + config.yaml）/多作者题解/文件 IO 场景开关，三步全 ✅ → v0.5d 代码编辑器升级（CodeMirror 6 + 补全 + 字体字号）✅；下一批 v0.5c 老师教学闭环（成绩导出/题目统计/已通过列）；题包导入导出、标签等暂缓。
实现记录见 [notes.md](notes.md)，排期与待对齐事项见 [plan.md](plan.md)（均为本地文档，不入库）。

## 许可证

以 [AGPL-3.0](LICENSE) 协议开源。架构设计仅参考主流开源 OJ 的公开思路、未复制其代码；随发行物分发的第三方组件（go-judge、Vditor、Element Plus 等）仍按各自原始协议提供。
