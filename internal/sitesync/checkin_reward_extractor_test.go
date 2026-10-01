package sitesync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestExtractCheckinReward(t *testing.T) {
	for _, tc := range []struct {
		name, code, response, want string
		fail                       bool
	}{
		{"generic", `return response.data?.reward ?? null;`, `{"data":{"reward":0.125}}`, "0.125", false},
		{"quota conversion", `const quota = response.data?.quota_awarded; return quota == null ? null : Number(quota)/500000;`, `{"data":{"quota_awarded":50000}}`, "0.1", false},
		{"zero", `return response.reward;`, `{"reward":0}`, "0", false},
		{"numeric string", `return response.reward;`, `{"reward":" 0.50 "}`, "0.5", false},
		{"rounding", `return 0.1 + 0.2;`, `{}`, "0.3", false},
		{"precision", `return 0.12345678;`, `{}`, "0.123457", false},
		{"negative zero", `return -0;`, `{}`, "0", false},
		{"missing", `return response.data?.reward ?? null;`, `{}`, "", false},
		{"undefined", `return response.reward;`, `{}`, "", false},
		{"empty code", ``, `{}`, "", false},
		{"boolean", `return true;`, `{}`, "", true},
		{"object", `return {reward:1};`, `{}`, "", true},
		{"negative", `return -1;`, `{}`, "", true},
		{"nan", `return NaN;`, `{}`, "", true},
		{"infinity", `return Infinity;`, `{}`, "", true},
		{"empty string", `return " ";`, `{}`, "", true},
		{"unit string", `return "$1";`, `{}`, "", true},
		{"syntax", `return (;`, `{}`, "", true},
		{"throw", `throw new Error(response.secret);`, `{"secret":"private-response-value"}`, "", true},
		{"invalid JSON", `return 1;`, `{`, "", true},
		{"oversized code", strings.Repeat(" ", model.CheckinRewardExtractorMaxBytes+1), `{}`, "", true},
		{"oversized response", `return 1;`, `"` + strings.Repeat("a", siteCheckinResponseLimit) + `"`, "", true},
		{"null byte", "return 1;\x00", `{}`, "", true},
		{"promise", `return Promise.resolve(1);`, `{}`, "", true},
		{"no host globals", `return [typeof std, typeof os, typeof bjson, typeof print, typeof console, typeof fetch, typeof require, typeof process, typeof setTimeout, typeof QJS_PROXY_VALUE].every(v => v === "undefined") ? 1 : -1;`, `{}`, "1", false},
		{"quoted body cannot escape compilation", `}); return 1; //`, `{}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reward, err := ExtractCheckinReward(context.Background(), tc.code, []byte(tc.response))
			if (err != nil) != tc.fail || reward != tc.want {
				t.Fatalf("reward=%q, err=%v; want=%q, fail=%v", reward, err, tc.want, tc.fail)
			}
			if err != nil && strings.Contains(err.Error(), "private-response-value") {
				t.Fatal("script exception exposed response data")
			}
		})
	}
}

func TestExtractCheckinRewardResourceLimitsAndIsolation(t *testing.T) {
	for _, code := range []string{
		`while (true) {}`,
		`return new ArrayBuffer(1024*1024*1024);`,
		`function recurse() { return recurse(); } return recurse();`,
		`return import("qjs:std");`,
	} {
		start := time.Now()
		if reward, err := ExtractCheckinReward(context.Background(), code, []byte(`{}`)); err == nil || reward != "" {
			t.Fatalf("resource limit accepted %q: %q, %v", code, reward, err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatalf("execution was not bounded: %s", code)
		}
		// Every failure must release its slot and leave the next runtime usable.
		if reward, err := ExtractCheckinReward(context.Background(), `return 2;`, []byte(`{}`)); err != nil || reward != "2" {
			t.Fatalf("subsequent execution failed: %q, %v", reward, err)
		}
	}
	// Pending jobs, including imports of QuickJS's OS module, must not execute.
	if reward, err := ExtractCheckinReward(context.Background(), `import("qjs:os").then(os => os.sleep(10000)); globalThis.saved = 42; return 1;`, []byte(`{}`)); err != nil || reward != "1" {
		t.Fatalf("synchronous extraction failed: %q, %v", reward, err)
	}
	if reward, err := ExtractCheckinReward(context.Background(), `return globalThis.saved ?? null;`, []byte(`{}`)); err != nil || reward != "" {
		t.Fatalf("runtime state leaked: %q, %v", reward, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reward, err := ExtractCheckinReward(ctx, `return 1;`, []byte(`{}`)); err == nil || reward != "" {
		t.Fatalf("cancelled execution accepted: %q, %v", reward, err)
	}
}
