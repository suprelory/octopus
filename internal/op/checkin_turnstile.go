package op

import (
	"context"
	"errors"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func readTurnstileSettings(tx *gorm.DB) (model.CheckinTurnstileSettings, error) {
	var row model.CheckinTurnstileSettings
	err := tx.First(&row, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DefaultCheckinTurnstileSettings(), nil
	}
	row.HasCloudflareKey, row.HasSolverQKey = row.CloudflareKeyCipher != "", row.SolverQKeyCipher != ""
	return row, err
}

func (s *CheckinStore) TurnstileSettings(ctx context.Context) (model.CheckinTurnstileSettings, error) {
	return readTurnstileSettings(s.DB.WithContext(ctx))
}

func (s *CheckinStore) SaveTurnstileSettings(ctx context.Context, row model.CheckinTurnstileSettings, revision int64, replaceCloudflare, replaceSolverQ bool, now time.Time) (model.CheckinTurnstileSettings, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := model.NormalizeCheckinTurnstile(&row.Config); err != nil {
		return row, err
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		previous, err := readTurnstileSettings(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if err != nil {
			return err
		}
		if revision < 0 || previous.Revision != revision {
			return ErrCheckinConflict
		}
		if !replaceCloudflare {
			if previous.CloudflareKeyCipher != "" && previous.Config.CloudflareSolver.BaseURL != row.Config.CloudflareSolver.BaseURL {
				return model.InvalidCheckin("changing the Cloudflare Solver URL requires replacing or clearing its API key")
			}
			row.CloudflareKeyCipher = previous.CloudflareKeyCipher
		}
		if !replaceSolverQ {
			if previous.SolverQKeyCipher != "" && previous.Config.SolverQ.BaseURL != row.Config.SolverQ.BaseURL {
				return model.InvalidCheckin("changing the SolverQ URL requires replacing or clearing its API key")
			}
			row.SolverQKeyCipher = previous.SolverQKeyCipher
		}
		if row.Config.Enabled && ((row.Config.CloudflareSolver.Enabled && row.CloudflareKeyCipher == "") || (row.Config.SolverQ.Enabled && row.SolverQKeyCipher == "")) {
			return model.InvalidCheckin("each enabled Turnstile solver requires an API key")
		}
		row.ID, row.Revision, row.UpdatedAt = 1, previous.Revision+1, now
		if previous.Revision == 0 {
			return tx.Create(&row).Error
		}
		return tx.Save(&row).Error
	})
	row.HasCloudflareKey, row.HasSolverQKey = row.CloudflareKeyCipher != "", row.SolverQKeyCipher != ""
	return row, err
}

// Only an explicit captcha rejection proves that a submitted mutation did not
// run. Persist that evidence before solving; MarkSubmitted must commit again
// before the one token-bearing retry. Crashes on either side remain recoverable.
func (s *CheckinStore) MarkTurnstileRejected(ctx context.Context, item model.CheckinJobItem, run *model.CheckinRun, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := checkinIntentTransaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		var account model.CheckinAccount
		if err := tx.First(&account, "id = ? AND lease_owner = ? AND lease_until > ? AND deleted_at IS NULL AND enabled = ?", item.AccountID, item.LeaseOwner, now, true).Error; err != nil {
			return err
		}
		result := tx.Model(&model.CheckinRun{}).Where("id = ? AND account_id = ? AND submit_intent_at IS NOT NULL AND status NOT IN ?", run.ID, item.AccountID, []string{"success", "already_checked_in"}).Updates(map[string]any{"safe_to_retry": true, "reason": "turnstile_required", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		return nil
	})
	if err == nil {
		run.SafeToRetry = true
	}
	return err
}
