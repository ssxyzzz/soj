// Package repo 是 MongoDB 数据访问层：连接管理、计数器、索引与启动引导。
package repo

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"soj/internal/richtext"
	"soj/internal/testdata"
)

// Repo 聚合所有集合的访问入口。
type Repo struct {
	cli         *mongo.Client
	db          *mongo.Database
	Users       *mongo.Collection
	Roles       *mongo.Collection
	Problems    *mongo.Collection
	Submissions *mongo.Collection
	Counters    *mongo.Collection
	Uploads     *mongo.Collection
	Solutions   *mongo.Collection
	Tags        *mongo.Collection
}

// Connect 建立 Mongo 连接（容器编排下依赖可能晚就绪，带重试等待）。
func Connect(ctx context.Context, uri, dbName string) (*Repo, error) {
	var cli *mongo.Client
	var err error
	for i := 0; i < 30; i++ {
		cli, err = mongo.Connect(options.Client().ApplyURI(uri))
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = cli.Ping(pingCtx, nil)
			cancel()
			if err == nil {
				break
			}
		}
		log.Printf("等待 MongoDB 就绪: %v", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("连接 MongoDB 失败: %w", err)
	}
	db := cli.Database(dbName)
	return &Repo{
		cli:         cli,
		db:          db,
		Users:       db.Collection("users"),
		Roles:       db.Collection("roles"),
		Problems:    db.Collection("problems"),
		Submissions: db.Collection("submissions"),
		Counters:    db.Collection("counters"),
		Uploads:     db.Collection("uploads"),
		Solutions:   db.Collection("solutions"),
		Tags:        db.Collection("tags"),
	}, nil
}

func (r *Repo) Close(ctx context.Context) error { return r.cli.Disconnect(ctx) }

// Ping 健康检查用。
func (r *Repo) Ping(ctx context.Context) error { return r.cli.Ping(ctx, nil) }

