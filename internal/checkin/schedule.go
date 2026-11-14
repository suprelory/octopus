package checkin

import (
	"math/rand/v2"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func location(site model.CheckinSiteConfig) *time.Location {
	loc, err := time.LoadLocation(site.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func businessDate(site model.CheckinSiteConfig, now time.Time) string {
	return now.In(location(site)).Format("2006-01-02")
}

func window(site model.CheckinSiteConfig, now time.Time) (time.Time, time.Time) {
	loc := location(site)
	local := now.In(loc)
	start, _ := model.CheckinClock(site.WindowStart)
	end, _ := model.CheckinClock(site.WindowEnd)
	from := time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
	until := time.Date(local.Year(), local.Month(), local.Day(), end/60, end%60, 0, 0, loc)
	return from, until
}

func inWindow(site model.CheckinSiteConfig, now time.Time) bool {
	start, end := window(site, now)
	return !now.Before(start) && now.Before(end)
}

func nextSchedule(site model.CheckinSiteConfig, account model.CheckinAccount, now time.Time, random func(int) int) time.Time {
	if random == nil {
		random = rand.IntN
	}
	local := now.In(location(site))
	start, end := window(site, local)
	if account.LastSuccessDate >= businessDate(site, now) || !local.Before(end) {
		start, end = window(site, local.AddDate(0, 0, 1))
	} else if local.After(start) {
		start = local
	}
	available := int(end.Sub(start) / time.Minute)
	delay := min(site.JitterMinutes, max(0, available-1))
	if delay > 0 {
		start = start.Add(time.Duration(random(delay+1)) * time.Minute)
	}
	return start.UTC()
}
