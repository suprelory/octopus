package op

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CheckinJobFilter struct {
	Status, Source, From, Until, Before string
	Limit                               int
}

type CheckinJobPage struct {
	Items      []model.CheckinJob `json:"items"`
	NextBefore string             `json:"next_before"`
}

// SQLite stores timestamps as text with their original offset. Comparing that
// text against UTC boundaries would misorder legacy/imported records. Its date
// function normalizes offsets; ID remains the tie-breaker within a millisecond.
func checkinJobTimeSQL(conn *gorm.DB, column string) (string, string) {
	if conn.Dialector.Name() == "sqlite" {
		return "julianday(" + column + ")", "julianday(?)"
	}
	return column, "?"
}

func (s *CheckinStore) JobPage(ctx context.Context, f CheckinJobFilter) (CheckinJobPage, error) {
	page := CheckinJobPage{Items: []model.CheckinJob{}}
	if f.Limit < 1 || f.Limit > 100 {
		return page, model.InvalidCheckin("limit must be between 1 and 100")
	}
	if f.Status != "" && !slices.Contains([]string{"queued", "running", "completed", "completed_with_errors", "canceled", "interrupted"}, f.Status) {
		return page, model.InvalidCheckin("invalid job status")
	}
	if f.Source != "" && !slices.Contains([]string{"manual", "scheduled", "confirmation", "confirmation_auto"}, f.Source) {
		return page, model.InvalidCheckin("invalid job source")
	}
	q := s.DB.WithContext(ctx)
	created, timeArg := checkinJobTimeSQL(s.DB, "created_at")
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	for _, date := range []string{f.From, f.Until} {
		if date != "" {
			if _, err := time.Parse(time.DateOnly, date); err != nil {
				return page, model.InvalidCheckin("dates must use YYYY-MM-DD")
			}
		}
	}
	if f.From != "" && f.Until != "" && f.From > f.Until {
		return page, model.InvalidCheckin("start date must not be after end date")
	}
	if f.From != "" {
		date, _ := time.Parse(time.DateOnly, f.From)
		q = q.Where(created+" >= "+timeArg, date)
	}
	if f.Until != "" {
		date, _ := time.Parse(time.DateOnly, f.Until)
		q = q.Where(created+" < "+timeArg, date.AddDate(0, 0, 1))
	}
	if f.Before != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(f.Before)
		parts := strings.Split(string(decoded), "|")
		if err != nil || len(parts) != 2 {
			return page, model.InvalidCheckin("invalid job cursor")
		}
		at, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return page, model.InvalidCheckin("invalid job cursor")
		}
		if _, err := uuid.Parse(parts[1]); err != nil {
			return page, model.InvalidCheckin("invalid job cursor")
		}
		// The cursor remains usable after its original job has been cleaned up.
		q = q.Where(created+" < "+timeArg+" OR ("+created+" = "+timeArg+" AND id < ?)", at.UTC(), at.UTC(), parts[1])
	}
	if err := q.Order(created + " DESC, id DESC").Limit(f.Limit + 1).Find(&page.Items).Error; err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextBefore = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID))
	}
	return page, nil
}
