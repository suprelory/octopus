package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestCheckinRefreshesBalanceWithoutCorruptingLastSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, checkin, self   string
		platform              model.SitePlatform
		balance, used, income float64
	}{
		{"quota total", `{"success":true,"data":{"reward":1}}`, `{"success":true,"data":{"quota":5000000,"used_quota":500000,"today_income":250000}}`, model.SitePlatformOneAPI, 9, 1, .5},
		{"quota remaining", `{"success":true}`, `{"success":true,"data":{"quota":5000000,"used_quota":500000}}`, model.SitePlatformNewAPI, 10, 1, 3},
		{"real zero", `{"success":true}`, `{"success":true,"data":{"quota":0,"used_quota":0,"today_income":0}}`, model.SitePlatformOneAPI, 0, 0, 0},
		{"already checked in", `{"success":false,"message":"already checked in today","data":{"reward":99}}`, `{"success":true,"data":{"quota":5000000,"used_quota":500000,"today_income":0}}`, model.SitePlatformOneAPI, 9, 1, 0},
		{"business failure", `{"success":true}`, `{"success":false,"data":{"quota":0,"used_quota":0}}`, model.SitePlatformOneAPI, 42, 5, 3},
		{"missing quota", `{"success":true}`, `{"success":true,"data":{"used_quota":0}}`, model.SitePlatformNewAPI, 42, 5, 3},
		{"null quota", `{"success":true}`, `{"success":true,"data":{"quota":null,"used_quota":0}}`, model.SitePlatformOneAPI, 42, 5, 3},
		{"invalid number", `{"success":true}`, `{"success":true,"data":{"quota":"NaN","used_quota":0}}`, model.SitePlatformNewAPI, 42, 5, 3},
		{"missing usage for total", `{"success":true}`, `{"success":true,"data":{"quota":500000}}`, model.SitePlatformOneAPI, 42, 5, 3},
		{"missing optional fields", `{"success":true}`, `{"success":true,"data":{"quota":"500000"}}`, model.SitePlatformNewAPI, 1, 5, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupProjectTestDB(t)
			var accountID int
			var probes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/user/checkin":
					_, _ = w.Write([]byte(tc.checkin))
				case "/api/user/self":
					probes.Add(1)
					// The network probe must happen after the outcome transaction.
					var count int64
					if err := db.GetDB().Model(&model.SiteCheckinLog{}).Where("account_id = ?", accountID).Count(&count).Error; err != nil || count != 1 {
						t.Errorf("balance fetched before the check-in committed: count=%d, err=%v", count, err)
					}
					_, _ = w.Write([]byte(tc.self))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			site, account := createCheckinFixture(t, ctx, server.URL)
			accountID = account.ID
			if err := db.GetDB().Model(site).Update("platform", tc.platform).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.GetDB().Model(account).Updates(map[string]any{"balance": 42, "balance_used": 5, "today_income": 3, "platform_user_id": 1}).Error; err != nil {
				t.Fatal(err)
			}
			result, err := CheckinAccount(ctx, account.ID)
			if err != nil || result.Status != model.SiteExecutionStatusSuccess || result.LogID == 0 || probes.Load() == 0 {
				t.Fatalf("check-in did not succeed and refresh: %+v, %v, probes=%d", result, err, probes.Load())
			}
			if result.Reason == model.SiteCheckinReasonAlreadyCheckedIn && result.Reward != "" {
				t.Fatal("already checked in counted a reward")
			}
			saved, err := op.SiteAccountGet(account.ID, ctx)
			if err != nil || saved.Balance != tc.balance || saved.BalanceUsed != tc.used || saved.TodayIncome != tc.income {
				t.Fatalf("incorrect refreshed account: %+v, %v", saved, err)
			}
		})
	}
}

func TestCheckinFailureAndUnsupportedDoNotRefresh(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		ctx := setupProjectTestDB(t)
		var probes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/user/checkin" {
				probes.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}))
		_, account := createCheckinFixture(t, ctx, server.URL)
		result, _ := CheckinAccount(ctx, account.ID)
		server.Close()
		if result == nil || probes.Load() != 0 {
			t.Fatalf("unexpected balance probe after status %d", status)
		}
	}
}

func TestSub2APIBalanceRequiresValidFieldAndHonorsCancellation(t *testing.T) {
	for _, body := range []string{`{"code":0,"data":{"balance":0}}`, `{"code":0,"data":{"balance":null}}`, `{"code":0,"data":{}}`} {
		t.Run(body, func(t *testing.T) {
			ctx := setupProjectTestDB(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			site, account := createCheckinFixture(t, ctx, server.URL)
			site.Platform = model.SitePlatformSub2API
			if err := db.GetDB().Model(account).Update("balance", 42).Error; err != nil {
				t.Fatal(err)
			}
			result := refreshAccountBalanceAfterCheckin(ctx, site, account, account.AccessToken)
			if result.ok != (body == `{"code":0,"data":{"balance":0}}`) {
				t.Fatalf("invalid balance accepted: %s, %+v", body, result)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if refreshAccountBalanceAfterCheckin(canceled, site, account, account.AccessToken).ok {
				t.Fatal("canceled refresh succeeded")
			}
		})
	}
}
