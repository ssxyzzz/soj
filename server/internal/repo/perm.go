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
	PermContestBankCreate    = "contest.bank.create"    // 创建主题库比赛
	PermClassCreate          = "class.create"           // 创建班级
	PermClassViewAll         = "class.view.all"         // 查看全部班级（含私密班详情/邀请码）
	PermSubmissionViewAll    = "submission.view.all"    // 提交记录总表 + 他人提交详情
	PermSubmissionViewUser   = "submission.view.user"   // 榜单/进度表中点看指定用户在某题的提交明细（含代码）
	PermSubmissionRateExempt = "submission.rate_exempt" // 提交不受全局限速
	PermSolutionManage       = "solution.manage"        // 题解管理：置顶/删除他人题解（v0.5b-1）
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
	{"class", "班级权限"},
	{"rank", "排名权限"},
	{"submission", "提交记录权限"},
	{"admin", "管理后台权限"},
}

// PermCatalog 权限位目录（展示顺序即后台勾选顺序；备份类操作归入管理后台权限）。
var PermCatalog = []PermInfo{
	{PermProblemViewAll, "题库全量可见", "可见隐藏题与比赛/考试锁定中的题，可直接向隐藏题提交", "problem"},
	{PermProblemCreate, "新建题目", "新建/导入题目（网页出题功能上线后生效）", "problem"},
	{PermProblemEditOwn, "编辑自己的题目", "编辑自己导入的题目的题面与隐藏标记", "problem"},
	{PermProblemEditAll, "编辑任意题目", "不受题目属主限制", "problem"},
	{PermSolutionManage, "题解管理", "置顶题解、删除他人题解（满分用户均可发布自己的题解）", "problem"},
	{PermContestBankCreate, "创建主题库比赛", "发起全站比赛", "contest"},
	{PermClassCreate, "创建班级", "建班并成为建班人", "class"},
	{PermClassViewAll, "查看全部班级", "可见私密班级的详情、成员与邀请码", "class"},
	{PermSubmissionViewAll, "查看全部提交", "提交记录总表与他人的提交详情/代码", "submission"},
	{PermSubmissionViewUser, "查看用户提交明细", "榜单/进度表中点看指定用户在某题的全部提交（含代码与测试点明细）", "submission"},
	{PermSubmissionRateExempt, "提交不限速", "提交不受全站每分钟限速约束", "submission"},
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
