package main

// 场景编排（考试形态）：登录风暴 → 预热浏览 → 赛中提交+榜单轮询（重叠）→ 自测洪峰。

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// terminalStatus 判题终态集合（repo/submission.go 状态目录）。
var terminalStatus = map[string]bool{
	"Accepted": true, "Wrong Answer": true, "Compile Error": true,
	"Time Limit Exceeded": true, "Memory Limit Exceeded": true,
	"Runtime Error": true, "System Error": true, "Cancelled": true,
}

// loginOne 单用户登录（组织者造数用，不记指标）。
func loginOne(hc *http.Client, base string, u *user) error {
	r, _, err := callJSON(hc, http.MethodPost, base, "/api/auth/login", "",
		map[string]string{"username": u.uname, "password": u.pass})
	if err != nil || r.Code != 0 {
		return fmt.Errorf("%s: %v %s", u.uname, err, r.Message)
	}
	var d struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(r.Data, &d) != nil || d.Token == "" {
		return fmt.Errorf("%s: 响应缺 token", u.uname)
	}
	u.token = d.Token
	return nil
}

// loginAll 登录风暴：全体同时登录（barrier 齐发）——每次成功登录全量失效身份缓存
// （auth.go idents.flush），是「开考整点卡」画像的触发形态。
func loginAll(hc *http.Client, base string, us []user, m *metrics) error {
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := 1; i < len(us); i++ {
		wg.Add(1)
		go func(u *user) {
			defer wg.Done()
			<-start
			r, ms, err := callJSON(hc, http.MethodPost, base, "/api/auth/login", "",
				map[string]string{"username": u.uname, "password": u.pass})
			m.observe("POST /api/auth/login", r, ms, err)
			if err != nil || r.Code != 0 {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s 登录失败: %v %s", u.uname, err, r.Message)
				}
				mu.Unlock()
				return
			}
			var d struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(r.Data, &d) == nil && d.Token != "" {
				u.token = d.Token
			}
		}(&us[i])
	}
	close(start)
	wg.Wait()
	return firstErr
}

// runBrowse 预热浏览：登录用户高频只读面（me / 首页统计 / 题库列表 / 比赛详情）。
// 两拍口径（v0.66 批④）：第一拍紧跟登录（ident/personal 缓存全 miss），隔 1s 第二拍
// 应全命中——两拍分列即缓存效应的实测证据（A5 精准失效 / A11 个性化缓存的收益面）。
func runBrowse(hc *http.Client, base string, us []user, cid int64, m *metrics) {
	eps := []struct{ name, path string }{
		{"GET /api/me", "/api/me"},
		{"GET /api/site/stats", "/api/site/stats"},
		{"GET /api/problems", "/api/problems?page=1&page_size=20"},
		{"GET /api/contests/:id", fmt.Sprintf("/api/contests/%d", cid)},
	}
	for pass := 1; pass <= 2; pass++ {
		suffix := ""
		if pass == 2 {
			suffix = "（二拍·缓存命中）"
			time.Sleep(time.Second)
		}
		var wg sync.WaitGroup
		for i := 1; i < len(us); i++ {
			wg.Add(1)
			go func(u *user) {
				defer wg.Done()
				for _, ep := range eps {
					r, ms, err := callJSON(hc, http.MethodGet, base, ep.path, u.token, nil)
					m.observe(ep.name+suffix, r, ms, err)
				}
			}(&us[i])
		}
		wg.Wait()
	}
}

type subOutcome struct {
	uname    string
	id       int64
	final    string
	judgeSec float64
	submitMS float64
	polls    int
	errText  string
}

type boardPoint struct {
	offSec float64
	ms     float64
}

