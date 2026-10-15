// Package storage 负责 MySQL / Redis 连接的建立与就绪等待。
// 容器编排里服务启动顺序不保证，因此这里带重试地等待依赖就绪。
package storage

import (
	"context"
	"database/sql"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/redis/go-redis/v9"
)

func MustOpenMySQL(dsn string) *sql.DB {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("解析 MySQL DSN 失败: %v", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return db
		}
		log.Printf("等待 MySQL 就绪: %v", err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("连接 MySQL 失败: %v", err)
	return nil
}

func MustOpenRedis(addr, password string, db int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err == nil {
			return rdb
		}
		log.Printf("等待 Redis 就绪: %v", err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("连接 Redis 失败")
	return nil
}
