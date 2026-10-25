# v0.19 界面设计语言规范 · 青格蓝调（web-design-v3）

> 本文档是 **v0.19 全站界面重设计的唯一权威设计文档**（定稿 2026-10-24）。
> ⚠️ 2026-10-24 起本文部分条目由 [web-design-v3-full.md](web-design-v3-full.md)（v0.20 全面对标批次）接替：§5 的「不改十阶色轴 / 不改字号 token 体系 / 不新增数据区块」三条已失效（色轴按 v3 重设计、字号升档 34/56、统计与筛选接口已落地）；其余 token / 画布 / 组件形制条目继续有效。2026-10-25 徽章改版（等级特效链恢复 / 难度方块点缀 / 极境六彩同源两形制）亦以 web-design-v3-full.md §1.6 为准；§4.4 容器收窄 1280 已被 2026-10-25 回调 1440 接替（web-design-v3-full.md §1.7）。
> 视觉基准 = `design-preview/soj-redesign-v3-full.html`（24 屏全场景预览稿，参考 simple408.cn 实测设计 token）。
> 布局原则（导航 / 主列+副列 / 副列顺序）仍以 [web-design.md](web-design.md) 为准——v3-full 完全兼容该原则，无冲突。
> 实施范围：**只换视觉皮肤（色 / 形 / 质感），不改信息架构、不改字号 token 体系、不加新数据区块**。

## 1. 设计来源与实测 token

参考站 simple408.cn（「AI 原生」设计语言）在真实浏览器中 `getComputedStyle` 实测提取（非目测）：

| 类别 | 值 | 来源 |
| --- | --- | --- |
| 画布底 | `linear-gradient(#FBFDFF, #F1F6FD)` | `.bg-base` |
| 青色网格线 | `rgba(122,184,196,.10)` 1px 双向、44px 格距 | `.bg-grid` |
| 青色光晕 | `radial-gradient(420px at 50% 33%, rgba(122,184,196,.12), transparent 60%)` | `.bg-spot` |
| 蓝色光晕 | `rgba(36,85,218,.05)` 径向 | 蓝斑 |
| 标题色 | `#0B1F45`（藏蓝），hero 850 字重 / 负字距 | h1 实测 rgb(11,31,69) |
| 正文色 | `#10233B` | body 实测 rgb(16,35,59) |
| 主蓝 | `#1F49BA`（实心）/ 渐变 `#2D68EF → #2455DA` | 主按钮实测 |
| 品牌渐变字 | 90deg `#16A7D2 → #1557B8 → #2C5DE0` 流动 | brand-text 实测 |
| 按钮形制 | 胶囊 999px，主按钮高 44–62 / 700 字重 | 实测 |
| 卡片 | 白底、圆角 12–16px、柔和大投影 `0 10px 40px rgba(15,23,42,.08)` | 登录卡实测 |
| 表格 | 圆角外框、表头浅蓝灰底、行悬停浅蓝 | 内页实测 |

## 2. 落站 token（`web/src/styles/main.css`）

### 2.1 新增 `--soj-*` 设计 token

```css
--soj-navy: #0B1F45;        /* 大标题（页头/hero/统计数字） */
--soj-ink: #22334D;         /* 正文（比 EP primary 略沉一档的藏蓝墨） */
--soj-blue: #1F49BA;        /* 主蓝（实心，= --el-color-primary） */
--soj-blue-deep: #1845AD;   /* 主蓝悬停 */
--soj-blue-hi: #2D68EF;     /* 渐变亮端 */
--soj-blue-lo: #2455DA;     /* 渐变暗端 */
--soj-teal: #16A7D2;        /* 品牌青（渐变/描边条点缀） */
--soj-blue-soft: rgba(31,73,186,.08);   /* 选中态浅底（菜单胶囊/我的行） */
--soj-line: rgba(11,31,69,.08);         /* 卡片/分隔 hairline */
--soj-surface-2: #F5F9FF;   /* 行悬停/浅填充 */
--soj-surface-3: #F0F5FC;   /* 表头底 */
```

### 2.2 EP 变量换血（`:root` 级，main.css 恒序后到者胜）

| EP 变量 | 旧值（Ant 对齐） | 新值 |
| --- | --- | --- |
| `--el-color-primary` | EP 默认 #409EFF | `#1F49BA`（light-3/5/7/8/9、dark-2 用 color-mix 按白阶 30/50/70/80/90% 派生） |
| `--el-color-success` / `warning` / `danger` / `info` | EP 默认 | `#0E9F6E` / `#D98207` / `#E02D3C` / `#7A8CA6`（同法派生 light 阶） |
| `--el-bg-color-page` | `#f5f5f5` | `#F4F8FC`（画布基准色；真实画布见 §3） |
| `--el-border-radius-base` / `-small` | 6 / 4 | 8 / 5（输入框、下拉等控件） |
| `--el-border-color` | `#d9d9d9` | `rgba(11,31,69,.20)`（藏蓝 hairline） |
| `--el-text-color-primary` | `rgba(0,0,0,.88)` | `rgba(11,31,69,.90)`（藏蓝墨水） |

### 2.3 组件形制（组件类级覆盖，EP 自声明处必须类级压）

