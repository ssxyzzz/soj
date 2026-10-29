package server

// RepoAPI server 层的 repo 依赖缝（v0.32 分层收紧第三步）：Handlers.repo 由具体
// *repo.Repo 换为本接口——httptest 可注入 fake（无 Mongo 跑 handler 场景），生产
// 形态恒为 *repo.Repo（下方编译期断言背书）。先大接口后按域拆（plan 既定路径）：
// 本接口 = server 包实际用到的 repo 方法全量清单（由调用面机械生成，197 个），
// 后续批次再按 handler 域收敛拆小。Importer 等住在 repo 包内、依赖私有 collection
// 的组件无法吃接口，走 concreteRepo() 类型断言（fake 不覆盖导入/种子路径）。

import (
	"context"
	"time"

	"soj/internal/repo"
)

// concreteRepo 生产形态下取回具体 *repo.Repo（Importer 构造等包内私有依赖专用）；
// fake 注入时返回 nil，调用方自行兜底（httptest 不覆盖导入/种子/批量路径）。
func (h *Handlers) concreteRepo() *repo.Repo {
	r, _ := h.repo.(*repo.Repo)
	return r
}

// RepoAPI server 包使用的 repo 方法全集（机械生成：grep h.repo.X 调用面 → go doc 签名）。
type RepoAPI interface {
	AcceptDiscussReply(ctx context.Context, tid, rid int64) error
	ActiveChallenge(ctx context.Context, uid, pid int64) (*repo.Challenge, error)
	ActiveDiscussMute(ctx context.Context, uid int64) (*repo.DiscussReportItem, error)
	AddAssignmentProblem(ctx context.Context, id, pid int64) error
	AddDiscussMute(ctx context.Context, uid int64, days int, reason string, byUID int64) error
	AddDiscussReport(ctx context.Context, targetType string, targetID, reporterUID int64, reason string, foldThreshold int64) (count int64, folded bool, err error)
	AddSecurityLog(ctx context.Context, e repo.SecurityLog) error
	AddXpAdjust(ctx context.Context, uid, delta int64, reason string, operatorUID int64) error
	AllUserIDs(ctx context.Context) ([]int64, error)
	AssignmentProgress(ctx context.Context, a *repo.Assignment, memberUIDs []int64) ([]*repo.ProgressRow, error)
	AttachChallengeSubmission(ctx context.Context, cid, sid int64, langName string, codeLen int, changelog []repo.ChallengeDelta) (bool, error)
	AvgJudgeMSRecent(ctx context.Context) (float64, error)
	BumpPwdVer(ctx context.Context, uid int64) error
	ChallengeRunningOK(ctx context.Context, cid, uid, pid int64) (bool, error)
	ClassMemberIDs(ctx context.Context, classID int64) ([]int64, error)
	ClassMemberRole(ctx context.Context, classID, uid int64) (string, error)
	ClearLoginFailures(ctx context.Context, uid int64) error
	ConsumeVerifyCode(ctx context.Context, id int64) error
	CountAllUsers(ctx context.Context) (int64, error)
	CountBroadcastsToday(ctx context.Context) (int64, error)
	CountSubmissionsSince(ctx context.Context, since time.Time) (int64, error)
	CountUserAcceptedAnswers(ctx context.Context, uid int64) (int64, error)
	CountUserDiscussTopicsToday(ctx context.Context, uid int64) (int64, error)
	CountUsersWithRole(ctx context.Context, role string) (int64, error)
	CountVisibleProblems(ctx context.Context, difficulty int) (int64, error)
	CreateAssignment(ctx context.Context, classID int64, title, intro string) (*repo.Assignment, error)
	CreateClass(ctx context.Context, name, intro string, private bool, ownerID int64, ownerUname string) (*repo.Class, error)
	CreateObjectiveSubmission(ctx context.Context, uid int64, uname string, p *repo.Problem, answers []repo.ObjectiveAnswer, contest *repo.Contest, exam *repo.Exam) (int64, error)
	CreateProblemList(ctx context.Context, l *repo.ProblemList) error
	CreateRole(ctx context.Context, role repo.Role) error
	CreateRound(ctx context.Context, kind string, c *repo.Contest) error
	CreateSubmission(ctx context.Context, uid int64, uname string, p *repo.Problem, lang, code string, contest *repo.Contest, exam *repo.Exam, challengeID int64) (int64, error)
	CreateUserWithEmail(ctx context.Context, username, password, nickname, role, email string) (int64, error)
	CreateUserWithID(ctx context.Context, id int64, username, password, nickname, role string) (int64, error)
	CreateUserWithPassword(ctx context.Context, username, password, nickname, role string) (int64, error)
	DeleteAssignment(ctx context.Context, id int64) error
	DeleteClass(ctx context.Context, classID int64) error
	DeleteProblemList(ctx context.Context, id int64) error
	DeleteRole(ctx context.Context, id string) error
	DeleteRound(ctx context.Context, kind string, id int64) error
	DeleteSolution(ctx context.Context, sid int64) error
	FailVerifyCode(ctx context.Context, id int64, already int) (int, error)
	GetAssignment(ctx context.Context, id int64) (*repo.Assignment, error)
	GetClass(ctx context.Context, id int64) (*repo.Class, error)
	GetClassByInviteCode(ctx context.Context, code string) (*repo.Class, error)
	GetClassMarks(ctx context.Context, uid int64) (pins, order []int64, err error)
	GetDiscussReply(ctx context.Context, rid int64) (*repo.DiscussReply, error)
	GetDiscussTopic(ctx context.Context, tid int64) (*repo.DiscussTopic, error)
	GetLevelConfig(ctx context.Context) (*repo.LevelConfig, error)
	GetMetaTagTree(ctx context.Context) ([]repo.TagNode, error)
	GetProblem(ctx context.Context, id int64) (*repo.Problem, error)
	GetProblemList(ctx context.Context, id int64) (*repo.ProblemList, error)
	GetProblemMeta(ctx context.Context, id int64) (*repo.Problem, error)
	GetProblemMetas(ctx context.Context, ids []int64) (map[int64]*repo.Problem, error)
	GetRole(ctx context.Context, id string) (*repo.Role, error)
	GetRound(ctx context.Context, kind string, id int64) (*repo.Contest, error)
	GetSolution(ctx context.Context, sid int64) (*repo.Solution, error)
	GetSubmission(ctx context.Context, id int64) (*repo.Submission, error)
	GetSystemSettings(ctx context.Context) (*repo.SystemSettings, error)
	GetTagTree(ctx context.Context) ([]repo.TagNode, error)
	GetTagTreeDoc(ctx context.Context, docID string) ([]repo.TagNode, error)
	GetUserByEmail(ctx context.Context, email string) (*repo.User, error)
	GetUserByID(ctx context.Context, id int64) (*repo.User, error)
	GetUserByUsername(ctx context.Context, username string) (*repo.User, string, error)
	GetUserSolution(ctx context.Context, pid, uid int64) (*repo.Solution, error)
	GetUsersByIDs(ctx context.Context, ids []int64) (map[int64]*repo.User, error)
	GetValidVerifyCode(ctx context.Context, purpose, target string) (*repo.VerifyCode, error)
	HasSolved(ctx context.Context, uid, pid int64) (bool, error)
	HiddenProblemIDs(ctx context.Context) ([]int64, error)
	ImportMembers(ctx context.Context, classID int64, usernames []string) (int, error)
	IncDiscussTopicReplies(ctx context.Context, tid int64) error
	InsertBroadcastRecord(ctx context.Context, byUID int64, title, body string) (int64, error)
	InsertDiscussReply(ctx context.Context, rep *repo.DiscussReply) error
	InsertDiscussTopic(ctx context.Context, t *repo.DiscussTopic) error
	InsertSolution(ctx context.Context, s *repo.Solution) error
	InsertUpload(ctx context.Context, u repo.Upload) error
	InsertVerifyCode(ctx context.Context, vc *repo.VerifyCode) error
	IsMember(ctx context.Context, classID, uid int64) (bool, error)
	JoinClass(ctx context.Context, classID, uid int64, uname, code string) error
	LastChallenge(ctx context.Context, uid, pid int64) (*repo.Challenge, error)
	LastUserReplyAt(ctx context.Context, uid int64) (time.Time, error)
	LeaveClass(ctx context.Context, classID, uid int64) error
	LiftDiscussMute(ctx context.Context, id int64) error
	LikedDiscussTargets(ctx context.Context, uid int64, targetType string, ids []int64) (map[int64]bool, error)
	ListActiveDiscussMutes(ctx context.Context) ([]*repo.DiscussMuteRow, error)
	ListAssignments(ctx context.Context, classID int64) ([]*repo.Assignment, error)
	ListClassExams(ctx context.Context, classID int64) ([]*repo.Exam, error)
	ListClasses(ctx context.Context) ([]repo.ClassCard, error)
	ListContests(ctx context.Context) ([]*repo.Contest, error)
	ListDiscussReplyChildren(ctx context.Context, tid int64, topIDs []int64) ([]*repo.DiscussReply, error)
	ListDiscussReplyPage(ctx context.Context, tid int64, page int) ([]*repo.DiscussReply, int64, error)
	ListDiscussReports(ctx context.Context, status string, page, pageSize int) ([]*repo.DiscussReportItem, int64, error)
	ListGlobalTopics(ctx context.Context, kind, q string, page, pageSize int) ([]*repo.DiscussTopic, int64, error)
	ListMembers(ctx context.Context, classID int64) ([]repo.ClassMember, error)
	ListNotifications(ctx context.Context, uid int64, unreadOnly bool, typ string, page, pageSize int) ([]*repo.Notification, int64, error)
	ListParticipantPairs(ctx context.Context, pids []int64) ([]repo.ListParticipRow, error)
	ListPidSubmissions(ctx context.Context, pid int64, uid int64, page, pageSize int, sort, lang string) ([]*repo.Submission, int64, error)
	ListProblemLists(ctx context.Context) ([]*repo.ProblemList, error)
	ListProblemStats(ctx context.Context, pids []int64) (map[int64]repo.ProblemStat, error)
	ListProblemsPage(ctx context.Context, f repo.ProblemListFilter, page, pageSize int) ([]*repo.Problem, int64, error)
	ListRankRows(ctx context.Context, pids []int64) ([]repo.ListRankRow, error)
	ListRoles(ctx context.Context) ([]*repo.Role, error)
	ListRoundsStartingSoon(ctx context.Context, ahead time.Duration) ([]*repo.Contest, error)
	ListScopeTopics(ctx context.Context, scope string, scopeID, viewerUID int64, includePrivate bool, page, pageSize int) ([]*repo.DiscussTopic, int64, error)
	ListSecurityLogs(ctx context.Context, page, pageSize int, typ, q string) ([]repo.SecurityLog, int64, error)
	ListSolutions(ctx context.Context, pid int64, page, pageSize int) ([]*repo.Solution, int64, error)
	ListSubmissions(ctx context.Context, f repo.SubmissionFilter, page, pageSize int) ([]*repo.Submission, int64, error)
	ListUnpublishedContests(ctx context.Context) ([]*repo.Contest, error)
	ListUnpublishedExams(ctx context.Context) ([]*repo.Exam, error)
	ListUsers(ctx context.Context, page, pageSize int, keyword, role string) ([]*repo.User, int64, error)
	LookupUsers(ctx context.Context, kw string, limit int) ([]*repo.User, error)
	MarkDiscussOfficial(ctx context.Context, tid int64) error
	MarkNotificationsRead(ctx context.Context, uid int64, ids []int64) error
	MarkRoundStartNotified(ctx context.Context, kind string, id int64) error
	MaxListSort(ctx context.Context) (int64, error)
	MyClassIDs(ctx context.Context, uid int64) ([]int64, error)
	MyClasses(ctx context.Context, uid int64) ([]repo.ClassCard, error)
	MyProblemStats(ctx context.Context, uid int64, includeHidden bool) (*repo.MyProblemStats, error)
	NotifyUsers(ctx context.Context, uids []int64, in repo.NotifyInput, retentionDays int) error
	PendingDepth(ctx context.Context) (int64, error)
	Ping(ctx context.Context) error
	ProblemIDExists(ctx context.Context, pid int64) (bool, error)
	ProblemSubmitCounts(ctx context.Context, pid int64) (total, accepted int64, err error)
	ProblemSummariesByIDs(ctx context.Context, pids []int64) (map[int64]*repo.ProblemSummary, error)
	ProblemTitleExists(ctx context.Context, title string) (bool, error)
	QueryVerifyCodeRate(ctx context.Context, purpose, target, ip string) (*repo.VerifyCodeRateQuery, error)
	QuitChallenge(ctx context.Context, uid, pid int64) (bool, error)
	RankMe(ctx context.Context, uid int64) (rank, solved int64, err error)
	ReconcileTagTreeDoc(ctx context.Context, docID, counterKey string, newNodes []repo.TagNode) ([]repo.TagNode, []int64, error)
	RecordLoginFailure(ctx context.Context, uid int64, maxFail int, lockDur time.Duration) (failedCount int, locked bool, err error)
	RecordTestDataDownload(ctx context.Context, uid, pid int64) error
	RegenInviteCode(ctx context.Context, classID int64) (string, error)
	RegradeObjective(ctx context.Context, id int64) error
	RejudgeProblem(ctx context.Context, pid int64) (int64, error)
	RejudgeRound(ctx context.Context, kind string, rid int64) (int64, error)
	RejudgeSubmission(ctx context.Context, id int64) error
	RemoveAssignmentProblem(ctx context.Context, id, pid int64) error
	ReorderAssignments(ctx context.Context, classID int64, ids []int64) (int64, error)
	ReplaceTagTreeDoc(ctx context.Context, docID string, nodes []repo.TagNode) error
	RequestCancelSubmission(ctx context.Context, sid int64) (string, error)
	ResolveDiscussReports(ctx context.Context, ids []int64, action string, byUID int64) error
	RoundLeaderboard(ctx context.Context, kind string, roundID int64, fullScores map[int64]int) ([]*repo.LeaderRow, error)
	RoundLeaderboardCached(ctx context.Context, kind string, roundID int64, fullScores map[int64]int) ([]*repo.LeaderRow, error)
	RoundSubmissionStubs(ctx context.Context, kind string, roundID int64, uid int64) ([]*repo.Submission, error)
	RoundSubmissions(ctx context.Context, kind string, roundID int64, uid int64) ([]*repo.Submission, error)
	RoundUserProblemSubmissions(ctx context.Context, kind string, roundID, uid, pid int64) ([]*repo.Submission, error)
	SaveClassMarks(ctx context.Context, uid int64, pins, order []int64) error
	ScanDuplicateProblems(ctx context.Context) ([]repo.DuplicateProblemGroup, error)
	SeedMarked(ctx context.Context) (bool, error)
	SetAssignmentProblems(ctx context.Context, id int64, pids []int64) error
	SetClassMemberRole(ctx context.Context, classID, uid int64, role string) error
	SetClassPrivate(ctx context.Context, classID int64, private bool) error
	SetDiscussReplyAccepted(ctx context.Context, rid int64, accepted bool) error
	SetDiscussTopicFlags(ctx context.Context, tid int64, pinned, featured, locked, private *bool) error
	SetProblemHidden(ctx context.Context, pid int64, hidden bool) error
	SetProblemStatement(ctx context.Context, pid int64, statement string) error
	SetRoundFileIO(ctx context.Context, kind string, id int64, enabled bool) error
	SetSolutionPinned(ctx context.Context, sid int64, pinned bool) error
	SetUserEmail(ctx context.Context, uid int64, email string) error
	SolvedDifficultyCounts(ctx context.Context, uids []int64) (map[int64][10]int64, error)
	SoftDeleteDiscussReply(ctx context.Context, rid int64) error
	SoftDeleteDiscussTopic(ctx context.Context, tid int64, byUID int64) error
	StartChallenge(ctx context.Context, uid int64, p *repo.Problem) (*repo.Challenge, error)
	SumXpAdjust(ctx context.Context, uids []int64) (map[int64]int64, error)
	SyncUnameReferences(ctx context.Context, uid int64, uname string) error
	TagRefProblemsField(ctx context.Context, field string, tagIDs []int64) ([]*repo.Problem, error)
	TagUsageCountsField(ctx context.Context, field string) (map[int64]int64, error)
	TestDataDownloadAllowed(ctx context.Context, uid, pid int64, limit int) (bool, error)
	TestDataDownloadedToday(ctx context.Context, uid int64, exclude []int64) (int64, error)
	ToggleDiscussLike(ctx context.Context, targetType string, targetID, uid int64) (liked bool, count int64, err error)
	TransferClass(ctx context.Context, classID, fromUID, toUID int64) error
	UnbindEmail(ctx context.Context, uid int64) error
	UnbindTagsField(ctx context.Context, field string, problemIDs, tagIDs []int64) (int64, error)
	UnfoldDiscussTarget(ctx context.Context, targetType string, targetID int64) error
	UnreadNotificationCount(ctx context.Context, uid int64) (int64, error)
	UpdateAssignment(ctx context.Context, id int64, p repo.AssignmentPatch) error
	UpdateClass(ctx context.Context, classID int64, p repo.ClassPatch) error
	UpdateDiscussTopicContent(ctx context.Context, tid int64, title, content, kind string, relatedPids []int64, listID, tagID int64) error
	UpdateLevelConfig(ctx context.Context, cfg *repo.LevelConfig) error
	UpdateNotifyEmailPrefs(ctx context.Context, id int64, cats []string) error
	UpdatePassword(ctx context.Context, uid int64, passwordHash string) error
	UpdateProblemList(ctx context.Context, id int64, p repo.ListPatch) error
	UpdateProblemMeta(ctx context.Context, pid int64, p repo.ProblemMetaPatch) error
	UpdateRole(ctx context.Context, id, name string, perms []string) error
	UpdateRoundFields(ctx context.Context, kind string, id int64, p repo.RoundPatch) error
	UpdateSolutionContent(ctx context.Context, sid int64, content string) error
	UpdateSystemSettings(ctx context.Context, s *repo.SystemSettings) error
	UpdateUser(ctx context.Context, id int64, p repo.UserPatch) error
	UploadsUsedBytes(ctx context.Context, uid int64) (int64, error)
	UpsertNotification(ctx context.Context, uid int64, in repo.NotifyInput, retentionDays int) error
	UserBestStatuses(ctx context.Context, uid int64, pids []int64) (map[int64]repo.UserBestStatus, error)
	UserContests(ctx context.Context, uid int64) ([]*repo.Contest, error)
	UserFirstAccepted(ctx context.Context, uid int64) ([]repo.FirstACEntry, error)
	UserProblemSubmissions(ctx context.Context, uid, pid int64) ([]*repo.Submission, error)
	UserRecentSubmissions(ctx context.Context, uid int64, limit int) ([]*repo.Submission, error)
	UserSubmissionStats(ctx context.Context, uid int64) (total, accepted int64, err error)
	ValidateMetaTagRefs(ctx context.Context, ids []int64) error
	ValidateTagRefs(ctx context.Context, ids []int64) error
	VisibleProblemAt(ctx context.Context, difficulty int, skip int64) (*repo.FeaturedRow, error)
}

// 编译期断言：生产形态 *repo.Repo 恒实现 RepoAPI（repo 侧新增方法而接口未跟随时，
// 这里立刻编译失败提醒同步——接口由调用面生成，只增不减）。
var _ RepoAPI = (*repo.Repo)(nil)
