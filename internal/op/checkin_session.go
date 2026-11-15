package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// Refresh uses the worker's existing account lease. Persist its intent before
// consuming a one-use refresh token, just as a check-in persists its submit intent.
func (s *CheckinStore) MarkSessionRefresh(ctx context.Context, item model.CheckinJobItem, account model.CheckinAccount, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return checkinIntentTransaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		result := sessionAccount(tx, item, account, now).Where("(refresh_pending = ? OR refresh_pending IS NULL)", false).
			Updates(map[string]any{"refresh_pending": true, "refresh_error": "", "refresh_retry_at": nil})
		return checkSessionWrite(result)
	})
}

func sessionAccount(tx *gorm.DB, item model.CheckinJobItem, account model.CheckinAccount, now time.Time) *gorm.DB {
	return tx.Model(&model.CheckinAccount{}).Where("id = ? AND credential_version = ? AND lease_owner = ? AND lease_until > ? AND deleted_at IS NULL AND enabled = ?", account.ID, account.CredentialVersion, item.LeaseOwner, now, true)
}

func checkSessionWrite(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrCheckinConflict
	}
	return nil
}

func (s *CheckinStore) SaveSessionRefresh(ctx context.Context, item model.CheckinJobItem, before model.CheckinAccount, cipher string, expiresAt *time.Time, refreshedAt, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return checkinIntentTransaction(s.DB.WithContext(ctx), func(tx *gorm.DB) error {
		return checkSessionWrite(sessionAccount(tx, item, before, now).Where("refresh_pending = ?", true).
			Updates(map[string]any{
				"credential_cipher": cipher, "credential_version": before.CredentialVersion + 1,
				"credential_identity_version": before.IdentityVersion(), "has_refresh_token": true,
				"token_expires_at": expiresAt, "token_refreshed_at": refreshedAt,
				"refresh_pending": false, "refresh_error": "", "refresh_retry_at": nil,
			}))
	})
}

func (s *CheckinStore) FailSessionRefresh(ctx context.Context, item model.CheckinJobItem, account model.CheckinAccount, pending bool, reason string, retryAt *time.Time, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return checkSessionWrite(sessionAccount(s.DB.WithContext(ctx), item, account, now).
		Updates(map[string]any{"refresh_pending": pending, "refresh_error": reason, "refresh_retry_at": retryAt}))
}
