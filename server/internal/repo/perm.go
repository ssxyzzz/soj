package repo

// 权限位目录（见 plan.md 第 3 章）：权限与角色解耦——目录在代码里定义（能力面由代码决定），
// 角色只是权限位的集合（roles 集合，数据可配）。允许 deny 不引入负权限位。
//
// 硬性约定：
//   - "*" 仅 root 内置角色持有，权限判定函数对 root 恒通过（不查库）；
//   - "管理角色"操作本身不设权限位，路由层硬编码 root，不可授权。
const (
	PermProblemViewAll       = "problem.view.all"       // 题库全量可见（隐藏/锁定题）+ 可提交隐藏题
	PermProblemCreate        = "problem.create"         // 新建/导入题目
	PermProblemEditOwn       = "problem.edit.own"       // 编辑自己导入的题目（题面/隐藏）
	PermProblemEditAll       = "problem.edit.all"       // 编辑任意题目
	PermProblemDataDownload  = "problem.data.download"  // 下载题目测试数据包（每日可下载题数受系统设置限额）
	PermContestBankCreate    = "contest.bank.create"    // 创建主题库比赛
	PermContestScoreView     = "contest.score.view"     // 查看比赛/考试成绩表（root/admin 默认开启，default 无）
	PermContestScoreDetail   = "contest.score.detail"   // 比赛成绩表详情：点看他人某题提交明细与代码（root/admin 默认开启，default 无）
	PermClassCreate          = "class.create"           // 创建班级
	PermClassViewAll         = "class.view.all"         // 查看全部班级（含私密班详情/邀请码）
	PermClassLeave           = "class.leave"            // 成员自行退出班级（default 默认关闭，v0.7.2）
	PermAssignmentScoreView  = "assignment.score.view"  // 查看班级作业成绩表（本班作业全班完成情况，default 默认开启）
	PermListManage           = "list.manage"            // 题单管理：创建题单（编辑/删除限创建者与 root，v0.18.3）
	PermListReorder          = "list.reorder"           // 题单排序：调整题单在题单页的展示顺序（默认仅 root，v0.18.3）
	PermListDelete           = "list.delete"            // 题单删除：可删除任意题单（创建者删自己的不受此位限，v0.18.3）
	PermSubmissionViewAll    = "submission.view.all"    // 提交记录总表 + 他人提交详情
	PermSubmissionViewUser   = "submission.view.user"   // 榜单/进度表中点看指定用户在某题的提交明细（含代码）
	PermSubmissionRateExempt = "submission.rate_exempt" // 提交不受全局限速
	PermSolutionManage       = "solution.manage"        // 题解管理：置顶/删除他人题解（v0.5.2-1）
	PermDiscussManage        = "discussion.manage"      // 讨论管理：删帖/置顶/加精/锁定/处理举报与禁言（v0.24）
	PermDiscussRateExempt    = "discussion.rate_exempt" // 发帖不受讨论区限速（v0.24）
	PermDiscussCodeBypass    = "discussion.code.bypass" // 题内讨论代码块查看不受分数门槛（v0.24）
	PermNotifyBroadcast      = "notify.broadcast"       // 全站公告广播（v0.24，默认仅 root）
	PermUserNicknameEdit     = "user.nickname.edit"     // 本人自助修改昵称（默认用户默认开启）
	PermUserUsernameEdit     = "user.username.edit"     // 用户管理中修改登录用户名（身份标识；默认仅 root，admin 无此位）
	PermAdminUserManage      = "admin.user.manage"      // 管理端用户管理
	PermAdminSettings        = "admin.settings"         // 系统设置
	PermBackupRun            = "backup.run"             // 一键备份下载（v0.4.9 备份面板）
	PermBackupKeys           = "backup.keys"            // 备份密钥模式管理（Tier 1 私钥一次性展示）
)

// PermAll 根角色通配权限，任何自定义角色不得持有。
const PermAll = "*"

// PermInfo 权限位条目（管理后台角色编辑用）；Group 指向 PermGroups 中的分类 id。
type PermInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Group string `json:"group"`
}

// PermGroup 权限分类（角色矩阵与新建角色按此顺序分组展示；无权限位的分类前端不渲染）。
type PermGroup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PermGroups 权限分类目录（首页/排名暂无权限位，占位定义、前端跳过空组）。
var PermGroups = []PermGroup{
	{"home", "首页权限"},
	{"problem", "题库权限"},
	{"contest", "比赛权限"},
	{"list", "题单权限"},
	{"class", "班级权限"},
	{"rank", "排名权限"},
	{"submission", "提交记录权限"},
	{"discussion", "讨论权限"},
	{"user", "用户权限"},
	{"admin", "管理后台权限"},
}

