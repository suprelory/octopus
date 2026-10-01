package sitesync

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// Capture a fresh balance with the resolved check-in credentials. Cached account
// balances may predate unrelated spending or deposits and cannot be a baseline.
// Income-log requests are unnecessary here and would delay the actual check-in.
func fetchAccountBalanceBeforeCheckin(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, accessToken string) siteBalanceFetchResult {
	snapshotCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return fetchSiteAccountBalanceResult(snapshotCtx, siteRecord, account, accessToken, firstManagedPlatformUserID(account), false)
}

// Keep the committed check-in outcome even if refreshing or saving its reward
// fails. Only fill a missing reward once, before consumers receive the result.
func fillCheckinRewardFromBalance(ctx context.Context, result *model.SiteCheckinResult, before, after siteBalanceFetchResult) {
	if result == nil || result.Status != model.SiteExecutionStatusSuccess || result.Reason != model.SiteCheckinReasonCheckedIn ||
		strings.TrimSpace(result.Reward) != "" || !before.ok || !after.ok {
		return
	}
	delta := after.balance - before.balance
	if math.IsNaN(delta) || math.IsInf(delta, 0) || delta < 0 {
		return
	}
	// Platform balances are expressed in USD. Match the six decimal places used
	// by reward statistics without exposing floating-point subtraction noise.
	reward := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(delta, 'f', 6, 64), "0"), ".")
	if reward == "-0" {
		reward = "0"
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	saved := db.GetDB().WithContext(persistCtx).Model(&model.SiteCheckinLog{}).
		Where("id = ? AND status = ? AND reason = ? AND reward = ?", result.LogID, model.SiteExecutionStatusSuccess, model.SiteCheckinReasonCheckedIn, "").
		Update("reward", reward)
	if saved.Error != nil {
		log.Warnf("could not save balance-based checkin reward for account %d: %v", result.AccountID, saved.Error)
		return
	}
	if saved.RowsAffected > 0 {
		result.Reward = reward
	}
}
