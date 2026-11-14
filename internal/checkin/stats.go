package checkin

import (
	"context"
	"math/big"
	"sort"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type RewardTotal struct {
	Key      string `json:"key"`
	Unit     string `json:"unit"`
	SiteName string `json:"site_name,omitempty"`
	Today    string `json:"today"`
	Days7    string `json:"days_7"`
	Days30   string `json:"days_30"`
	Total    string `json:"total"`
}

type Stats struct {
	Total          int           `json:"total"`
	Success        int           `json:"success"`
	Already        int           `json:"already"`
	Failed         int           `json:"failed"`
	Pending        int           `json:"pending"`
	TodayCompleted int           `json:"today_completed"`
	UnknownReward  int           `json:"unknown_reward"`
	InvalidReward  int           `json:"invalid_reward"`
	Rewards        []RewardTotal `json:"rewards"`
}

func (s *Service) Stats(ctx context.Context, siteID, accountID string) (Stats, error) {
	result := Stats{Rewards: []RewardTotal{}}
	type sums struct {
		view   RewardTotal
		values [4]big.Int
	}
	groups := map[string]*sums{}
	q := s.Store.DB.WithContext(ctx).Model(&model.CheckinRun{})
	if siteID != "" {
		q = q.Where("site_id = ?", siteID)
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	var batch []model.CheckinRun
	err := q.FindInBatches(&batch, 500, func(_ *gorm.DB, _ int) error {
		for _, run := range batch {
			result.Total++
			loc, err := time.LoadLocation(run.Timezone)
			if err != nil {
				loc = time.UTC
			}
			now := s.Now().In(loc)
			today := now.Format("2006-01-02")
			switch run.Status {
			case "success":
				result.Success++
			case "already_checked_in":
				result.Already++
			case "failed":
				result.Failed++
			case "pending", "manual_required", "running":
				result.Pending++
			}
			if run.CheckinDate == today && (run.Status == "success" || run.Status == "already_checked_in") {
				result.TodayCompleted++
			}
			if run.Status != "success" {
				continue
			}
			if run.RewardMicros == nil {
				if run.RewardState == "invalid" {
					result.InvalidReward++
				} else {
					result.UnknownReward++
				}
				continue
			}
			key, name := run.SiteID+":"+run.RewardUnit, run.SiteName
			switch run.RewardUnit {
			case "USD", "CNY", "EUR", "GBP", "JPY", "HKD":
				key, name = run.RewardUnit, ""
			}
			group := groups[key]
			if group == nil {
				group = &sums{view: RewardTotal{Key: key, Unit: run.RewardUnit, SiteName: name}}
				groups[key] = group
			}
			amount := big.NewInt(*run.RewardMicros)
			group.values[3].Add(&group.values[3], amount)
			if run.CheckinDate == today {
				group.values[0].Add(&group.values[0], amount)
			}
			if run.CheckinDate >= now.AddDate(0, 0, -6).Format("2006-01-02") && run.CheckinDate <= today {
				group.values[1].Add(&group.values[1], amount)
			}
			if run.CheckinDate >= now.AddDate(0, 0, -29).Format("2006-01-02") && run.CheckinDate <= today {
				group.values[2].Add(&group.values[2], amount)
			}
		}
		return nil
	}).Error
	for _, group := range groups {
		group.view.Today = formatMicros(&group.values[0])
		group.view.Days7 = formatMicros(&group.values[1])
		group.view.Days30 = formatMicros(&group.values[2])
		group.view.Total = formatMicros(&group.values[3])
		result.Rewards = append(result.Rewards, group.view)
	}
	sort.Slice(result.Rewards, func(i, j int) bool { return result.Rewards[i].Key < result.Rewards[j].Key })
	return result, err
}
