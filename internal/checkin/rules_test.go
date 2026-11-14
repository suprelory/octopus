package checkin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
)

func TestStatusRulesRequireExplicitTypedDatedEvidence(t *testing.T) {
	rule := model.CheckinStatusRule{Checked: model.CheckinMatch{Path: "data.checked", Value: json.RawMessage("true")}, Unchecked: model.CheckinMatch{Path: "data.checked", Value: json.RawMessage("false")}, DatePath: "data.date"}
	tests := []struct {
		name, body, want string
		status           int
	}{
		{"checked", `{"success":true,"data":{"checked":true,"date":"2030-11-14"}}`, "checked_in", 200},
		{"unchecked", `{"data":{"checked":false,"date":"2030-11-14"}}`, "not_checked_in", 200},
		{"string boolean", `{"data":{"checked":"false","date":"2030-11-14"}}`, "unknown", 200},
		{"missing", `{"data":{"date":"2030-11-14"}}`, "unknown", 200},
		{"failed envelope", `{"success":false,"data":{"checked":false,"date":"2030-11-14"}}`, "unknown", 200},
		{"invalid envelope", `{"success":"false","data":{"checked":false,"date":"2030-11-14"}}`, "unknown", 200},
		{"wrong date", `{"data":{"checked":true,"date":"2030-11-15"}}`, "unknown", 200},
		{"HTTP error", `{"data":{"checked":true,"date":"2030-11-14"}}`, "unknown", 500},
		{"HTML", "<html>Login required</html>", "unknown", 200},
		{"extra JSON", `{"data":{}} {"data":{}}`, "unknown", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evaluateStatus(rule, tt.status, []byte(tt.body), "2030-11-14"); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
	data, _ := decodeObject([]byte(`{"values":[{"code":1.0}],"large":1e999999999}`))
	if !matches(data, model.CheckinMatch{Path: "values.0.code", Value: json.RawMessage("1")}) {
		t.Fatal("numeric array rule failed")
	}
	if matches(data, model.CheckinMatch{Path: "large", Value: json.RawMessage("1")}) {
		t.Fatal("unbounded exponent accepted")
	}
}

func TestRewardsPreservePrecisionAndDistinguishMissingFromZero(t *testing.T) {
	rule := model.CheckinRewardRule{Path: "data.reward", Multiplier: "1"}
	tests := []struct {
		value, state string
		want         int64
	}{
		{"0", "known", 0}, {"0.0000005", "known", 1}, {`"12.345678"`, "known", 12_345_678},
		{"9007199254.740993", "known", 9_007_199_254_740_993}, {"1e-6", "known", 1},
		{"null", "unknown", 0}, {"-1", "invalid", 0}, {`"NaN"`, "invalid", 0}, {"true", "invalid", 0},
		{"9223372036854.775808", "invalid", 0}, {`"1e999999"`, "invalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			amount, state := extractReward(rule, []byte(`{"data":{"reward":`+tt.value+`}}`))
			if state != tt.state {
				t.Fatalf("state=%s want=%s", state, tt.state)
			}
			if state == "known" && (amount == nil || *amount != tt.want) {
				t.Fatalf("amount=%v want=%d", amount, tt.want)
			}
			if state != "known" && amount != nil {
				t.Fatal("unknown/invalid value converted to zero")
			}
		})
	}
	amount, err := decimalMicros("500000", "0.000002")
	if err != nil || *amount != 1_000_000 {
		t.Fatalf("explicit quota conversion failed: %v %v", amount, err)
	}
}

func TestSubmitInterpretationAndRedirects(t *testing.T) {
	config := model.CheckinSiteConfig{}
	for _, tt := range []struct {
		body   string
		code   int
		status string
	}{
		{`{"success":true}`, 200, "success"},
		{`{"success":"true"}`, 200, "manual_required"},
		{`{"success":false,"message":"今日已签到"}`, 200, "already_checked_in"},
		{`{"success":true,"message":"already checked in"}`, 500, "manual_required"},
		{"<html>not found</html>", 404, "failed"},
		{"<html>Cloudflare</html>", 200, "manual_required"},
	} {
		if got := interpretSubmit(config, tt.code, []byte(tt.body)); got.Status != tt.status {
			t.Fatalf("%s (%d): %+v", tt.body, tt.code, got)
		}
	}
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, err := newHTTPClient("", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response := requestHTTP(context.Background(), client, model.CheckinSiteConfig{BaseURL: redirect.URL}, model.CheckinRequest{Method: "GET", Path: "/submit"}, model.CheckinCredentials{Type: "cookie", Cookie: "secret"}, "2030-11-14")
	if response.StatusCode != 307 || targetCalls != 0 {
		t.Fatal("redirect forwarded credential or repeated submission")
	}
}

func TestStatisticsSeparateUnitsAndRetainDeletedHistory(t *testing.T) {
	h := newHarness(t)
	siteA, accountA := h.account("https://example.invalid", false, false)
	siteB, accountB := h.account("https://example.invalid", false, false)
	insert := func(site model.CheckinSite, account model.CheckinAccount, date, unit, status string, micros *int64, state string) {
		t.Helper()
		r := model.CheckinRun{ID: uuid.NewString(), SiteID: site.ID, AccountID: account.ID, SiteName: site.Config.Name, AccountName: account.Name, CheckinDate: date, Timezone: "UTC", Status: status, RewardMicros: micros, RewardState: state, RewardUnit: unit}
		if err := h.s.Store.DB.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	one, two, three := int64(1_000_000), int64(2_000_000), int64(3_000_000)
	insert(siteA, accountA, "2030-11-14", "USD", "success", &one, "known")
	insert(siteB, accountB, "2030-11-14", "USD", "success", &two, "known")
	insert(siteA, accountA, "2030-11-13", "points", "success", &three, "known")
	insert(siteB, accountB, "2030-11-13", "points", "success", &two, "known")
	insert(siteA, accountA, "2030-10-01", "CNY", "success", &one, "known")
	insert(siteA, accountA, "2030-11-12", "USD", "already_checked_in", nil, "unknown")
	insert(siteB, accountB, "2030-11-12", "USD", "success", nil, "invalid")
	if err := h.s.Store.Delete(context.Background(), siteA.ID, ""); err != nil {
		t.Fatal(err)
	}
	stats, err := h.s.Stats(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Rewards) != 4 || stats.Success != 6 || stats.Already != 1 || stats.InvalidReward != 1 || stats.TodayCompleted != 2 {
		t.Fatalf("bad grouping: %+v", stats)
	}
	for _, r := range stats.Rewards {
		if r.Unit == "USD" && (r.Today != "3" || r.Total != "3") {
			t.Fatal("currency aggregate incorrect")
		}
		if r.Unit == "CNY" && (r.Today != "0" || r.Days30 != "0" || r.Total != "1") {
			t.Fatal("date window incorrect")
		}
	}
	filtered, err := h.s.Stats(context.Background(), siteA.ID, accountA.ID)
	if err != nil || filtered.Total != 4 {
		t.Fatal("deleted history no longer available")
	}
}
