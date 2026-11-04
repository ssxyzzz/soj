package server

// roundMySubmissions 拆形口径锁（v0.64 病根①）：已揭榜列表瘦身——行内无 code
//（揭榜等待期前端 1.5s 轮询，旧形状每拍全量代码 MB 级 JSON.parse；看代码改走
// 详情接口按需取）；隐藏期行保留 code（考时回看唯一通道——详情接口隐藏期对
// 参赛者 404）。

import (
	"context"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

// fakeRepo 补 round 两方法（本文件局部覆盖）
func (f *fakeRepo) GetRound(_ context.Context, _ string, id int64) (*repo.Contest, error) {
	for _, c := range f.rounds {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, mongo.ErrNoDocuments
}

func (f *fakeRepo) RoundSubmissions(_ context.Context, _ string, _, _ int64) ([]*repo.Submission, error) {
	return f.roundSubs, nil
}

func TestRoundMySubmissionsSplitShape(t *testing.T) {
	mk := func(published bool) (*gin.Engine, string, *fakeRepo, *Handlers) {
		f := baseFake()
		f.roles[repo.RoleDefault] = &repo.Role{ID: repo.RoleDefault}
		f.rounds = []*repo.Contest{{
			ID: 7, ClassID: 1, CreatedBy: 1,
			Published: published, HideDuring: !published,
			Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Hour),
		}}
		f.roundSubs = []*repo.Submission{{
			ID: 100, UID: 42, Username: "stu", ProblemID: 5, Language: "cpp",
			Status: "AC", Score: 100, Code: "int main(){}",
		}}
		h := newTestHandlers(f)
		gin.SetMode(gin.TestMode)
		r := gin.New()
		api := r.Group("/api", AuthRequired(h))
		api.GET("/contests/:id/submissions/mine", h.ContestMySubmissions)
		tok := f.addUser(h, 42, "stu", repo.RoleDefault, 1)
		f.isMember = true
		return r, tok, f, h
	}

	// 已揭榜：行不带 code 键
	r, tok, _, _ := mk(true)
	code, out := doJSON(r, "GET", "/api/contests/7/submissions/mine", tok, "")
	data := out["data"].(map[string]any)
	if code != 200 || data["revealed"] != true {
		t.Fatalf("揭榜请求异常: code=%d resp=%v", code, out)
	}
	row := data["list"].([]any)[0].(map[string]any)
	if _, has := row["code"]; has {
		t.Error("已揭榜列表行不得带 code（拆形核心口径）")
	}
	if row["status"] != "AC" {
		t.Errorf("揭榜行应带状态: %v", row["status"])
	}

	// 隐藏期：行带 code（考时回看唯一通道），不带状态/得分
	r, tok, _, _ = mk(false)
	code, out = doJSON(r, "GET", "/api/contests/7/submissions/mine", tok, "")
	data = out["data"].(map[string]any)
	if code != 200 || data["revealed"] != false {
		t.Fatalf("隐藏期请求异常: code=%d resp=%v", code, out)
	}
	row = data["list"].([]any)[0].(map[string]any)
	if row["code"] != "int main(){}" {
		t.Error("隐藏期列表行必须带 code（详情接口隐藏期 404，列表是唯一回看通道）")
	}
	if _, has := row["status"]; has {
		t.Error("隐藏期行不得带状态（赛期内不出分）")
	}
}