// Bootstrap 创建索引、初始化 admin 账号、把上次异常退出遗留的 Judging 重置为 Pending。
// store 用于需要读测试数据文件的一次性迁移。幂等，每次启动都执行。
func (r *Repo) Bootstrap(ctx context.Context, store testdata.Provider) error {
	_, err := r.Submissions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "_id", Value: 1}}},
		// v0.7.1 队列领取排序：优先级降序 + 先来先服务（批量重测不挤占学生新提交）
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "priority", Value: -1}, {Key: "_id", Value: 1}}},
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "pid", Value: 1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "contestId", Value: 1}, {Key: "uid", Value: 1}}},
	})
	if err != nil {
		return err
	}
	_, err = r.Users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "uname", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// 数据包下载记账（v0.8.3）：每日限额按 (uid, day) 数不同题；同题同天重复下载去重
	_, err = r.db.Collection("dataDownloads").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "day", Value: 1}}},
		{Keys: bson.D{{Key: "uid", Value: 1}, {Key: "pid", Value: 1}, {Key: "day", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	if err != nil {
		return err
	}

	// 初始管理员：users 为空时创建 admin / admin123（root 角色）
	if n, err := r.Users.CountDocuments(ctx, bson.M{}); err != nil {
		return err
	} else if n == 0 {
		uid, err := r.CreateUserWithPassword(ctx, "admin", "admin123", "管理员", RoleRoot)
		if err != nil {
			return err
		}
		_ = uid
		log.Printf("[boot] 已创建初始管理员: admin / admin123（请尽快修改密码）")
	}

	// 角色种子：内置 default/admin/root（不存在才建，不覆盖 root 的调整）
	if err := r.EnsureBuiltinRoles(ctx); err != nil {
		return err
	}

	// 一次性数据迁移（角色体系、班级引用结构等），meta.migrations 记录完成标记
	if err := r.runMigrations(ctx, store); err != nil {
		return err
	}

	// 题目计数器对齐现存最大 _id（幂等守卫，每次启动跑：计数器落后会让建题撞 _id）
	if err := r.syncProblemCounter(ctx); err != nil {
		return err
	}

	// v0.7.1 队列优先级回填：存量提交补默认高档（幂等，只有升级后首次有改动）
	if res, err := r.Submissions.UpdateMany(ctx,
		bson.M{"priority": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"priority": PriorityHigh}}); err == nil && res.ModifiedCount > 0 {
		log.Printf("[boot] 已为 %d 条存量提交补队列优先级默认值", res.ModifiedCount)
	}

	// 遗留的 Judging（进程被杀时没判完）重新排队
	res, err := r.Submissions.UpdateMany(ctx,
		bson.M{"status": "Judging"},
		bson.M{"$set": bson.M{"status": "Pending"},
			"$unset": bson.M{"judge": "", "claimedAt": "", "heartbeatAt": ""}})
	if err == nil && res.ModifiedCount > 0 {
		log.Printf("[boot] 已把 %d 条遗留 Judging 提交重新排队", res.ModifiedCount)
	}
	return err
}

// runMigrations 按需执行一次性数据迁移，meta.migrations 记录各项完成标记。
func (r *Repo) runMigrations(ctx context.Context, store testdata.Provider) error {
	meta := r.db.Collection("meta")
	var done bson.M
	err := meta.FindOne(ctx, bson.M{"_id": "migrations"}).Decode(&done)
	if err != nil && !IsNotFound(err) {
		return err
	}
	flagDone := func(k string) bool {
		b, _ := done[k].(bool)
		return b
	}
	setFlag := func(k string) error {
		if _, err := meta.InsertOne(ctx, bson.M{"_id": "migrations"}); err != nil && !mongo.IsDuplicateKeyError(err) {
			return err
		}
		_, err := meta.UpdateOne(ctx, bson.M{"_id": "migrations"}, bson.M{"$set": bson.M{k: true}})
		return err
	}

	if !flagDone("rolesV2") {
		if err := r.migrateRoles(ctx); err != nil {
			return err
		}
		if err := setFlag("rolesV2"); err != nil {
			return err
		}
		log.Printf("[boot] 角色体系已迁移到 default/admin/root")
	}
	if !flagDone("classRefsV1") {
		if err := r.migrateClassRefs(ctx); err != nil {
			return err
		}
		if err := setFlag("classRefsV1"); err != nil {
			return err
		}
		log.Printf("[boot] 班级题目引用结构已迁移（增加 hidden 标记）")
	}
	if !flagDone("memberRolesV1") {
		if err := r.migrateMemberRoles(ctx); err != nil {
			return err
		}
		if err := setFlag("memberRolesV1"); err != nil {
			return err
		}
		log.Printf("[boot] 班级成员角色已迁移（owner/admin/member）")
	}
	if !flagDone("assignmentsV1") {
		if err := r.migrateAssignments(ctx); err != nil {
			return err
		}
		if err := setFlag("assignmentsV1"); err != nil {
			return err
		}
		log.Printf("[boot] 班级题目引用已迁移为作业")
	}
	if !flagDone("examsV1") {
		if err := r.migrateExams(ctx); err != nil {
			return err
		}
		if err := setFlag("examsV1"); err != nil {
			return err
		}
		log.Printf("[boot] 班级比赛已迁移为考试（exams 集合）")
	}
	if !flagDone("samplesMdV1") {
		if err := r.migrateSamplesMd(ctx, store); err != nil {
			return err
		}
		if err := setFlag("samplesMdV1"); err != nil {
			return err
		}
		log.Printf("[boot] 样例已并入题面（Markdown）")
	}
	if !flagDone("solutionManageV1") {
		// v0.5.2-1：题解管理权限位（初始归属 admin）补发存量 admin 角色
		if _, err := r.Roles.UpdateOne(ctx,
			bson.M{"_id": RoleAdmin},
			bson.M{"$addToSet": bson.M{"perms": PermSolutionManage}}); err != nil {
			return err
		}
		if err := setFlag("solutionManageV1"); err != nil {
			return err
		}
		log.Printf("[boot] admin 角色已补发题解管理权限位（solution.manage）")
	}
	if !flagDone("submissionViewUserV1") {
		// v0.5.2-2：查看用户提交明细权限位（初始归属 admin）补发存量 admin 角色
		if _, err := r.Roles.UpdateOne(ctx,
			bson.M{"_id": RoleAdmin},
			bson.M{"$addToSet": bson.M{"perms": PermSubmissionViewUser}}); err != nil {
			return err
		}
		if err := setFlag("submissionViewUserV1"); err != nil {
			return err
		}
		log.Printf("[boot] admin 角色已补发查看用户提交明细权限位（submission.view.user）")
	}
	if !flagDone("nicknameEditV1") {
		// v0.6.x：真名下线的同时开放本人昵称修改。旧「查看真实姓名」权限位已从目录删除，
		// 这里顺手清掉角色文档里的残留（EffectivePerms 也会过滤，双保险）。
		if _, err := r.Roles.UpdateMany(ctx,
			bson.M{},
			bson.M{"$pull": bson.M{"perms": "user.realname.view"}}); err != nil {
			return err
		}
		if _, err := r.Roles.UpdateMany(ctx,
			bson.M{"_id": bson.M{"$in": []string{RoleDefault, RoleAdmin}}},
			bson.M{"$addToSet": bson.M{"perms": PermUserNicknameEdit}}); err != nil {
			return err
		}
		if err := setFlag("nicknameEditV1"); err != nil {
			return err
		}
		log.Printf("[boot] 已补发昵称修改权限位（user.nickname.edit），真实姓名权限位已清理")
	}
	if !flagDone("roundFlagsV1") {
		// v0.7：比赛/考试新增「开启成绩表」「赛时隐藏成绩」两个开关。存量场次补默认值：
		// 两者都开——成绩表保持现状可见规则，赛时隐藏成绩保持 OI 现状。
		for _, col := range []*mongo.Collection{r.roundsCol(KindContest), r.roundsCol(KindExam)} {
			if _, err := col.UpdateMany(ctx,
				bson.M{"scoreboard": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"scoreboard": true}}); err != nil {
				return err
			}
			if _, err := col.UpdateMany(ctx,
				bson.M{"hideDuring": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"hideDuring": true}}); err != nil {
				return err
			}
		}
		if err := setFlag("roundFlagsV1"); err != nil {
			return err
		}
		log.Printf("[boot] 比赛/考试已补默认开关（成绩表开、赛时隐藏成绩开）")
	}
	if !flagDone("classLeaveV1") {
		// v0.7.2：退出班级改为权限位（class.leave）管控，default 刻意不给；
		// admin 角色补发该位（root 走 "*" 恒通，无需补）
		if _, err := r.Roles.UpdateOne(ctx,
			bson.M{"_id": RoleAdmin},
			bson.M{"$addToSet": bson.M{"perms": PermClassLeave}}); err != nil {
			return err
		}
		if err := setFlag("classLeaveV1"); err != nil {
			return err
		}
		log.Printf("[boot] admin 角色已补发退出班级权限位（class.leave）")
	}
	if !flagDone("adminProblemViewAllV1") {
		// v0.7.2：存量 admin 角色补发「题库全量可见」——builtin 定义一直包含该位，
		// 但早期创建的角色文档没有它且从未补发过（导致存量 admin 看不到隐藏题/锁定标记）
		if _, err := r.Roles.UpdateOne(ctx,
			bson.M{"_id": RoleAdmin},
			bson.M{"$addToSet": bson.M{"perms": PermProblemViewAll}}); err != nil {
			return err
		}
		if err := setFlag("adminProblemViewAllV1"); err != nil {
			return err
		}
		log.Printf("[boot] admin 角色已补发题库全量可见权限位（problem.view.all）")
	}
	if !flagDone("objQuestionsV1") {
		// v0.9.3：客观题从单题（question）升级为多小题（questions[]）。
		// 存量：把 question 文档搬进 questions[1]（分值 100）；提交侧 answer/blanks
		// 搬进 objAnswers[0]，旧字段 $unset。
		cur, err := r.Problems.Find(ctx, bson.M{"question": bson.M{"$exists": true}})
		if err != nil {
			return err
		}
		var plist []*Problem
		if err := cur.All(ctx, &plist); err != nil {
			return err
		}
		for _, p := range plist {
			var raw struct {
				Question *ObjectiveQuestion `bson:"question"`
			}
			if err := r.Problems.FindOne(ctx, bson.M{"_id": p.ID}).Decode(&raw); err != nil || raw.Question == nil {
				continue
			}
			q := *raw.Question
			if q.Score <= 0 {
				q.Score = 100
			}
			if _, err := r.Problems.UpdateOne(ctx, bson.M{"_id": p.ID}, bson.M{
				"$set":   bson.M{"questions": []ObjectiveQuestion{q}},
				"$unset": bson.M{"question": ""},
			}); err != nil {
				return err
			}
		}
		// 提交：answer/blanks → objAnswers[0]
		scur, err := r.Submissions.Find(ctx, bson.M{"$or": []bson.M{
			{"answer": bson.M{"$exists": true}}, {"blanks": bson.M{"$exists": true}},
		}})
		if err != nil {
			return err
		}
		var slist []*Submission
		if err := scur.All(ctx, &slist); err != nil {
			return err
		}
		for _, sub := range slist {
			oa := ObjectiveAnswer{}
			if len(sub.Answer) > 0 {
				oa.Answer = sub.Answer
			}
			if len(sub.Blanks) > 0 {
				oa.Blanks = sub.Blanks
			}
			if _, err := r.Submissions.UpdateOne(ctx, bson.M{"_id": sub.ID}, bson.M{
				"$set":   bson.M{"objAnswers": []ObjectiveAnswer{oa}},
				"$unset": bson.M{"answer": "", "blanks": ""},
			}); err != nil {
				return err
			}
		}
		if err := setFlag("objQuestionsV1"); err != nil {
			return err
		}
		log.Printf("[boot] 客观题已升级多小题（%d 题 / %d 提交迁移）", len(plist), len(slist))
	}
	if !flagDone("dataDownloadPermV1") {
		// v0.8.3：下载数据权限位（初始归属 admin）补发存量 admin 角色
		if _, err := r.Roles.UpdateOne(ctx,
			bson.M{"_id": RoleAdmin},
			bson.M{"$addToSet": bson.M{"perms": PermProblemDataDownload}}); err != nil {
			return err
		}
		if err := setFlag("dataDownloadPermV1"); err != nil {
			return err
		}
		log.Printf("[boot] admin 角色已补发下载数据权限位（problem.data.download）")
	}
	if !flagDone("contentMdV1") {
		if err := r.migrateContentMd(ctx); err != nil {
			return err
		}
		if err := setFlag("contentMdV1"); err != nil {
			return err
		}
		log.Printf("[boot] HTML 富文本字段已回迁为 markdown（v0.8 编辑器换回 Vditor）")
	}
	return nil
}