// PermCatalog 权限位目录（展示顺序即后台勾选顺序；备份类操作归入管理后台权限）。
var PermCatalog = []PermInfo{
	{PermProblemViewAll, "题库全量可见", "可见隐藏题与比赛/考试锁定中的题，可直接向隐藏题提交", "problem"},
	{PermProblemCreate, "新建题目", "新建/导入题目（网页出题功能上线后生效）", "problem"},
	{PermProblemEditOwn, "编辑自己的题目", "编辑自己导入的题目的题面与隐藏标记", "problem"},
	{PermProblemEditAll, "编辑任意题目", "不受题目属主限制", "problem"},
	{PermProblemDataDownload, "下载数据", "下载题目测试数据包（zip，含造数据脚本等存档文件）；每日可下载题数由系统设置限定，无此权限不可下载", "problem"},
	{PermSolutionManage, "题解管理", "置顶题解、删除他人题解（满分用户均可发布自己的题解）", "problem"},
	{PermContestBankCreate, "创建主题库比赛", "发起全站比赛", "contest"},
	{PermContestScoreView, "查看比赛/考试成绩表", "可查看比赛与考试的成绩表（各题得分与排名）；仍受场次「开启成绩表」开关与结果发布状态约束，组织者不受限。default 角色默认无此位", "contest"},
	{PermContestScoreDetail, "查看比赛/考试成绩表详情", "在比赛/考试成绩表中点看指定用户某题的提交明细与代码（AC 与各次通过状态）；组织者与「查看用户提交明细」持有者不受限。default 角色默认无此位", "contest"},
	{PermListManage, "题单管理", "创建题单；编辑/删除仅限题单创建者与 root，不受此位约束", "list"},
	{PermListReorder, "调整题单排序", "在题单页拖动调整题单的展示顺序（全局次序）；默认仅 root", "list"},
	{PermListDelete, "删除任意题单", "可删除任意人的题单；创建者删除自己的题单不受此位限制", "list"},
	{PermClassCreate, "创建班级", "建班并成为建班人", "class"},
	{PermClassViewAll, "查看全部班级", "可见私密班级的详情、成员与邀请码", "class"},
	{PermClassLeave, "退出班级", "成员可自行退出所在班级；关闭后只能由老师移出（default 角色默认关闭）", "class"},
	{PermAssignmentScoreView, "查看班级作业成绩表", "班级成员可查看本班作业的完整成绩表（全班各题得分与排名）；关闭后只能看到自己的完成进度。点看他人提交代码仍受「查看用户提交明细」或该题满分约束，不随此位放开", "class"},
	{PermSubmissionViewAll, "查看全部提交", "提交记录总表与他人的提交详情/代码", "submission"},
	{PermSubmissionViewUser, "查看用户提交明细", "榜单/进度表中点看指定用户在某题的全部提交（含代码与测试点明细）", "submission"},
	{PermSubmissionRateExempt, "提交不限速", "提交不受全站每分钟限速约束", "submission"},
	{PermDiscussManage, "讨论管理", "讨论区：删帖/置顶/加精/锁定/处理举报与禁言（三区通用）", "discussion"},
	{PermDiscussRateExempt, "发帖不限速", "讨论发帖/回复不受全站讨论限速约束", "discussion"},
	{PermDiscussCodeBypass, "讨论代码门槛豁免", "题内讨论中查看被分数门槛隐藏的代码块（教师群体本就不受限）", "discussion"},
	{PermNotifyBroadcast, "全站公告广播", "向全员发站内通知公告（受每日频次约束）；默认仅 root", "discussion"},
	{PermUserNicknameEdit, "修改昵称", "允许用户在本人的个人设置中修改昵称；关闭后只能由管理员在用户管理中修改", "user"},
	{PermUserUsernameEdit, "修改用户名", "在用户管理中修改任意用户的登录用户名（改名后原用户名即释放，登录与展示即时切换）；默认仅 root 开启，admin 无此位", "user"},
	{PermAdminUserManage, "用户管理", "管理后台：建号/改角色/重置密码", "admin"},
	{PermAdminSettings, "系统设置", "管理后台：公告与提交限速等站点设置", "admin"},
	{PermBackupRun, "备份下载", "一键备份到本地（加密归档下载）", "admin"},
	{PermBackupKeys, "备份密钥管理", "切换备份密钥模式、查看 Tier 1 私钥（敏感，建议仅 root）", "admin"},
}

// permIndex 目录索引。
var permIndex = func() map[string]bool {
	m := make(map[string]bool, len(PermCatalog))
	for _, p := range PermCatalog {
		m[p.ID] = true
	}
	return m
}()

// PermExists 权限位是否在目录中（不含 "*"）。
func PermExists(id string) bool { return permIndex[id] }

// PermIDs 目录 id 列表（root 生效权限全集展开用）。
func PermIDs() []string {
	ids := make([]string, 0, len(PermCatalog))
	for _, p := range PermCatalog {
		ids = append(ids, p.ID)
	}
	return ids
}

// ValidPermSet 校验一组权限位：全部在目录内、不含 "*"、无重复。
func ValidPermSet(perms []string) bool {
	seen := make(map[string]bool, len(perms))
	for _, p := range perms {
		if p == PermAll || !permIndex[p] || seen[p] {
			return false
		}
		seen[p] = true
	}
	return true
}

// EffectivePerms 计算角色的生效权限位：root 展开为全目录；其余过滤掉目录外的残留
// （权限位从目录移除后，角色文档里的旧值自动失效）。
func EffectivePerms(r *Role) []string {
	if r == nil || r.ID == RoleRoot {
		return PermIDs()
	}
	out := make([]string, 0, len(r.Perms))
	for _, p := range r.Perms {
		if permIndex[p] {
			out = append(out, p)
		}
	}
	return out
}
