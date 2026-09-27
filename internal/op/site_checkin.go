package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

type SiteCheckinLogFilter struct {
	SiteID    int
	AccountID int
	Status    model.SiteExecutionStatus
	Source    string
	BeforeID  int64
	Limit     int
	From      *time.Time
	Until     *time.Time
}

func SiteCheckinLogList(ctx context.Context, filter SiteCheckinLogFilter) (*model.SiteCheckinLogPage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := db.GetDB().WithContext(ctx).Model(&model.SiteCheckinLog{})
	if filter.SiteID > 0 {
		query = query.Where("site_id = ?", filter.SiteID)
	}
	if filter.AccountID > 0 {
		query = query.Where("account_id = ?", filter.AccountID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.BeforeID > 0 {
		query = query.Where("id < ?", filter.BeforeID)
	}
	if filter.From != nil {
		query = query.Where("finished_at >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("finished_at < ?", filter.Until.UTC())
	}
	page := &model.SiteCheckinLogPage{Items: make([]model.SiteCheckinLog, 0, limit+1)}
	if err := query.Order("id DESC").Limit(limit + 1).Find(&page.Items).Error; err != nil {
		return nil, fmt.Errorf("list site checkin logs: %w", err)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextBeforeID = page.Items[limit-1].ID
	}
	return page, nil
}