// contentMdTargets v0.8 回迁目标：集合 → {HTML 字段, md 字段}（settings 单文档双字段单独处理）。
var contentMdTargets = []struct{ coll, htmlField, mdField string }{
	{"problems", "contentHtml", "statement"},
	{"contests", "introHtml", "intro"},
	{"exams", "introHtml", "intro"},
	{"solutions", "contentHtml", "content"},
	{"assignments", "introHtml", "intro"},
	{"classes", "introHtml", "intro"},
}

// migrateContentMd v0.8 编辑器推倒重来（Vditor 回归）：存量 HTML 富文本字段一次性转回 md。
// 「编辑即迁移」语义下 HTML 是编辑后的新内容，转换结果覆盖 md 字段后删除 HTML 字段；
// bson.M 原始读写不经过结构体（结构体届时才删字段）。幂等：无 HTML 字段的文档天然跳过，
// Bootstrap 在服务流量前执行，无并发写竞争。
func (r *Repo) migrateContentMd(ctx context.Context) error {
	convert := func(coll *mongo.Collection, htmlField, mdField string) (int, error) {
		cursor, err := coll.Find(ctx,
			bson.M{htmlField: bson.M{"$exists": true, "$ne": ""}},
			options.Find().SetProjection(bson.M{"_id": 1, htmlField: 1}))
		if err != nil {
			return 0, err
		}
		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			return 0, err
		}
		for _, d := range docs {
			html, _ := d[htmlField].(string)
			if _, err := coll.UpdateOne(ctx,
				bson.M{"_id": d["_id"]},
				bson.M{
					"$set":   bson.M{mdField: richtext.HTMLToMd(html)},
					"$unset": bson.M{htmlField: ""},
				}); err != nil {
				return 0, err
			}
		}
		return len(docs), nil
	}

	total := 0
	for _, t := range contentMdTargets {
		n, err := convert(r.db.Collection(t.coll), t.htmlField, t.mdField)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.coll, t.htmlField, err)
		}
		total += n
	}

	// settings 单文档双字段：homeNotice / backupGuide
	var s bson.M
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&s)
	if err == nil {
		set, unset := bson.M{}, bson.M{}
		for _, f := range []struct{ html, md string }{
			{"homeNoticeHtml", "homeNotice"}, {"backupGuideHtml", "backupGuide"},
		} {
			if html, ok := s[f.html].(string); ok && html != "" {
				set[f.md] = richtext.HTMLToMd(html)
				unset[f.html] = ""
				total++
			}
		}
		if len(set) > 0 {
			if _, err := r.db.Collection("settings").UpdateOne(ctx,
				bson.M{"_id": "system"}, bson.M{"$set": set, "$unset": unset}); err != nil {
				return err
			}
		}
	} else if !IsNotFound(err) {
		return err
	}
	if total > 0 {
		log.Printf("[boot] v0.8 回迁：%d 篇 HTML 富文本已转回 markdown", total)
	}
	return nil
}

