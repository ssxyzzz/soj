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
             └─ 判题线程池：每秒从 MongoDB 原子领取 Pending 提交
                 ├─ 题目测试数据：按需逐点读取（/data/testdata 卷）
                 └─ go-judge 沙箱（localhost:5050）编译运行并比较输出
```

## 功能亮点

- **教学场景一把抓**：班级作业（随时做、即时出分，老师看每人各题完成情况）、班级考试（OI 赛制：考时不出分只记次数、考后出榜、可设赛后隐藏期）、主题库比赛与全站解题排名（隐藏期成绩防赛中推测）；班级支持公开/私密、邀请码与名单导入、多管理员。
- **判题能打**：比对模式按题可配——全文比对 / 宽容比对（token 化 + 浮点容差）/ 自定义 checker（答案不唯一的题也能出）；子任务分组给分；考试/比赛可开文件 IO（NOI 惯例，stdout 不参与比对）；比赛锁题期间全站指引到赛事页提交。
- **网页建题**：富文本题面（粘贴即传图）、时限/内存、测试数据 zip 上传（支持 config.yaml、整包替换、容忍 macOS 打包杂物）；也支持目录式题包 `make import` 一键导入，示例题首次启动自动就位。
- **标签体系**：root 在后台以层级卡片流维护多级标签树（点击下钻、双击改名、长按拖动换位、引用角标、五色分层，被引用标签删改有拦截），题库按标签多选筛选、详情页信息卡展示通过率与标签。
- **在线编程**：CodeMirror 6 编辑器（语法高亮/自动缩进/三层补全，Tab 接受弹层，字体字号即改即存）+ 题面左栏、代码右栏的全屏做题页，分栏宽度可拖。
- **权限灵活**：15 个权限位 + 可配置角色——内置 default / admin / root，root 可建自定义角色（备份员、出题老师…）；导航、路由与接口全链路判权，降权即时生效。
- **数据安全**：管理后台一键加密备份（age），**下载的永远是密文**，误传网盘也无害；密钥两级（服务器托管 / 离线私钥防整机入侵）。
- **性能与安全打底**：单 Go 二进制内嵌前端、秒级热替换发版；gzip、静态资源一年期 immutable 缓存、渲染内容 DOMPurify 消毒、提交限速可调。

## 快速启动

```bash
make up   # 自动先编译前端（缺 Node/Go 会自动装到 ~/.local/opt 免 sudo），再起 3 容器
```

打开 **http://localhost**（主机侧只暴露 80 端口），默认管理员 `admin / admin123`（root），登录后即可体验完整判题流程。

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

两种方式：**网页建题**（持「新建题目」权限位者直接在网页建，见功能亮点）；或目录式题包 `make import DIR=路径`（或放进 `data/seed/` 随启动自动导入）：

```
my-problem/
├── problem.yaml      # title / timeLimitMs / memoryLimitKb / samples / hidden / subtasks / tags
├── statement.md      # 题面
└── cases/            # 每个测试点一对输入/答案：1/in 1/ans、2/in 2/ans …
```

目录名前缀数字即题目 ID（`7_我的题目` → pid 7）；`hidden: true` 设为主题库隐藏（适合考试题）；可选 `config.yaml` 精配时限/比对/子任务/校验器（白名单制，未知字段忽略）。

## 配置

全部走环境变量（见 `docker-compose.yml` 与 `server/internal/config/config.go`），常用的：`SOJ_MONGO_URI`（Mongo 连接串）、`SOJ_DATA_DIR`（测试数据根目录）、`SOJ_SANDBOX_ADDR`（沙箱地址）、`SOJ_JUDGE_WORKERS`（判题并发，默认 2）、`SOJ_JWT_SECRET`（**生产必改**）。

## 备份与恢复

管理后台「备份」面板一键完成：mongodump + 测试数据打包 → age 加密 → 密文归档下载，服务器同步留存；命令行等价 `./scripts/backup.sh`（cron 可直接调）。恢复：

```bash
./scripts/restore.sh <归档> --pull-key        # Tier 0：私钥在服务器密钥卷
./scripts/restore.sh <归档> <私钥文件>         # Tier 1：用启用时一次性保存的私钥
```

恢复会覆盖现有库并还原测试数据卷，默认二次确认（`--yes` 跳过）。

## 路线图

v0.4.1 作业与管理入口 ✅ → v0.4.2 班级考试 ✅ → v0.4.3 代码可见性 ✅ → v0.4.4 富文本 Vditor ✅ → v0.4.5 上云前性能与安全 ✅ → v0.4.6 全站两列布局改造 ✅ → v0.4.7 权限位与可配置角色 ✅ → v0.4.8 个人设置 ✅ → v0.4.9 备份加密与备份面板 ✅ → v0.4.10 管理后台改版 ✅ → v0.5a 题面图片上传 ✅ → v0.5b 网页建题/比对扩展/多作者题解/文件 IO ✅ → v0.5d 代码编辑器升级（CodeMirror 6）✅ → v0.5f 标签树（标签多选筛选/详情信息卡/建题级联打标）✅ → 编辑器体验小批（Tab 补全/三层补全/字号 12-48）与标签树卡片化管理 ✅ → 下一批 视觉融合两批（AFFiNE × Editor.js：全站底座浅灰画布与圆角体系 / Markdown 限宽排版与编辑器 chrome）→ v0.5c 老师教学闭环（成绩导出/题目统计/已通过列）；题包导入导出等暂缓。

实现记录见 [notes.md](notes.md)，排期与需求记录见 [plan.md](plan.md)。

## 许可证

以 [AGPL-3.0](LICENSE) 协议开源。架构设计仅参考主流开源 OJ 的公开思路、未复制其代码；随发行物分发的第三方组件（go-judge、Vditor、Element Plus 等）仍按各自原始协议提供。
