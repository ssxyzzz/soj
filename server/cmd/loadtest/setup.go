package main

// 造数与清理：用户走 repo 直连（bcrypt/文档结构与业务同源），题目与比赛走 HTTP
// （网页建题/建赛同路径——测试数据落在与应用一致的层）。清理无 repo 删除函数
// 可用（业务无删用户/删题），直接按 uid/_id 范围删文档。
// 压测用户口令从环境变量 SOJ_LDT_PASSWORD 注入（不进源码；跨次复用同一值保证幂等）。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"soj/internal/repo"
)

const (
	// 全员 admin 角色：榜单权限位（contest.score.view）+ 自测等级闸教职工豁免 +
	// 提交限速豁免——压测聚焦链路负载，不被学生侧权限模型挡住（口径在报告注明）
	ldtRole = repo.RoleAdmin

	acCode = `#include <iostream>
int main(){long long a,b;std::cin>>a>>b;std::cout<<a+b<<"\n";}`
	waCode = `#include <iostream>
int main(){long long a,b;std::cin>>a>>b;std::cout<<a*b<<"\n";}`
)

func unameAt(prefix string, i int) string { return fmt.Sprintf("%s_u%03d", prefix, i) }

// seedUsers 幂等种子：uidBase=组织者（root——存量库的 admin 角色可能缺 contest.bank.create
// 等后期权限位、无补发迁移；root 权限判定恒过不查库），uidBase+1..N=选手（admin——
// score.view 有补发迁移、自测等级闸教职工豁免、提交限速豁免）。唯一键冲突视为已存在
// （复用），并顺带校准角色防上轮残留口径不一致。
func seedUsers(ctx context.Context, r *repo.Repo, mc *mongo.Client, dbName, prefix string, uidBase int64, n int, pass string) error {
	users := mc.Database(dbName).Collection("users")
	for i := 0; i <= n; i++ {
		role := ldtRole
		if i == 0 {
			role = repo.RoleRoot
		}
		uid := uidBase + int64(i)
		if _, err := r.CreateUserWithID(ctx, uid, unameAt(prefix, i), pass,
			fmt.Sprintf("压测%03d", i), role); err != nil && !mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("种子用户 %s 失败: %w", unameAt(prefix, i), err)
		}
		if _, err := users.UpdateOne(ctx, bson.M{"_id": uid}, bson.M{"$set": bson.M{"role": role}}); err != nil {
			return fmt.Errorf("校准 %s 角色失败: %w", unameAt(prefix, i), err)
		}
	}
	return nil
}

// buildTestdataZip 内存构造建题数据包：根下 data/ 目录（config.yaml + 两组数据对）。
func buildTestdataZip() []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"data/config.yaml": "time: 1s\nmemory: 256MB\n",
		"data/1.in":        "1 2\n",
		"data/1.out":       "3\n",
		"data/2.in":        "222 333\n",
		"data/2.out":       "555\n",
	}
	for name, content := range files {
		if w, err := zw.Create(name); err == nil {
			_, _ = w.Write([]byte(content))
		}
	}
	_ = zw.Close()
	return buf.Bytes()
}

// createProblem 走网页建题接口（POST /api/problems，multipart：元数据 + 数据 zip）。
func createProblem(hc *http.Client, base, token, title string) (int64, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range map[string]string{
		"title":           title,
		"type":            "default",
		"statement":       "## A+B\n\n输入两个整数 $a, b$，输出它们的和。\n",
		"time_limit_ms":   "1000",
		"memory_limit_kb": "262144",
	} {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("zip", "data.zip")
	_, _ = fw.Write(buildTestdataZip())
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, base+"/api/problems", &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	var wrap struct {
		Code int `json:"code"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil || wrap.Code != 0 || wrap.Data.ID == 0 {
		return 0, fmt.Errorf("响应异常: %s", raw)
	}
	return wrap.Data.ID, nil
}

// createContest 走建赛接口：start 提前 2 分钟（创建即 running）、hide_during=false
// （赛期内提交状态对本人可见，GET /api/submissions/:id 可轮询）、scoreboard=true。
func createContest(hc *http.Client, base, token string, pid int64) (int64, error) {
	now := time.Now()
	r, _, err := callJSON(hc, http.MethodPost, base, "/api/contests", token, map[string]any{
		"title":       "LDT 压测赛 " + now.Format("0102-150405"),
		"start":       now.Add(-2 * time.Minute).Format(time.RFC3339),
		"end":         now.Add(45 * time.Minute).Format(time.RFC3339),
		"hidden_days": 0,
		"problem_ids": []int64{pid},
		"scoreboard":  true,
		"hide_during": false,
		"hide_meta":   false,
	})
	if err != nil {
		return 0, err
	}
	if r.Code != 0 {
		return 0, fmt.Errorf("HTTP %d code=%d: %s", r.Status, r.Code, r.Message)
	}
	id, ok := parseID(r.Data)
	if !ok {
		return 0, fmt.Errorf("响应缺 id: %s", r.Data)
	}
	return id, nil
}

// cleanupAll 直接删压测文档：用户/提交按 uid 段，比赛/题目按 _id。
func cleanupAll(ctx context.Context, cli *mongo.Client, dbName string, uidBase int64, n int, cid, pid int64) error {
	db := cli.Database(dbName)
	uids := bson.M{"$gte": uidBase, "$lte": uidBase + int64(n)}
	for _, del := range []struct {
		coll string
		q    bson.M
	}{
		{"users", bson.M{"_id": uids}},
		{"submissions", bson.M{"uid": uids}},
		{"contests", bson.M{"_id": cid}},
		{"problems", bson.M{"_id": pid}},
	} {
		if cid <= 0 && del.coll == "contests" {
			continue
		}
		if pid <= 0 && del.coll == "problems" {
			continue
		}
		if _, err := db.Collection(del.coll).DeleteMany(ctx, del.q); err != nil {
			return fmt.Errorf("清理 %s 失败: %w", del.coll, err)
		}
	}
	return nil
}