// seedDone 读 meta.migrations 的 seedV1 标记：种子题目只在首次部署播种一次，
// 置位后启动不再播种（此后加题走 make import / 网页建题，见 importer.SeedAll）。
func (r *Repo) seedDone(ctx context.Context) (bool, error) {
	var done bson.M
	err := r.db.Collection("meta").FindOne(ctx, bson.M{"_id": "migrations"}).Decode(&done)
	if IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	b, _ := done["seedV1"].(bool)
	return b, nil
}

// markSeedDone 置位 seedV1（全量播种成功后才调用；中途失败留待下次启动重试）。
func (r *Repo) markSeedDone(ctx context.Context) error {
	meta := r.db.Collection("meta")
	if _, err := meta.InsertOne(ctx, bson.M{"_id": "migrations"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	_, err := meta.UpdateOne(ctx, bson.M{"_id": "migrations"}, bson.M{"$set": bson.M{"seedV1": true}})
	return err
}

// migrateExams 考试拆分迁移：把 contests 里 classId≠0 的班级比赛搬入 exams 集合，
// 其提交的 contestId/contestTitle 同步改名为 examId/examTitle。
func (r *Repo) migrateExams(ctx context.Context) error {
	cursor, err := r.db.Collection("contests").Find(ctx,
		bson.M{"classId": bson.M{"$gt": 0}})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var rounds []bson.M
	if err := cursor.All(ctx, &rounds); err != nil {
		return err
	}
	for _, doc := range rounds {
		cid, ok := doc["_id"].(int64)
		if !ok {
			continue
		}
		if _, err := r.db.Collection("exams").InsertOne(ctx, doc); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				continue // 已迁移过
			}
			return err
		}
		if _, err := r.Submissions.UpdateMany(ctx,
			bson.M{"contestId": cid},
			bson.M{"$rename": bson.M{"contestId": "examId", "contestTitle": "examTitle"}}); err != nil {
			return err
		}
		if _, err := r.db.Collection("contests").DeleteOne(ctx, bson.M{"_id": cid}); err != nil {
			return err
		}
	}
	return nil
}

