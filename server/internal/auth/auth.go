// Package auth 提供密码哈希（bcrypt）与 JWT 签发/解析。
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken 签发 JWT。claims 只含身份标识（uid/uname）：
// 角色与权限每次请求从数据库权威读取（进程内缓存），降权即时生效。
func GenerateToken(userID int64, username, secret string) (string, error) {
	claims := jwt.MapClaims{
		"uid":   userID,
		"uname": username,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func ParseToken(token, secret string) (userID int64, username string, err error) {
	tok, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return 0, "", err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return 0, "", jwt.ErrTokenInvalidClaims
	}
	uid, ok := claims["uid"].(float64)
	if !ok {
		return 0, "", jwt.ErrTokenInvalidClaims
	}
	username, _ = claims["uname"].(string)
	return int64(uid), username, nil
}
