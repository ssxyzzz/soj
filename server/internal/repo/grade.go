package repo

// 年级结构化与自动晋级（v0.34）：grade 自由文本 → gradeNo 枚举。
// 0=未设置、1–12=一年级…高三、13=已毕业（终态不再晋级）。每年 9/1（北京时间）
// 切换学年后懒触发一次晋级：1–11 各 +1、12→13、0/13 不动；跨多个学年（服务器长期
// 停机）按年差一次补齐再钳上界。settings.gradePromoYear 记最近已执行的学年起始年，
// findOneAndUpdate CAS 抢标记防并发重入；存量自由文本由 migrateGradeNo 一次性转档
// 并把标记垫到当前学年（迁移不触发晋级）。

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// GradeGraduated 已毕业终态档位。
const GradeGraduated = 13

// gradeNames 1–12 档显示名（一年级…六年级 / 初一…初三 / 高一…高三）。
var gradeNames = [12]string{"一年级", "二年级", "三年级", "四年级", "五年级", "六年级", "初一", "初二", "初三", "高一", "高二", "高三"}

// GradeName 档位 → 显示名；0/越界返回空串（未设置），13 返回「已毕业」。
func GradeName(no int) string {
	switch {
	case no >= 1 && no <= 12:
		return gradeNames[no-1]
	case no == GradeGraduated:
		return "已毕业"
	default:
		return ""
	}
}

// gradeAliases 存量文本/花名册填写的常见写法 → 档位（迁移与导入解析共用）。
var gradeAliases = func() map[string]int {
	m := map[string]int{}
	for i, name := range gradeNames {
		m[name] = i + 1
	}
	extra := map[string]int{
		"七年级": 7, "八年级": 8, "九年级": 9,
		"十年级": 10, "十一年级": 11, "十二年级": 12,
		"小一": 1, "小二": 2, "小三": 3, "小四": 4, "小五": 5, "小六": 6,
		"初一新生": 7, "高一新生": 10,
	}
	for k, v := range extra {
		m[k] = v
	}
	// 「7年级」…「12年级」数字变体
	for i := 1; i <= 12; i++ {
		m[strconv.Itoa(i)+"年级"] = i
	}
	return m
}()

// ParseGrade 年级文本 → 档位：中文别名 → 数字 1–12 → 空串/未知返回 0（未设置）。
func ParseGrade(s string) int {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0
	}
	if no, ok := gradeAliases[t]; ok {
		return no
	}
	if n, err := strconv.Atoi(t); err == nil && n >= 1 && n <= 12 {
		return n
	}
	return 0
}

// beijing 东八区（学年按北京时间 9/1 切换，与服务器时区无关）。
var beijing = time.FixedZone("Asia/Shanghai", 8*3600)

// SchoolYearStart 当前学年起始年：北京时间 9 月起（含 9/1）为新学年，否则归上一年。
func SchoolYearStart(t time.Time) int {
	bt := t.In(beijing)
	if bt.Month() >= time.September {
		return bt.Year()
	}
	return bt.Year() - 1
}

// nextPromoTime 下一晋级时刻（学年起始年 +1 的 9/1 00:00 北京时间），状态展示用。
func nextPromoTime(curYear int) time.Time {
	return time.Date(curYear+1, time.September, 1, 0, 0, 0, 0, beijing)
}

// readGradePromoYear 读 settings 的晋级标记（文档/字段缺失 = 0，从未执行）。
func (r *Repo) readGradePromoYear(ctx context.Context) (int, error) {
	var doc struct {
		GradePromoYear int `bson:"gradePromoYear"`
	}
	err := r.db.Collection("settings").FindOne(ctx, bson.M{"_id": "system"}).Decode(&doc)
	if IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.GradePromoYear, nil
}

// GradePromoInfo 晋级状态（后台展示）：当前学年、下一晋级时刻、已执行到的学年、
// 在档各档位人数（含未设置/已毕业，给「立即晋级」提供影响面预览）。
type GradePromoInfo struct {
	CurrentYear int       `json:"current_year"`
	NextAt      time.Time `json:"next_at"`
	LastYear    int       `json:"last_year"`
	Counts      [14]int64 `json:"counts"` // 下标 = 档位（0 未设置 … 13 已毕业）
}