// migrateAssignments 作业化迁移：每个班级已引用的题目装进一个「默认作业」。
func (r *Repo) migrateAssignments(ctx context.Context) error {
	cursor, err := r.db.Collection("classes").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var classes []bson.M
	if err := cursor.All(ctx, &classes); err != nil {
		return err
	}
	for _, doc := range classes {
		cid, ok := doc["_id"].(int64)
		if !ok {
			continue
		}
		raw, _ := doc["problems"].(bson.A)
		pids := make([]int64, 0, len(raw))
		for _, v := range raw {
			switch ref := v.(type) {
			case int64: // 旧格式：纯 pid
				pids = append(pids, ref)
			case bson.M: // {pid, hidden} 引用对象
				if pid, ok := ref["pid"].(int64); ok {
					pids = append(pids, pid)
				}
			case bson.D: // 驱动默认把嵌套文档解码为 bson.D
				for _, e := range ref {
					if e.Key == "pid" {
						if pid, ok := e.Value.(int64); ok {
							pids = append(pids, pid)
						}
					}
				}
			}
		}
		if len(pids) == 0 {
			continue
		}
		id, err := r.nextSeq(ctx, "assignment")
		if err != nil {
			return err
		}
		_, err = r.db.Collection("assignments").InsertOne(ctx, Assignment{
			ID: id, ClassID: cid, Title: "默认作业", Problems: pids, CreatedAt: time.Now(),
		})
		if err != nil {
			return err
		}
		_, _ = r.db.Collection("classes").UpdateOne(ctx,
			bson.M{"_id": cid}, bson.M{"$set": bson.M{"problems": bson.A{}}})
	}
	return nil
}

