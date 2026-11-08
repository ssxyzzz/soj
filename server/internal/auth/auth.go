// Package auth 提供密码哈希（bcrypt）与 JWT 签发/解析。
package auth

import (
	"sync"
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

var (
	dummyOnce sync.Once
	dummyHash string
)

// DummyCheck 用户不存在时执行一次等代价的 bcrypt 比对，抹平「用户名是否存在」的
// 响应时间差（登录时序侧信道，v0.14）；结果恒丢弃。
func DummyCheck(password string) {
	dummyOnce.Do(func() { dummyHash, _ = HashPassword("soj-dummy-固定口令垫片") })
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
}

// token 有效期与滑动续期窗口（v0.82.7）：token 固定 7 天；剩余不足 3 天时鉴权
// 中间件在响应头 X-Renewed-Token 下发新票（前端拦截器换存）——活跃会话窗口不断
// 前移永不过期，连续 3 天不活跃的 token 自然死亡，泄露面不放大。
const (
	TokenTTL    = 7 * 24 * time.Hour
	RenewWindow = 3 * 24 * time.Hour
)

// GenerateToken 签发 JWT。claims 只含身份标识（uid/uname）与密码版本（pver）：
// 角色与权限每次请求从数据库权威读取（进程内缓存），降权即时生效；pver 用于
// 改密/重置后让所有旧 token 立即失效（v0.14 全端下线）。
func GenerateToken(userID int64, username string, pwdVer int, secret string) (string, error) {
	claims := jwt.MapClaims{
		"uid":   userID,
		"uname": username,
		"pver":  pwdVer,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(TokenTTL).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken 解析并返回 uid/用户名/密码版本/过期时刻（v0.82.7 起带 exp，供中间件
// 判定滑动续期）；v0.14 之前签发的 token 无 pver，按 0 处理。
func ParseToken(token, secret string) (userID int64, username string, pwdVer int, exp time.Time, err error) {
	tok, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return 0, "", 0, time.Time{}, err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return 0, "", 0, time.Time{}, jwt.ErrTokenInvalidClaims
	}
	uid, ok := claims["uid"].(float64)
	if !ok {
		return 0, "", 0, time.Time{}, jwt.ErrTokenInvalidClaims
	}
	username, _ = claims["uname"].(string)
	if v, ok := claims["pver"].(float64); ok {
		pwdVer = int(v)
	}
	if v, ok := claims["exp"].(float64); ok {
		exp = time.Unix(int64(v), 0)
	}
	return int64(uid), username, pwdVer, exp, nil
}
