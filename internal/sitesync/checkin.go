package sitesync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

// All in-process entry points share this guard. The map contains only active
// accounts and the mutex is never held during database or upstream calls.
var accountCheckins = struct {
	sync.Mutex
	running map[int]struct{}
}{running: make(map[int]struct{})}

func acquireAccountCheckin(accountID int) bool {
	accountCheckins.Lock()
	defer accountCheckins.Unlock()
	if _, running := accountCheckins.running[accountID]; running {
		return false
	}
	accountCheckins.running[accountID] = struct{}{}
	return true
}

func releaseAccountCheckin(accountID int) {
	accountCheckins.Lock()
	delete(accountCheckins.running, accountID)
	accountCheckins.Unlock()
}

func CheckinAccount(ctx context.Context, accountID int) (*model.SiteCheckinResult, error) {
	return checkinAccountWithTrigger(ctx, accountID, SiteBatchTriggerManual)
}

func checkinAccountWithTrigger(ctx context.Context, accountID int, trigger SiteBatchTrigger) (*model.SiteCheckinResult, error) {
	started := time.Now()
	acquired := acquireAccountCheckin(accountID)
	if acquired {
		defer releaseAccountCheckin(accountID)
	}
	// Load after acquiring the guard, so a preceding run cannot leave us with a
	// stale success timestamp, failure count or next execution time.
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	if !acquired {
		return persistCheckinOutcome(ctx, siteRecord, account, trigger, started, &model.SiteCheckinResult{
			Status: model.SiteExecutionStatusSkipped, Reason: model.SiteCheckinReasonAlreadyRunning,
			Message: "checkin is already running",
		}, "", false)
	}
	if trigger == SiteBatchTriggerScheduled {
		// A manual run or an account edit may have completed after the batch
		// selected its work. Recheck eligibility while holding the account guard.
		nextAt, scheduleErr := ensureAccountCheckinSchedule(ctx, siteRecord, account, time.Now())
		if scheduleErr != nil {
			return nil, sanitizeSiteError(scheduleErr)
		}
		if nextAt == nil || time.Now().Before(*nextAt) {
			return persistCheckinOutcome(ctx, siteRecord, account, trigger, started, &model.SiteCheckinResult{
				Status: model.SiteExecutionStatusSkipped, Reason: string(SiteBatchReasonScheduledLater),
				Message: "automatic checkin is not due",
			}, "", false)
		}
	}

	result, accessToken, runErr := checkinAccountState(ctx, siteRecord, account)
	if runErr != nil {
		result = &model.SiteCheckinResult{
			Status:  model.SiteExecutionStatusFailed,
			Reason:  string(siteBatchReason(sanitizeSiteError(runErr))),
			Message: apperror.Message(runErr),
		}
	} else if result == nil {
		runErr = fmt.Errorf("checkin returned no result")
		result = &model.SiteCheckinResult{Status: model.SiteExecutionStatusFailed, Reason: string(SiteBatchReasonInternalError), Message: runErr.Error()}
	} else if result.Status == model.SiteExecutionStatusFailed {
		runErr = newSiteBusinessError(result.Message)
		result.Reason = string(siteBatchReason(runErr))
	} else if result.Status == model.SiteExecutionStatusSkipped && result.Reason == "" {
		result.Reason = model.SiteCheckinReasonUnsupported
	}
	result, err = persistCheckinOutcome(ctx, siteRecord, account, trigger, started, result, accessToken, true)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	if runErr != nil {
		safeErr := sanitizeSiteError(runErr)
		return result, apperror.Wrap(apperror.Code(safeErr), result.Message, safeErr).
			WithStatus(apperror.Status(safeErr)).WithParams(apperror.Params(safeErr))
	}
	return result, nil
}

func newSuccessfulCheckinResult(message, reward string) *model.SiteCheckinResult {
	reason := model.SiteCheckinReasonCheckedIn
	if isAlreadyCheckedInMessage(message) {
		reason = model.SiteCheckinReasonAlreadyCheckedIn
		reward = ""
	}
	return &model.SiteCheckinResult{
		Status: model.SiteExecutionStatusSuccess, Reason: reason,
		Message: firstNonEmptyString(message, "checkin success"), Reward: reward,
	}
}

func checkinRewardString(value any) string {
	if number, ok := value.(float64); ok {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return jsonString(value)
}

func persistCheckinOutcome(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, trigger SiteBatchTrigger, started time.Time, result *model.SiteCheckinResult, accessToken string, updateAccount bool) (*model.SiteCheckinResult, error) {
	finished := time.Now()
	result.SiteID, result.AccountID = siteRecord.ID, account.ID
	result.Message = sanitizeCheckinText(result.Message, account, accessToken)
	result.Reward = sanitizeCheckinText(result.Reward, account, accessToken)
	if result.Status != model.SiteExecutionStatusSuccess || result.Reason == model.SiteCheckinReasonAlreadyCheckedIn {
		result.Reward = ""
	}
	entry := model.SiteCheckinLog{
		ID: snowflake.GenerateID(), SiteID: siteRecord.ID, AccountID: account.ID,
		SiteName: siteRecord.Name, AccountName: account.Name, Platform: siteRecord.Platform,
		Source: string(trigger), Status: result.Status, Reason: result.Reason,
		Message: result.Message, Reward: result.Reward, DurationMs: finished.Sub(started).Milliseconds(),
		StartedAt: started.UTC(), FinishedAt: finished.UTC(),
	}
	// A client disconnect or a batch deadline must not discard an upstream
	// outcome that has already happened. Bound this final database write alone.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := db.GetDB().WithContext(persistCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		if !updateAccount {
			return nil
		}
		return updateAccountCheckinState(tx, siteRecord, account, result.Status, result.Message, accessToken, finished)
	})
	if err != nil {
		return nil, fmt.Errorf("persist checkin outcome: %w", err)
	}
	result.LogID = entry.ID
	return result, nil
}

func sanitizeCheckinText(value string, account *model.SiteAccount, resolvedToken string) string {
	secrets := []string{account.Password, account.AccessToken, account.APIKey, account.RefreshToken, resolvedToken}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
		if looksLikeCookieToken(secret) {
			for _, part := range strings.Split(secret, ";") {
				_, cookieValue, ok := strings.Cut(part, "=")
				if cookieValue = strings.TrimSpace(cookieValue); ok && cookieValue != "" {
					value = strings.ReplaceAll(value, cookieValue, "[REDACTED]")
				}
			}
		}
	}
	return sanitizeSiteStatusText(value)
}