func (r *Repo) GradePromotionInfo(ctx context.Context) (*GradePromoInfo, error) {
	cur := SchoolYearStart(time.Now())
	last, err := r.readGradePromoYear(ctx)
	if err != nil {
		return nil, err
	}
	info := &GradePromoInfo{CurrentYear: cur, NextAt: nextPromoTime(cur), LastYear: last}
	cursor, err := r.users.Aggregate(ctx, []bson.M{
		{"$group": bson.M{
			"_id": bson.M{"$ifNull": bson.A{"$gradeNo", 0}},
			"n":   bson.M{"$sum": 1},
		}},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID int64 `bson:"_id"`
			N  int64 `bson:"n"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		if row.ID >= 0 && row.ID <= GradeGraduated {
			info.Counts[row.ID] = row.N
		}
	}
	return info, cursor.Err()
}

// EnsureGradePromotion 懒触发晋级（Bootstrap + 读路径调用，幂等）：当前学年 > 标记时
// CAS 抢标记（旧值带回判年差），赢家按年差一次 $inc 补齐并钳 13；标记为 0（首次/
// 迁移垫底）只落基线不晋级。返回本次晋级的用户数。
func (r *Repo) EnsureGradePromotion(ctx context.Context) (int64, error) {
	cur := SchoolYearStart(time.Now())
	col := r.db.Collection("settings")
	// 文档兜底存在（不能靠 upsert——过滤条件含「值不满足」，文档已存在时不匹配会
	// 触发重复 _id 插入直接 E11000）
	if _, err := col.InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return 0, err
	}
	var doc struct {
		GradePromoYear int `bson:"gradePromoYear"`
	}
	// 抢标记：条件更新保证并发/多次调用只有一个赢家拿到执行权；不匹配（已执行到
	// 当前学年）= 无操作
	err := col.FindOneAndUpdate(ctx,
		bson.M{"_id": "system", "$or": bson.A{
			bson.M{"gradePromoYear": bson.M{"$lt": cur}},
			bson.M{"gradePromoYear": bson.M{"$exists": false}},
		}},
		bson.M{"$set": bson.M{"gradePromoYear": cur}},
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&doc)
	if IsNotFound(err) {
		return 0, nil // 别人已执行到当前学年
	}
	if err != nil {
		return 0, err
	}
	if doc.GradePromoYear <= 0 {
		return 0, nil // 首次基线（全新部署 / 迁移垫底不算晋级）
	}
	diff := cur - doc.GradePromoYear
	if diff <= 0 {
		return 0, nil
	}
	return r.promoteBy(ctx, int64(diff))
}

// promoteBy 年差晋级：1–12 档 $inc 年差（跨学年一次补齐），再钳 13（高三经年差
// 越过毕业线归已毕业）；未设置（无 gradeNo 字段）不动。返回位移人数。
func (r *Repo) promoteBy(ctx context.Context, diff int64) (int64, error) {
	res, err := r.users.UpdateMany(ctx,
		bson.M{"gradeNo": bson.M{"$gte": 1, "$lte": 12}},
		bson.M{"$inc": bson.M{"gradeNo": diff}})
	if err != nil {
		return 0, err
	}
	if _, err := r.users.UpdateMany(ctx,
		bson.M{"gradeNo": bson.M{"$gt": GradeGraduated}},
		bson.M{"$set": bson.M{"gradeNo": GradeGraduated}}); err != nil {
		return res.ModifiedCount, err
	}
	if res.ModifiedCount > 0 {
		slog.Info("年级晋级完成", "component", "boot", "action", "学年自动晋级",
			"years", diff, "promoted", res.ModifiedCount)
	}
	return res.ModifiedCount, nil
}

// migrateGradeNo 存量迁移（Bootstrap 一次性，flag=gradeNoV1）：自由文本 grade 按
// 别名表转 gradeNo（转不出的置未设置，由 root 补录），转完删旧字段并把晋级标记垫到
// 当前学年——迁移不算晋级。返回 (转档数, 无法识别置空数)。
func (r *Repo) migrateGradeNo(ctx context.Context) (int64, int64, error) {
	cursor, err := r.users.Find(ctx, bson.M{"grade": bson.M{"$exists": true, "$ne": ""}})
	if err != nil {
		return 0, 0, err
	}
	defer cursor.Close(ctx)
	var converted, unmapped int64
	for cursor.Next(ctx) {
		var u struct {
			ID    int64  `bson:"_id"`
			Grade string `bson:"grade"`
		}
		if err := cursor.Decode(&u); err != nil {
			return converted, unmapped, err
		}
		upd := bson.M{"$unset": bson.M{"grade": ""}}
		if no := ParseGrade(u.Grade); no > 0 {
			upd["$set"] = bson.M{"gradeNo": no}
			converted++
		} else {
			unmapped++ // 「2024级」等大学式写法：置未设置，root 用户管理补录
		}
		if _, err := r.users.UpdateOne(ctx, bson.M{"_id": u.ID}, upd); err != nil {
			return converted, unmapped, err
		}
	}
	if err := cursor.Err(); err != nil {
		return converted, unmapped, err
	}
	// 晋级标记垫底（仅从未设置时写入）：迁移落完档位从当前学年起算，9/1 前转档的
	// 高三不会立即被推成已毕业。文档兜底 + 不带 upsert 的条件写（同 EnsureGradePromotion，
	// 防「值不满足」分支触发重复 _id 插入）
	if _, err := r.db.Collection("settings").InsertOne(ctx, bson.M{"_id": "system"}); err != nil && !mongo.IsDuplicateKeyError(err) {
		return converted, unmapped, err
	}
	if _, err := r.db.Collection("settings").UpdateOne(ctx,
		bson.M{"_id": "system", "gradePromoYear": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"gradePromoYear": SchoolYearStart(time.Now())}}); err != nil {
		return converted, unmapped, err
	}
	if converted+unmapped > 0 {
		slog.Info("存量年级已结构化", "component", "boot",
			"converted", converted, "unmapped", unmapped)
	}
	return converted, unmapped, nil
}

// ---- 建号批量查重（v0.34 两段式导入预览用）----

// ExistingUserIDs 批量查已占用的用户 ID。
func (r *Repo) ExistingUserIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	cursor, err := r.users.Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			ID int64 `bson:"_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = true
	}
	return out, cursor.Err()
}

// ExistingUsernames 批量查已存在的用户名（小写归一——建号判重口径与 usernameRE 的
// 大小写不敏感一致）。
func (r *Repo) ExistingUsernames(ctx context.Context, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(names) == 0 {
		return out, nil
	}
	lower := make([]string, 0, len(names))
	for _, n := range names {
		lower = append(lower, strings.ToLower(n))
	}
	cursor, err := r.users.Find(ctx, bson.M{"uname": bson.M{"$in": lower}},
		options.Find().SetProjection(bson.M{"uname": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var row struct {
			Uname string `bson:"uname"`
		}
		if err := cursor.Decode(&row); err != nil {
			return nil, err
		}
		out[strings.ToLower(row.Uname)] = true
	}
	return out, cursor.Err()
}
