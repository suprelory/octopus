package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecretOnce sync.Once
	jwtSecretKey  []byte
)

// getJWTSecret returns the JWT signing key, generating and persisting one if needed.
func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret, err := op.SettingGetString(model.SettingKeyJWTSecret)
		if err == nil && secret != "" {
			jwtSecretKey = []byte(secret)
			return
		}

		// Generate a random 32-byte secret
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			// Fallback to legacy method if random generation fails
			user := op.UserGet()
			jwtSecretKey = []byte(user.Username + user.Password)
			log.Warnf("failed to generate random JWT secret, using legacy method")
			return
		}
		generated := base64.RawURLEncoding.EncodeToString(b)
		if err := op.SettingSetString(model.SettingKeyJWTSecret, generated); err != nil {
			// If we can't persist, still use the generated key for this session
			log.Warnf("failed to persist JWT secret: %s", err.Error())
		}
		jwtSecretKey = []byte(generated)
	})
	return jwtSecretKey
}

// JWT 有效期约定（分钟）：
//   - expire == 0：默认短有效期
//   - expire == -1：「记住我」
//   - expire > 0：客户端指定，但会被 clamp 到 MaxJWTExpiresMin
//
// 其它取值由 handler 侧拒绝；jwtLifetime 仍然兜底到默认值，保证 ExpiresAt
// 一定非 nil（历史实现里 expire < -1 三个分支都不命中，会签出没有 exp 声明的
// token，并在随后的 claims.ExpiresAt.Format 上空指针 panic）。
const (
	defaultJWTExpiresMin    = 15
	rememberMeJWTExpiresMin = 30 * 24 * 60
	// MaxJWTExpiresMin 是客户端可请求的最长有效期（30 天）。
	MaxJWTExpiresMin = 30 * 24 * 60
)

// JWTExpireValid 判断客户端传来的 expire 是否在白名单内。正数超过上限不算
// 非法，会在 jwtLifetime 里被 clamp。
func JWTExpireValid(expiresMin int) bool {
	return expiresMin >= -1
}

// jwtLifetime 把 expire 映射成有效期，任何不认识的取值都落到默认值。
func jwtLifetime(expiresMin int) time.Duration {
	switch {
	case expiresMin > 0:
		if expiresMin > MaxJWTExpiresMin {
			expiresMin = MaxJWTExpiresMin
		}
		return time.Duration(expiresMin) * time.Minute
	case expiresMin == -1:
		return time.Duration(rememberMeJWTExpiresMin) * time.Minute
	default:
		return time.Duration(defaultJWTExpiresMin) * time.Minute
	}
}

func GenerateJWTToken(expiresMin int) (string, string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Issuer:    conf.APP_NAME,
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtLifetime(expiresMin))),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
	if err != nil {
		return "", "", err
	}
	return token, claims.ExpiresAt.Format(time.RFC3339), nil
}

func VerifyJWTToken(token string) bool {
	// 固定签名算法与签发者。golang-jwt v5 本身已拒绝 alg:none，且 keyfunc 返回
	// []byte 时 RS256 会因密钥类型不匹配而失败，这里属于纵深防御。
	jwtToken, err := jwt.Parse(token,
		func(token *jwt.Token) (interface{}, error) {
			return getJWTSecret(), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(conf.APP_NAME),
	)
	if err != nil || !jwtToken.Valid {
		return false
	}
	return true
}

func GenerateAPIKey() string {
	const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const keyLen = 48
	// 字符集长度 62 不是 2 的幂，直接对随机字节取模会让前 8 个字符出现概率偏高，
	// 所以做拒绝采样：只接受 < 248 (= 62*4) 的字节，丢弃率约 3%。
	const maxAcceptable = 256 - 256%len(keyChars)

	out := make([]byte, 0, keyLen)
	// 一次多读一些，正常情况下一轮就够；不够再补读，避免 48 次系统调用。
	buf := make([]byte, keyLen+keyLen/8+8)
	for len(out) < keyLen {
		if _, err := rand.Read(buf); err != nil {
			return ""
		}
		for _, v := range buf {
			if int(v) >= maxAcceptable {
				continue
			}
			out = append(out, keyChars[int(v)%len(keyChars)])
			if len(out) == keyLen {
				break
			}
		}
	}
	return "sk-" + conf.APP_NAME + "-" + string(out)
}
