package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *CheckinStore) BeginInspection(ctx context.Context, accountID, kind string, now time.Time) (model.CheckinAccount, model.CheckinSite, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var account model.CheckinAccount
	var site model.CheckinSite
	if kind != "balance" && kind != "detect" {
		return account, site, model.InvalidCheckin("inspection kind must be balance or detect")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, "id = ? AND deleted_at IS NULL", accountID).Error; err != nil {
			return err
		}
		if err := tx.First(&site, "id = ? AND deleted_at IS NULL", account.SiteID).Error; err != nil {
			return err
		}
		if !account.Enabled || !site.Config.Enabled {
			return model.InvalidCheckin("site or account is disabled")
		}
		location, err := time.LoadLocation(site.Config.Timezone)
		if err != nil {
			return err
		}
		owner, until := uuid.NewString(), now.Add(CheckinLeaseDuration)
		account.LeaseOwner, account.LeaseUntil = owner, &until
		account.Inspection = &model.CheckinInspection{
			ID: owner, Kind: kind, Status: "running", Authentication: "unknown",
			CheckinSupport: "not_checked", CheckinStatus: "unknown",
			CheckinDate: now.In(location).Format("2006-01-02"), Timezone: site.Config.Timezone,
			StartedAt: now, Steps: []model.CheckinInspectionStep{},
		}
		result := tx.Model(&model.CheckinAccount{}).
			Where("id = ? AND (lease_until IS NULL OR lease_until <= ?)", account.ID, now).
			Select("LeaseOwner", "LeaseUntil", "Inspection").Updates(&account)
		return checkSessionWrite(result)
	})
	return account, site, err
}

// The same lease and credential-version checks protect token rotation and
// observations. Completing an inspection changes no check-in/scheduling fields.
func (s *CheckinStore) SaveInspection(ctx context.Context, item model.CheckinJobItem, account model.CheckinAccount, inspection *model.CheckinInspection, balance *model.CheckinBalanceSnapshot, finished bool, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row := model.CheckinAccount{Inspection: inspection, Balance: balance}
	columns := []string{"Inspection"}
	if !finished {
		progress := *inspection
		progress.Status, progress.Reason, progress.FinishedAt, progress.BalanceUpdated = "running", "", nil, false
		row.Inspection = &progress
	}
	if balance != nil {
		columns = append(columns, "Balance")
	}
	if finished {
		columns = append(columns, "LeaseOwner", "LeaseUntil")
	}
	return checkSessionWrite(sessionAccount(s.DB.WithContext(ctx), item, account, now).Select(columns).Updates(&row))
}

func recoverCheckinInspections(tx *gorm.DB, now time.Time) error {
	var accounts []model.CheckinAccount
	if err := tx.Where("lease_until <= ? AND inspection IS NOT NULL", now).Find(&accounts).Error; err != nil {
		return err
	}
	for _, account := range accounts {
		if account.Inspection == nil || account.Inspection.Status != "running" {
			continue
		}
		interruptCheckinInspection(&account, now)
		account.LeaseOwner, account.LeaseUntil = "", nil
		if err := tx.Model(&model.CheckinAccount{}).Where("id = ? AND lease_until <= ?", account.ID, now).
			Select("Inspection", "LeaseOwner", "LeaseUntil").Updates(&account).Error; err != nil {
			return err
		}
	}
	return nil
}

func interruptCheckinInspection(account *model.CheckinAccount, now time.Time) {
	if account.Inspection != nil && account.Inspection.Status == "running" {
		inspection := *account.Inspection
		if now.Before(inspection.StartedAt) {
			now = inspection.StartedAt
		}
		inspection.Status, inspection.Reason = "interrupted", "inspection_interrupted"
		inspection.FinishedAt, inspection.BalanceUpdated = &now, false
		account.Inspection = &inspection
	}
}
