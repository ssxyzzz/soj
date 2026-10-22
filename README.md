# SOJ — 在线判题系统

面向班级与社团的轻量级 Online Judge：一个 Go 程序 + MongoDB + go-judge 沙箱，三个容器就是一个完整的 OJ。
支持班级作业（即时出分）、班级考试（OI 赛制）、主题库比赛、解题排名；权限采用**可配置角色 + 权限位**（对标 Hydro 的权限位/角色解耦、按单站点简化），root 可在管理后台自定义角色。

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

- **教学场景一把抓**：班级作业（随时做、即时出分，老师看每人各题完成情况）、班级考试（OI 赛制：考时不出分只记次数、考后出榜、可设赛后隐藏期）、主题库比赛与全站解题排名（隐藏期成绩防赛中推测）；班级支持公开/私密、邀请码与名单导入、多管理员。
- **判题能打**：比对模式按题可配——全文比对 / 宽容比对（token 化 + 浮点容差）/ 自定义 checker（答案不唯一的题也能出）；子任务分组给分；考试/比赛可开文件 IO（NOI 惯例，stdout 不参与比对）；比赛锁题期间全站指引到赛事页提交。判题队列自带优先级（学生提交优先于单条/批量重测）、租约心跳自愈（判题器失联自动重排队，无需重启）、评测可取消、按题/按赛一键批量重测。
- **题型齐全**：传统题之外支持客观题族（单选/多选/判断/填空；多小问整卷、按小题给分、提交即判、整卷满分解锁答案与解析）、函数题（driver.cpp 与选手实现同次合编，仅 C++，提交页/在线编程自动预填 template.cpp）与交互题（interactor 与选手程序 go-judge 双进程管道，C/C++，逐点回传交互器反馈）；语言矩阵按题型收窄。
- **网页建题**：Markdown 题面（Vditor 编辑器：所见即所得 / 即时渲染 / 分屏预览、内容主题与编辑模式跟随个人设置、高度随内容自生长且可拖拽、KaTeX 公式、粘贴即传图、导出 Markdown/PDF/HTML）、时限/内存、单一题包上传入口（自动识别 SOJ v2 / Hydro / 裸数据，整包替换、容忍 macOS 打包杂物）、题包与 extra 存档整包下载（权限位 + 每日限额）；客观题小题支持 YAML 导入/导出（含答案/解析），任意题可导出学生版试卷；也支持目录式题包 `make import` 一键导入，示例题首次启动自动就位。
- **试卷打印**：单题详情与比赛/考试编辑弹窗可打开 A4 学生版打印视图；整卷按卷内顺序拼接题目，开启文件 IO 时在题目标题下展示英文文件名。只保留题号/标题/题面/样例/客观题选项与填空位，答案与后台信息不出现；浏览器直接另存 PDF。
- **标签体系**：root 在后台以层级卡片流维护多级标签树（点击下钻、双击改名、长按拖动换位、引用角标、五色分层，被引用标签删改有拦截），题库按标签多选筛选、详情页信息卡展示通过率与标签。
- **在线编程**：CodeMirror 6 编辑器（语法高亮/自动缩进/三层补全，Tab 接受弹层，字体字号即改即存）+ 题面左栏、代码右栏的全屏做题页，分栏宽度可拖。
- **等级与难度体系**：题目十档难度（锚点对齐 NOI 大纲与教学进度，存题可按通过率预估后再手调，题包往返携带）；学生 AC 获得难度加权经验（打太简单的题收益递减），等级读时实时聚合——调难度全员等级自动跟随；等级名、升级阈值、衰减系数、特效开关全部后台可调（默认段位取自王者荣耀：青铜→极境），代码自动补全按等级解锁；等级与难度徽章为同体系浅色 tag（难度 10% 浓度、等级 30%，光晕与呼吸星逐级递增，极境彩色渐变/暗黑压轴），顶栏 SOJ 同步极境特效，全站动画尊重系统「减少动态效果」设置。
- **权限灵活**：19 个权限位 + 可配置角色——内置 default / admin / root，root 可建自定义角色（备份员、出题老师…）；导航、路由与接口全链路判权，降权即时生效；退出班级也是权限位（default 默认不能自行退班，由老师移出或授予）；修改用户名单独权限位（默认仅 root）。
- **注册管控**：系统设置一键开关注册开放——关闭后登录页「注册」页签与顶栏「注册」入口同步隐藏、注册接口一并拒绝，建号收归用户管理；存量部署默认保持开放。
- **账号安全**：本人修改密码（旧密码 + 新密码二次确认，改后**全端下线**——旧登录态连同管理员重置一并立即失效）；登录失败锁定（连续 5 次锁 15 分钟，递进式提示「已失败 N 次还剩 M 次机会」，锁定期正确密码也拒，到期自动解锁，管理端可见锁定徽章）；认证接口 IP 限流 + 429 审计（私网来源自动豁免，适配 frp 隧道共享出口拓扑）；JWT 密钥缺失拒绝启动、登录时序侧信道抹平；安全审计日志（登录/锁定/改密/重置/限流流水带 IP/UA，root 后台可查，TTL 90 天滚动清理）；密码强度 8–64 + 弱口令黑名单 + 不得含用户名（存量账号登录不受影响）。
- **HTTPS 与域名**：`deploy/https/` 开箱部署包——Caddy 配置（阿里云 DNS-01 证书自动签发续期，不占 80 端口、不受备案状态影响）、frps/frpc 参考配置、分步手册（RAM 子账号 → 备案期间自测 → 备案通过日十分钟切换清单 → 回退）；ICP 备案号后台直接填写，页脚悬挂并链接工信部备案系统。
- **数据安全**：管理后台一键加密备份（age），**备份下载的永远是密文**，误传网盘也无害；密钥两级（服务器托管 / 离线私钥防整机入侵）。
- **性能与安全打底**：单 Go 二进制内嵌前端、秒级热替换发版；gzip、静态资源一年期 immutable 缓存（Vditor 等直拷资源 no-cache 回源校验）、Markdown 渲染自带 XSS 过滤、提交限速可调。

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

