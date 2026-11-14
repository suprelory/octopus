package checkin

import (
	"context"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestScheduleUsesSiteDateAndDSTCalendarDays(t *testing.T) {
	site := model.CheckinSiteConfig{Timezone: "Asia/Shanghai", WindowStart: "08:00", WindowEnd: "09:00", JitterMinutes: 120}
	now := time.Date(2030, 11, 13, 23, 59, 0, 0, time.UTC)
	if businessDate(site, now) != "2030-11-14" || inWindow(site, now) {
		t.Fatal("incorrect business date/window")
	}
	next := nextSchedule(site, model.CheckinAccount{}, now, func(n int) int { return n - 1 })
	if next.Format(time.RFC3339) != "2030-11-14T00:59:00Z" {
		t.Fatalf("jitter escaped window: %s", next)
	}
	site.Timezone, site.WindowStart, site.WindowEnd, site.JitterMinutes = "America/New_York", "00:00", "24:00", 0
	loc := location(site)
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	next = nextSchedule(site, model.CheckinAccount{LastSuccessDate: "2026-11-01"}, now, nil)
	if next.In(loc).Format("2006-01-02 15:04") != "2026-11-02 00:00" || next.Sub(now) != 25*time.Hour {
		t.Fatalf("DST schedule used fixed 24h interval: %v", next)
	}
}

func TestScheduleTargetSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	site, account := h.account("https://example.invalid", false, true)
	site.Config.WindowStart, site.Config.WindowEnd, site.Config.JitterMinutes = "13:00", "14:00", 30
	if _, err := h.s.Store.SaveSite(context.Background(), site.ID, site.Revision, site.Config); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := h.s.Store.Account(context.Background(), account.ID)
	if err != nil || before.NextRunAt == nil {
		t.Fatal("target not persisted", err)
	}
	restarted := New(h.s.Store, h.s.Vault)
	defer restarted.Close()
	restarted.Now = func() time.Time { return h.now.Add(time.Minute) }
	if err := restarted.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := h.s.Store.Account(context.Background(), account.ID)
	if after.NextRunAt == nil || !after.NextRunAt.Equal(*before.NextRunAt) {
		t.Fatal("restart rerandomized schedule")
	}
}