// runExam 赛中核心负载：榜单轮询（每用户 3s 一拍）与提交+状态轮询（1.5s 一拍）
// 重叠运行——比赛进行中「边判题边刷榜」的真实形态；全员到终态后收榜单。
func runExam(hc *http.Client, base string, us []user, cid, pid int64, m *metrics) ([]subOutcome, []boardPoint) {
	examStart := time.Now()
	boardStop := make(chan struct{})
	var boardWG sync.WaitGroup
	var boardMu sync.Mutex
	var board []boardPoint

	for i := 1; i < len(us); i++ {
		boardWG.Add(1)
		go func(u *user) {
			defer boardWG.Done()
			for {
				r, ms, err := callJSON(hc, http.MethodGet, base,
					fmt.Sprintf("/api/contests/%d/leaderboard", cid), u.token, nil)
				m.observe("GET /api/contests/:id/leaderboard", r, ms, err)
				boardMu.Lock()
				board = append(board, boardPoint{offSec: time.Since(examStart).Seconds(), ms: ms})
				boardMu.Unlock()
				select {
				case <-boardStop:
					return
				case <-time.After(3 * time.Second):
				}
			}
		}(&us[i])
	}

	outcomes := make([]subOutcome, len(us))
	var wg sync.WaitGroup
	for i := 1; i < len(us); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u := &us[i]
			o := &outcomes[i]
			o.uname = u.uname
			time.Sleep(time.Duration(rand.IntN(2000)) * time.Millisecond) // 开考错峰 0-2s
			if i%10 == 9 {                                                // 每 10 人一个 WA 首交：榜单 tries/errors + 重交负载
				r, ms, err := callJSON(hc, http.MethodPost, base,
					fmt.Sprintf("/api/contests/%d/problems/%d/submissions", cid, pid), u.token,
					map[string]string{"language": "cpp", "code": waCode})
				m.observe("POST contest submission", r, ms, err)
			}
			started := time.Now()
			r, ms, err := callJSON(hc, http.MethodPost, base,
				fmt.Sprintf("/api/contests/%d/problems/%d/submissions", cid, pid), u.token,
				map[string]string{"language": "cpp", "code": acCode})
			m.observe("POST contest submission", r, ms, err)
			o.submitMS = ms
			if err != nil || r.Code != 0 {
				o.errText = fmt.Sprintf("提交失败: %v %s", err, r.Message)
				return
			}
			id, ok := parseID(r.Data)
			if !ok {
				o.errText = "响应缺 id"
				return
			}
			o.id = id
			deadline := time.Now().Add(240 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(1500 * time.Millisecond)
				pr, pms, perr := callJSON(hc, http.MethodGet, base,
					fmt.Sprintf("/api/submissions/%d", id), u.token, nil)
				m.observe("GET /api/submissions/:id", pr, pms, perr)
				o.polls++
				if perr == nil && pr.Code == 0 {
					var d struct {
						Status string `json:"status"`
					}
					if json.Unmarshal(pr.Data, &d) == nil && terminalStatus[d.Status] {
						o.final = d.Status
						o.judgeSec = time.Since(started).Seconds()
						return
					}
				}
			}
			o.errText = "240s 未到终态"
		}(i)
	}
	wg.Wait()
	close(boardStop)
	boardWG.Wait()
	return outcomes, board
}

// runSelftest 自测洪峰：10 用户同时各 2 次——全站自测并发闸=2、编译闸=1，
// 排队延迟即批②隔离项要治的基线数据。
func runSelftest(hc *http.Client, base string, us []user, pid int64, m *metrics) {
	var wg sync.WaitGroup
	for i := 1; i < len(us) && i <= 10; i++ {
		wg.Add(1)
		go func(u *user) {
			defer wg.Done()
			for k := 0; k < 2; k++ {
				r, ms, err := callJSON(hc, http.MethodPost, base,
					fmt.Sprintf("/api/problems/%d/run", pid), u.token,
					map[string]string{"language": "cpp", "code": acCode, "input": "3 4"})
				m.observe("POST /api/problems/:id/run", r, ms, err)
			}
		}(&us[i])
	}
	wg.Wait()
}