两种方式：**网页建题**（持「新建题目」权限位者直接在网页建，见功能亮点）；或目录式题包 `make import DIR=路径`（或放进 `data/seed/`——仅首次部署播种一次，之后启动不再播种，加题用 import 或网页建题）：

```
my-problem/
├── problem.yaml      # 唯一配置：title/type/tags/time/memory/hidden/samples/subtasks/file_io；
│                     #   客观题内嵌 questions，函数题/交互题声明 driver/template/interactor
├── statement.md      # 题面
├── cases/            # 每个测试点一对输入/答案：1/in 1/ans、2/in 2/ans …
├── checker.cpp       # 〔可选〕SPJ（testlib 约定）
├── driver.cpp        # 〔函数题〕评测驱动器
├── template.cpp      # 〔函数题〕学生模板
├── interactor.cpp    # 〔交互题〕交互器
└── extra/            # 〔可选〕造数据脚本等存档文件
```

目录名前缀数字即题目 ID（`7_我的题目` → pid 7）；`type` 缺省为传统题，可设 `objective` / `function` / `interactive`；`hidden: true` 设为主题库隐藏（适合考试题）。题包以单个 `problem.yaml` 为唯一配置；旧版 SOJ 包与 Hydro 包里的 `config.yaml` 仍会在导入时自动兼容解析。网页上传只有一个入口，后端自动识别 SOJ v2 / Hydro / 裸数据包；下载任意题的数据包即格式模板。

## 配置

全部走环境变量（见 `docker-compose.yml` 与 `server/internal/config/config.go`），常用的：`SOJ_MONGO_URI`（Mongo 连接串）、`SOJ_DATA_DIR`（测试数据根目录）、`SOJ_SANDBOX_ADDR`（沙箱地址）、`SOJ_JUDGE_WORKERS`（判题并发，默认 2）、`SOJ_JWT_SECRET`（**不设置将拒绝启动**；compose 开发默认值放行但会打警告，生产务必换随机长字符串）、`SOJ_TRUSTED_PROXIES`（可信代理 IP/CIDR，逗号分隔，ClientIP 只从可信直连方解析 X-Forwarded-For；`none` = 不信任任何代理）。

## HTTPS 与域名上线

公网部署强烈建议 HTTPS（明文 HTTP 下学生密码与登录 token 在公网裸奔）。SOJ 应用本身保持纯 HTTP，TLS 在入口层用 Caddy 终止、证书走 Let's Encrypt DNS-01 自动签发续期；配套的 Caddyfile、frps/frpc 参考配置与阿里云分步手册（RAM 子账号 → Caddy 安装 → 备案期间自测 → 备案通过切换日清单 → 回退）见 [deploy/https/](deploy/https/)。ICP 备案号在管理后台「系统设置」直接填写，页脚自动悬挂并链接工信部备案系统（留空不展示）。

## 备份与恢复

管理后台「备份」面板一键完成：mongodump + 测试数据打包 → age 加密 → 密文归档下载，服务器同步留存；命令行等价 `./scripts/backup.sh`（cron 可直接调）。恢复：

