package repo

// 带资料建号契约（v0.97.8 管理员建号统一走 CreateUserWithProfile）：pwdRemind 随建号
// 落库自带（首登提醒改密，原 handler 补刀位退役）、性别/年级/学校/地区随号落库、
// 空资料不落字段。自动取号（id≤0）与指定 ID 双分支同验。本地无 Mongo 自动 skip。

import (
	"context"
	"testing"
)

func TestCreateUserWithProfile(t *testing.T) {
	r := openTestRepo(t)
	ctx := context.Background()

	// 自动取号分支（id=0）：pwdRemind 自带 + 四字段落库
	uid, err := r.CreateUserWithProfile(ctx, 0, "stu_a", "pwd", "小A", RoleDefault, UserProfile{
		Gender: "女", GradeNo: ParseGrade("六年级"), School: "实验中学", Region: "高新区",
	})
	if err != nil {
		t.Fatalf("CreateUserWithProfile auto: %v", err)
	}
	u, err := r.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatalf("GetUserByID %d: %v", uid, err)
	}
	if !u.PwdRemind {
		t.Fatalf("带资料建号应自带 pwdRemind=true（首登提醒改密）")
	}
	if u.Gender != "女" || u.GradeNo != 6 || u.School != "实验中学" || u.Region != "高新区" {
		t.Fatalf("资料落库不符：gender=%q gradeNo=%d school=%q region=%q", u.Gender, u.GradeNo, u.School, u.Region)
	}

	// 指定 ID 分支：同款落库，ID 精确落位
	if _, err := r.CreateUserWithProfile(ctx, 501, "stu_b", "pwd", "小B", RoleDefault, UserProfile{
		GradeNo: ParseGrade("已毕业"),
	}); err != nil {
		t.Fatalf("CreateUserWithProfile id=501: %v", err)
	}
	u, err = r.GetUserByID(ctx, 501)
	if err != nil {
		t.Fatalf("GetUserByID 501: %v", err)
	}
	if !u.PwdRemind || u.GradeNo != GradeGraduated {
		t.Fatalf("指定 ID 建号 pwdRemind=true gradeNo=13，实际 pwdRemind=%v gradeNo=%d", u.PwdRemind, u.GradeNo)
	}

	// 空资料：不落任何资料字段（读回全零值），pwdRemind 仍自带
	uid, err = r.CreateUserWithProfile(ctx, 0, "stu_c", "pwd", "小C", RoleDefault, UserProfile{})
	if err != nil {
		t.Fatalf("CreateUserWithProfile empty: %v", err)
	}
	u, err = r.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatalf("GetUserByID %d: %v", uid, err)
	}
	if !u.PwdRemind || u.Gender != "" || u.GradeNo != 0 || u.School != "" || u.Region != "" {
		t.Fatalf("空资料建号 pwdRemind=true 其余零值，实际 %+v", u)
	}
}
