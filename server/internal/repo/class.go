package repo

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ClassProblemRef 班级对题库题目的引用。Hidden=true 表示仅本班成员可见
// （只影响班级页展示，全局题库恒可见——见 DEVELOPMENT.md 需求结论）。
type ClassProblemRef struct {
	PID    int64 `bson:"pid"`
	Hidden bool  `bson:"hidden"`
}

// Class 班级。题目全局共享（引用实现）：班级通过 Problems 引用题目，不持有题目。
type Class struct {
	ID         int64             `bson:"_id"`
	Name       string            `bson:"name"`
	Intro      string            `bson:"intro"`
	OwnerID    int64             `bson:"ownerId"`
	InviteCode string            `bson:"inviteCode"`
	Problems   []ClassProblemRef `bson:"problems"`
	CreatedAt  time.Time         `bson:"createdAt"`
}

// 班级成员角色
const (
	ClassRoleOwner  = "owner"
	ClassRoleAdmin  = "admin"
	ClassRoleMember = "member"
)

// ClassMember 班级成员（冗余 uname 供成员列表展示）。
type ClassMember struct {
	ClassID  int64     `bson:"classId"`
	UID      int64     `bson:"uid"`
	Uname    string    `bson:"uname"`
	Role     string    `bson:"role"` // owner / admin / member
	JoinedAt time.Time `bson:"joinedAt"`
}

// inviteAlphabet 去掉易混淆字符（0/O/1/I/L）的邀请码字母表。
const inviteAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

func genInviteCode() (string, error) {
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b), nil
}

