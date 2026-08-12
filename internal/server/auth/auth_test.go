package auth

import (
	"testing"
	"time"
)

func TestJWTExpireValid(t *testing.T) {
	cases := []struct {
		expire int
		want   bool
	}{
		{0, true},       // 默认
		{-1, true},      // 记住我
		{60, true},      // 1 小时
		{1 << 20, true}, // 超大正数：合法，会被 clamp
		// expire < -1 以前三个分支都不命中，会签出没有 exp 声明的 token，
		// 随后在 claims.ExpiresAt.Format 上空指针 panic。
		{-2, false},
		{-100, false},
	}

	for _, tc := range cases {
		if got := JWTExpireValid(tc.expire); got != tc.want {
			t.Errorf("JWTExpireValid(%d) = %v, want %v", tc.expire, got, tc.want)
		}
	}
}

func TestJWTLifetime(t *testing.T) {
	cases := []struct {
		name   string
		expire int
		want   time.Duration
	}{
		{"default", 0, defaultJWTExpiresMin * time.Minute},
		{"remember me", -1, rememberMeJWTExpiresMin * time.Minute},
		{"explicit", 60, 60 * time.Minute},
		{"clamped", MaxJWTExpiresMin * 10, MaxJWTExpiresMin * time.Minute},
		{"at limit", MaxJWTExpiresMin, MaxJWTExpiresMin * time.Minute},
		// 非法值也必须有确定的有效期，不能落到 ExpiresAt == nil。
		{"below range", -2, defaultJWTExpiresMin * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jwtLifetime(tc.expire); got != tc.want {
				t.Fatalf("jwtLifetime(%d) = %v, want %v", tc.expire, got, tc.want)
			}
		})
	}
}

func TestGenerateAPIKeyShape(t *testing.T) {
	const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

	seen := make(map[string]struct{}, 64)
	charCounts := make(map[byte]int)

	for i := 0; i < 64; i++ {
		key := GenerateAPIKey()
		if key == "" {
			t.Fatal("GenerateAPIKey returned an empty string")
		}
		if _, dup := seen[key]; dup {
			t.Fatalf("GenerateAPIKey returned a duplicate key: %q", key)
		}
		seen[key] = struct{}{}

		const prefix = "sk-octopus-"
		if len(key) <= len(prefix) {
			t.Fatalf("key %q is shorter than its prefix", key)
		}
		suffix := key[len(key)-48:]
		if len(suffix) != 48 {
			t.Fatalf("key %q suffix length = %d, want 48", key, len(suffix))
		}
		for j := 0; j < len(suffix); j++ {
			ch := suffix[j]
			if !containsByte(keyChars, ch) {
				t.Fatalf("key %q contains out-of-alphabet byte %q", key, ch)
			}
			charCounts[ch]++
		}
	}

	// 拒绝采样的意义在于覆盖整个字符集而不是偏向前 8 个字符。
	// 64 个 key * 48 字符 = 3072 次抽样，覆盖率远超一半才算正常。
	if len(charCounts) < len(keyChars)/2 {
		t.Errorf("only %d of %d alphabet characters were produced, sampling looks skewed",
			len(charCounts), len(keyChars))
	}
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}
