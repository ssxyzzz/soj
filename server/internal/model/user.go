package model

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func CreateUser(ctx context.Context, username, passwordHash, nickname string) (int64, error) {
	res, err := db.ExecContext(ctx,
		"INSERT INTO users (username, password_hash, nickname, role) VALUES (?, ?, ?, 'user')",
		username, passwordHash, nickname)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByUsername 返回用户与密码哈希；未找到时返回 sql.ErrNoRows。
func GetUserByUsername(ctx context.Context, username string) (*User, string, error) {
	row := db.QueryRowContext(ctx,
		"SELECT id, username, nickname, role, password_hash FROM users WHERE username = ?", username)
	var u User
	var hash string
	if err := row.Scan(&u.ID, &u.Username, &u.Nickname, &u.Role, &hash); err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := db.QueryRowContext(ctx,
		"SELECT id, username, nickname, role, created_at FROM users WHERE id = ?", id)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Nickname, &u.Role, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func GetUsersByIDs(ctx context.Context, ids []int64) (map[int64]*User, error) {
	out := make(map[int64]*User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx,
		"SELECT id, username, nickname, role, created_at FROM users WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Nickname, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out[u.ID] = &u
	}
	return out, rows.Err()
}

var ErrNotFound = sql.ErrNoRows

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