func (r *Repo) CreateClass(ctx context.Context, name, intro string, ownerID int64, ownerUname string) (*Class, error) {
	code, err := genInviteCode()
	if err != nil {
		return nil, err
	}
	c := &Class{
		ID:         0,
		Name:       name,
		Intro:      intro,
		OwnerID:    ownerID,
		InviteCode: code,
		Problems:   []ClassProblemRef{},
		CreatedAt:  time.Now(),
	}
	c.ID, err = r.nextSeq(ctx, "class")
	if err != nil {
		return nil, err
	}
	if _, err := r.db.Collection("classes").InsertOne(ctx, c); err != nil {
		return nil, err
	}
	// 建班人即首位成员（owner）
	_, err = r.db.Collection("classmembers").InsertOne(ctx, ClassMember{
		ClassID: c.ID, UID: ownerID, Uname: ownerUname, Role: ClassRoleOwner, JoinedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *Repo) GetClass(ctx context.Context, id int64) (*Class, error) {
	var c Class
	if err := r.db.Collection("classes").FindOne(ctx, bson.M{"_id": id}).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ClassCard 班级卡片（主页/班级列表）。
type ClassCard struct {
	ID           int64  `bson:"_id"          json:"id"`
	Name         string `bson:"name"         json:"name"`
	Intro        string `bson:"intro"        json:"intro"`
	MemberCount  int64  `bson:"memberCount"  json:"member_count"`
	ProblemCount int64  `bson:"problemCount" json:"problem_count"`
}

// ListClasses 全部班级卡片（按创建时间正序）。题数 = 各作业题目数之和。
func (r *Repo) ListClasses(ctx context.Context) ([]ClassCard, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$lookup", Value: bson.M{
			"from":         "classmembers",
			"localField":   "_id",
			"foreignField": "classId",
			"as":           "m",
		}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         "assignments",
			"localField":   "_id",
			"foreignField": "classId",
			"as":           "a",
		}}},
		{{Key: "$project", Value: bson.M{
			"name":        1,
			"intro":       1,
			"memberCount": bson.M{"$size": "$m"},
			"problemCount": bson.M{"$reduce": bson.M{
				"input":        "$a",
				"initialValue": 0,
				"in": bson.M{"$add": bson.A{
					"$$value",
					bson.M{"$size": bson.M{"$ifNull": bson.A{"$$this.problems", bson.A{}}}},
				}},
			}},
		}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := r.db.Collection("classes").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	cards := make([]ClassCard, 0)
	if err := cursor.All(ctx, &cards); err != nil {
		return nil, err
	}
	return cards, nil
}

func (r *Repo) JoinClass(ctx context.Context, classID, uid int64, uname, code string) error {
	c, err := r.GetClass(ctx, classID)
	if err != nil {
		return err
	}
	if c.InviteCode != code {
		return ErrInviteCode
	}
	if ok, err := r.IsMember(ctx, classID, uid); err != nil {
		return err
	} else if ok {
		return nil // 已是成员，幂等
	}
	_, err = r.db.Collection("classmembers").InsertOne(ctx, ClassMember{
		ClassID: classID, UID: uid, Uname: uname, Role: ClassRoleMember, JoinedAt: time.Now(),
	})
	return err
}

func (r *Repo) LeaveClass(ctx context.Context, classID, uid int64) error {
	res, err := r.db.Collection("classmembers").DeleteOne(ctx, bson.M{"classId": classID, "uid": uid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (r *Repo) IsMember(ctx context.Context, classID, uid int64) (bool, error) {
	err := r.db.Collection("classmembers").FindOne(ctx,
		bson.M{"classId": classID, "uid": uid},
		options.FindOne().SetProjection(bson.M{"_id": 1})).Err()
	if IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// ClassMemberRole 用户在班级内的角色；非成员返回 ("", nil)。
func (r *Repo) ClassMemberRole(ctx context.Context, classID, uid int64) (string, error) {
	var m struct {
		Role string `bson:"role"`
	}
	err := r.db.Collection("classmembers").FindOne(ctx,
		bson.M{"classId": classID, "uid": uid},
		options.FindOne().SetProjection(bson.M{"role": 1})).Decode(&m)
	if IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if m.Role == "" { // 旧数据兜底
		return ClassRoleMember, nil
	}
	return m.Role, nil
}

// SetClassMemberRole 设置成员角色（owner 角色不可改，权限由调用方校验）。
func (r *Repo) SetClassMemberRole(ctx context.Context, classID, uid int64, role string) error {
	res, err := r.db.Collection("classmembers").UpdateOne(ctx,
		bson.M{"classId": classID, "uid": uid, "role": bson.M{"$ne": ClassRoleOwner}},
		bson.M{"$set": bson.M{"role": role}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (r *Repo) ListMembers(ctx context.Context, classID int64) ([]ClassMember, error) {
	cursor, err := r.db.Collection("classmembers").Find(ctx,
		bson.M{"classId": classID}, options.Find().SetSort(bson.D{{Key: "joinedAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	list := make([]ClassMember, 0)
	if err := cursor.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ImportMembers 按用户名批量导入已有用户，返回导入数量。
func (r *Repo) ImportMembers(ctx context.Context, classID int64, usernames []string) (int, error) {
	added := 0
	for _, name := range usernames {
		u, _, err := r.GetUserByUsername(ctx, name)
		if IsNotFound(err) {
			continue // 不存在的用户名跳过
		}
		if err != nil {
			return added, err
		}
		ok, err := r.IsMember(ctx, classID, u.ID)
		if err != nil {
			return added, err
		}
		if ok {
			continue
		}
		if _, err := r.db.Collection("classmembers").InsertOne(ctx, ClassMember{
			ClassID: classID, UID: u.ID, Uname: u.Username, Role: ClassRoleMember, JoinedAt: time.Now(),
		}); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func (r *Repo) AddClassProblem(ctx context.Context, classID, pid int64, hidden bool) error {
	_, err := r.db.Collection("classes").UpdateOne(ctx,
		bson.M{"_id": classID},
		bson.M{"$addToSet": bson.M{"problems": ClassProblemRef{PID: pid, Hidden: hidden}}})
	return err
}

func (r *Repo) RemoveClassProblem(ctx context.Context, classID, pid int64) error {
	_, err := r.db.Collection("classes").UpdateOne(ctx,
		bson.M{"_id": classID}, bson.M{"$pull": bson.M{"problems": pid}})
	return err
}

func (r *Repo) RegenInviteCode(ctx context.Context, classID int64) (string, error) {
	code, err := genInviteCode()
	if err != nil {
		return "", err
	}
	_, err = r.db.Collection("classes").UpdateOne(ctx,
		bson.M{"_id": classID}, bson.M{"$set": bson.M{"inviteCode": code}})
	return code, err
}

// MyClassIDs 用户加入的全部班级 ID。
func (r *Repo) MyClassIDs(ctx context.Context, uid int64) ([]int64, error) {
	cursor, err := r.db.Collection("classmembers").Find(ctx,
		bson.M{"uid": uid}, options.Find().SetProjection(bson.M{"classId": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var members []struct {
		ClassID int64 `bson:"classId"`
	}
	if err := cursor.All(ctx, &members); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ClassID)
	}
	return ids, nil
}

// MyClasses 用户加入的班级列表（含计数）。
func (r *Repo) MyClasses(ctx context.Context, uid int64) ([]ClassCard, error) {
	cursor, err := r.db.Collection("classmembers").Find(ctx,
		bson.M{"uid": uid}, options.Find().SetProjection(bson.M{"classId": 1}))
	if err != nil {
		return nil, err
	}
	var members []struct {
		ClassID int64 `bson:"classId"`
	}
	if err := cursor.All(ctx, &members); err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	all, err := r.ListClasses(ctx)
	if err != nil {
		return nil, err
	}
	mine := make([]ClassCard, 0, len(members))
	for _, m := range members {
		for _, c := range all {
			if c.ID == m.ClassID {
				mine = append(mine, c)
			}
		}
	}
	return mine, nil
}

var ErrInviteCode = errors.New("邀请码错误")
