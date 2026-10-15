// Package model 存放数据表结构与数据库访问函数（database/sql 裸 SQL，不引入 ORM）。
package model

import (
	"context"
	"database/sql"
	"time"
)

// 判题状态（submissions.status 列存储这些字符串）
const (
	StatusPending      = "Pending"
	StatusJudging      = "Judging"
	StatusAccepted     = "Accepted"
	StatusWrongAnswer  = "Wrong Answer"
	StatusCompileError = "Compile Error"
	StatusTLE          = "Time Limit Exceeded"
	StatusMLE          = "Memory Limit Exceeded"
	StatusRE           = "Runtime Error"
	StatusSystemError  = "System Error"
)

// Redis 键
const (
	RankKey = "soj:rank:ac" // 排行榜 zset，member=用户ID，score=解题数
)

type User struct {
	ID        int64
	Username  string
	Nickname  string
	Role      string
	CreatedAt time.Time
}

type Problem struct {
	ID            int64
	Title         string
	Description   string
	TimeLimitMS   int
	MemoryLimitKB int
	CreatedAt     time.Time
}

type TestCase struct {
	ID        int64
	ProblemID int64
	Input     string
	Output    string
	IsSample  bool
}

type Submission struct {
	ID           int64
	UserID       int64
	Username     string // JOIN users
	ProblemID    int64
	ProblemTitle string // JOIN problems
	Language     string
	Code         string
	Status       string
	TimeMS       int
	MemoryKB     int
	Score        int
	ErrorInfo    string
	CreatedAt    time.Time
}

var db *sql.DB

func SetDB(d *sql.DB) { db = d }

func Ping(ctx context.Context) error { return db.PingContext(ctx) }