```bash
./scripts/restore.sh <归档> --pull-key        # Tier 0：私钥在服务器密钥卷
./scripts/restore.sh <归档> <私钥文件>         # Tier 1：用启用时一次性保存的私钥
```

恢复会覆盖现有库并还原测试数据卷，默认二次确认（`--yes` 跳过）。

## 部署到服务器

开发机是唯一事实源，服务器只是运行副本——**代码一律在开发机改完再推**，不要直接改服务器上的文件（下次同步会被覆盖；万一两侧分叉，把服务器副本 rsync 拉回临时目录 diff 合并后再推）。同步两步：

```bash
rsync -a --exclude node_modules --exclude .git --exclude backups --exclude .env \
  --exclude .mimosa --exclude docker-compose.override.yml \
  --exclude plan.md --exclude web-design.md --exclude notes.md ~/soj 服务器别名:~/
ssh 服务器别名 'cd ~/soj && make up && docker compose up -d --build soj'
```

（`plan.md` / `web-design.md` / `notes.md` 等内部文档只留开发机，不随部署上服务器；rsync 只加不删，改排除项后记得手动清一次服务器上的旧副本。）

建议封装成本地 `scripts/sync-to-server.sh`（**不入库**，各机自建：目标别名因机而异）——完成上面两步并做 `/api/health` 健康检查；`--no-build` 只同步不构建，`SOJ_HOST` 覆盖目标机。

服务器侧参数放 **`docker-compose.override.yml`**（compose 自动合并，rsync 已排除）：生产判题并发、Mongo 缓存上限，用 `ports: !override []` 清掉开发端口映射（27017/5050 不对外）。基础 compose 保持开发形态入库，两边互不干扰。最后一步 `--build` 让镜像与代码同步，宿主机重启后容器自愈不会回退到旧版本。

## 路线图

v0.4.1 作业与管理入口 ✅ → v0.4.2 班级考试 ✅ → v0.4.3 代码可见性 ✅ → v0.4.4 富文本 Vditor ✅ → v0.4.5 上云前性能与安全 ✅ → v0.4.6 全站两列布局改造 ✅ → v0.4.7 权限位与可配置角色 ✅ → v0.4.8 个人设置 ✅ → v0.4.9 备份加密与备份面板 ✅ → v0.4.10 管理后台改版 ✅ → v0.5.1 题面图片上传 ✅ → v0.5.2 网页建题/比对扩展/多作者题解/文件 IO ✅ → v0.5.3 代码编辑器升级（CodeMirror 6）✅ → v0.5.5 标签树（标签多选筛选/详情信息卡/建题级联打标）✅ → 编辑器体验小批（Tab 补全/三层补全/字号 12-48）与标签树卡片化管理 ✅ → 视觉融合三批（AFFiNE 底座 / Markdown 限宽排版与编辑器 chrome / Ant Design 对齐与工程收编）✅ → v0.5.6 老师教学闭环（成绩导出/已通过列）✅ → v0.6.2 统一成绩表 ✅ → v0.6.3 字号 token ✅ → v0.7 设计原则落地 ✅ → v0.7.1 判题队列 ✅ → v0.7.2 退出班级/副列重排 ✅ → v0.7.3 班级编辑/选题器 ✅ → v0.8 编辑器回归 Markdown/Vditor ✅ → v0.8.1 编辑器样式/偏好 ✅ → v0.8.2 管理卡精简 ✅ → v0.8.3 数据存档/下载 ✅ → v0.9 题包 v2 + 客观题 ✅ → v0.9.1 函数题 ✅ → v0.9.2 交互题 ✅ → v0.9.3 编辑器高度与拖拽修复 ✅ → v0.9.4 浏览器验收 4 项修复 ✅ → v0.9.5 编辑器高度/全屏/加载修复 ✅ → v0.10 客观题 YAML / 单题与整卷试卷打印 ✅ → v0.11 题目难度 + 学生等级 + 默认标签树 ✅ → v0.11.1 账号与部署策略（初始账号 root/sojroot123、注册开关、修改用户名权限位、全新库静默引导 + 隐藏目录防崩）✅ → v0.13 HTTPS 与域名接入（可信代理/ICP 备案号页脚/Caddy DNS-01 部署包）✅（交付待验收）→ v0.14 账号安全地基（改密全端下线/登录锁定/IP 限流/JWT 强制/安全审计）✅（交付待验收）。

实现记录见 [notes.md](notes.md)，排期与需求记录见 [plan.md](plan.md)。

## 许可证

以 [AGPL-3.0](LICENSE) 协议开源。架构设计仅参考主流开源 OJ 的公开思路、未复制其代码；随发行物分发的第三方组件（go-judge、Vditor、Element Plus 等）仍按各自原始协议提供。