// migrateMemberRoles 班级成员角色迁移：建班人 → owner，其余无角色的 → member。
func (r *Repo) migrateMemberRoles(ctx context.Context) error {
	cursor, err := r.db.Collection("classes").Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	var classes []bson.M
	if err := cursor.All(ctx, &classes); err != nil {
		return err
	}
	for _, doc := range classes {
		cid, ok1 := doc["_id"].(int64)
		owner, ok2 := doc["ownerId"].(int64)
		if !ok1 || !ok2 {
			continue
		}
		if _, err := r.db.Collection("classmembers").UpdateMany(ctx,
			bson.M{"classId": cid, "uid": owner},
			bson.M{"$set": bson.M{"role": ClassRoleOwner}}); err != nil {
			return err
		}
		if _, err := r.db.Collection("classmembers").UpdateMany(ctx,
			bson.M{"classId": cid, "uid": bson.M{"$ne": owner}, "role": bson.M{"$in": bson.A{nil, ""}}},
			bson.M{"$set": bson.M{"role": ClassRoleMember}}); err != nil {
			return err
		}
	}
	return nil
}

// migrateClassRefs 班级引用结构迁移：旧 []int64 → []{pid, hidden:false}。
func (r *Repo) migrateClassRefs(ctx context.Context) error {
	// 只挑 problems 第一个元素仍是标量的旧文档
	filter := bson.M{"problems.0": bson.M{"$type": bson.A{"long", "int", "double", "decimal"}}}
	cursor, err := r.db.Collection("classes").Find(ctx, filter)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		raw, _ := doc["problems"].(bson.A)
		refs := make(bson.A, 0, len(raw))
		for _, v := range raw {
			if pid, ok := v.(int64); ok {
				refs = append(refs, ClassProblemRef{PID: pid})
			}
		}
		if _, err := r.db.Collection("classes").UpdateOne(ctx,
			bson.M{"_id": doc["_id"]},
			bson.M{"$set": bson.M{"problems": refs}}); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// nextSeq 原子自增计数器（_id 不够用时统一从这里取号）。
func (r *Repo) nextSeq(ctx context.Context, name string) (int64, error) {
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	err := r.Counters.FindOneAndUpdate(ctx,
		bson.M{"_id": name},
		bson.M{"$inc": bson.M{"seq": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	return doc.Seq, err
}

// syncProblemCounter 启动守卫：problem 计数器 $max 到题目集合现存最大 _id。
// 计数器一旦落后（手动修库 / 恢复旧备份），建题会取到已占用的号直接撞 _id（E11000）；
// $max 幂等只会前进不会回退，每次启动跑一遍兜底。题目集合为空时不建计数器文档。
func (r *Repo) syncProblemCounter(ctx context.Context) error {
	var last struct {
		ID int64 `bson:"_id"`
	}
	err := r.Problems.FindOne(ctx, bson.M{},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}}),
	).Decode(&last)
	if IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var before struct {
		Seq int64 `bson:"seq"`
	}
	err = r.Counters.FindOneAndUpdate(ctx,
		bson.M{"_id": "problem"},
		bson.M{"$max": bson.M{"seq": last.ID}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.Before),
	).Decode(&before)
	if IsNotFound(err) {
		err = nil // 计数器文档缺失视为 0，本次 $max 已把它拉齐
	}
	if err == nil && before.Seq < last.ID {
		log.Printf("[boot] problem 计数器已从 %d 对齐到最大题目 _id %d", before.Seq, last.ID)
	}
	return err
}