| 组件 | 规则 |
| --- | --- |
| `el-card` | 圆角 16px、边 `--soj-line`、两层柔影 `0 1px 2px rgba(11,31,69,.04), 0 12px 32px rgba(11,31,69,.06)`；hover 阴影略升 |
| `el-button` | **全站胶囊 999px**；`primary` 实心款 = 渐变 `#2D68EF→#2455DA` + 柔光投影 `0 6px 16px rgba(36,85,218,.22)`，hover 提亮、active 压暗；plain/text/link 不受渐变影响（`:not(.is-plain):not(.is-text):not(.is-link)` 限定） |
| `el-tag` / TierBadge / ulv / diff | **胶囊回归 999px**（推翻 2026-10-23「徽章矩形化」决策——v0.19 拍板全站徽章跟随胶囊形制；SOJ 顶栏品牌胶囊不变） |
| `el-table` | 表头 `--soj-surface-3` 底 + 藏蓝 700 字重；行悬停 `--soj-surface-2`；斑马纹维持 EP lighter |
| `el-dialog` / `el-message-box` | 圆角 20px / 16px + 大柔影 |
| `el-pagination` | 页钮 8px 圆角方块 |

## 3. 画布（全站背景）

三层固定画布，写在 `body` 与 `body::after`（z-index:-1，不与内容交互）：

1. 冷白纵向渐变 `#FBFDFF → #F1F6FD`（`background-attachment: fixed`）；
2. 双径向光晕：顶部中央青 `rgba(122,184,196,.14)`、右上蓝 `rgba(36,85,218,.05)`；
3. `body::after` 青色 1px 网格（44px 格距 `rgba(122,184,196,.11)`），`mask-image` 径向渐隐（顶部实、往下淡出至 78% 高度）——工程图纸质感但不过量。

透明化跟进：`MainLayout` 的 `.main`/`.footer` 与 `LoginView` 的 `.login-page` 原自带 `--el-bg-color-page` 实底，改透明让画布透出。

## 4. 关键界面（模板级改动）

### 4.1 顶栏（MainLayout）

- 高度 64px、**玻璃拟态**：`rgba(251,253,255,.72)` + `backdrop-filter: blur(16px) saturate(1.5)` + hairline 底边、sticky。
- 品牌双色结构：渐变小方块（`#2D68EF→#16A7D2`，白字 S）+ 「SOJ」流彩渐变字（90deg 青→蓝→青，7s 流动；`prefers-reduced-motion` 退静态）。**等级特效开（Lv.10 极境）时维持原 ulv 胶囊标识不变**——特效态优先于默认态。
- 菜单项胶囊化：active/hover = `--soj-blue-soft` 浅底 + 主蓝字 700，去 EP 横线指示条。

### 4.2 登录页（LoginView）

全屏居中白卡（宽 390px、圆角 16px、大柔影），卡内：品牌（方块 + 渐变字）→ 登录/注册分段切换（胶囊 seg）→ 表单 → 全宽胶囊主按钮；忘记密码弹窗维持。背景 = 全局画布（无导航）。

### 4.3 首页（HomeView）

hero 从卡片改为**开放式居中区块**：kicker 小标（SANDBOX JUDGED · INSTANT SCORED）→ 大标题（`--soj-fs-hero`，藏蓝 800+，「成长」渐变点睛）→ 副文案 → 胶囊按钮组（开始刷题 = 渐变主按钮）→ 技术栈 chips。特性卡图标改为彩色圆角方块；公告条、班级卡跟随全局组件换血。**不新增统计行/今日一题等需要新接口的区块**（预览稿中有、实现不做，见 plan v0.19 §7）。

### 4.4 容器与字号

- 内容容器 1440px → **1280px**（`.page` 与 PageShell 同步；v3-full 预览实测观感）。
- 字号/字重 token 表（`--soj-fs-*` / `--soj-fw-*`）**整体不动**——层次感由色彩与形制承担，避免全站字号回归测试成本。

## 5. 明确不做 / 保留

- **不改**：布局原则（web-design.md）、字号 token 体系、十阶色轴（`--soj-tier-*`/ulv/diff 配色保留，仅形制变胶囊）、等级特效体系、任何业务逻辑与接口。
- **不引入** Web 字体（Noto Sans SC 仅作本地优先候选，字体栈首插 `'Noto Sans SC'`，CJK 先行纪律不变；无网络依赖）。
- **不新增**假数据区块（首页统计行/今日一题等）——视觉稿元素凡需新后端接口的一律不落地。
- 暗色模式仍缓（§3 备忘维持）。

## 6. 变更文件清单

| 文件 | 改动 |
| --- | --- |
| `web/src/styles/main.css` | token 换血 + 画布 + EP 组件覆盖（§2–3） |
| `web/src/layouts/MainLayout.vue` | 玻璃顶栏 / 品牌双色 / 菜单胶囊 / main·footer 透明 |
| `web/src/views/LoginView.vue` | 居中卡布局（§4.2），逻辑零改动 |
| `web/src/views/HomeView.vue` | hero 开放式区块 + 特性图标方块（§4.3），逻辑零改动 |
| `web/src/components/PageShell.vue` | 容器 1280 |
| `web/src/components/TierBadge.vue` | 徽章胶囊（999px） |

验收口径：`npm run lint` / `typecheck` / `build` 全绿 + `web-dev` 起本地，浏览器逐页截图对照 `soj-redesign-v3-full.html`（首页/题库/题目/比赛/班级/排名/提交/登录八屏为主）。
