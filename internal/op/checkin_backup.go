package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func exportCheckin(ctx context.Context, conn *gorm.DB) (*model.CheckinDump, error) {
	dump := &model.CheckinDump{Version: 7}
	var accounts []model.CheckinAccount
	var notifications []model.CheckinNotificationTarget
	for _, target := range []any{&dump.Sites, &accounts, &dump.Runs, &dump.Attempts, &dump.Jobs, &dump.Items, &notifications, &dump.NotificationDeliveries} {
		if err := conn.WithContext(ctx).Find(target).Error; err != nil {
			return nil, err
		}
	}
	for _, account := range accounts {
		dump.Accounts = append(dump.Accounts, model.CheckinAccountBackup{CheckinAccount: account, Cipher: account.CredentialCipher, IdentityVersion: account.IdentityVersion()})
	}
	for _, target := range notifications {
		dump.NotificationTargets = append(dump.NotificationTargets, model.CheckinNotificationBackup{CheckinNotificationTarget: target, Cipher: target.ConnectionCipher})
	}
	turnstile, err := readTurnstileSettings(conn.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if turnstile.Revision > 0 {
		dump.Turnstile = &model.CheckinTurnstileBackup{CheckinTurnstileSettings: turnstile, CloudflareCipher: turnstile.CloudflareKeyCipher, SolverQCipher: turnstile.SolverQKeyCipher}
	}
	return dump, nil
}

func validateCheckinDump(dump *model.CheckinDump) error {
	if dump == nil {
		return nil
	}
	if dump.Version < 1 || dump.Version > 7 {
		return errors.New("unsupported checkin backup version")
	}
	if len(dump.Sites) > 10000 || len(dump.Accounts) > 100000 || len(dump.Runs) > 1000000 || len(dump.Attempts) > 2000000 || len(dump.Jobs) > 1000000 || len(dump.Items) > 1000000 {
		return errors.New("checkin backup exceeds resource limits")
	}
	validID := func(id string) error {
		if _, err := uuid.Parse(id); err != nil {
			return errors.New("invalid checkin backup ID")
		}
		return nil
	}
	var vault *checkinsecret.Vault
	sites := map[string]model.CheckinSite{}
	for i := range dump.Sites {
		if err := validID(dump.Sites[i].ID); err != nil {
			return err
		}
		if err := model.NormalizeCheckinSite(&dump.Sites[i].Config); err != nil {
			return fmt.Errorf("invalid checkin site: %w", err)
		}
		dump.Sites[i].ProxyConfigID = dump.Sites[i].Config.ProxyConfigID
		sites[dump.Sites[i].ID] = dump.Sites[i]
	}
	for i := range dump.Accounts {
		account := &dump.Accounts[i]
		if err := validID(account.ID); err != nil {
			return err
		}
		if err := validID(account.SiteID); err != nil {
			return err
		}
		if account.Name == "" || len(account.Name) > 128 || account.CredentialVersion < 1 || account.Revision < 1 {
			return errors.New("invalid checkin account metadata")
		}
		if account.IdentityVersion < 0 || account.IdentityVersion > account.CredentialVersion {
			return errors.New("invalid checkin credential identity version")
		}
		account.CredentialIdentityVersion = account.IdentityVersion
		if err := validateCheckinObservation(account.Balance, account.Inspection); err != nil {
			return err
		}
		switch account.RefreshError {
		case "", "sub2api_refresh_deferred", "sub2api_refresh_uncertain", "sub2api_refresh_rejected", "sub2api_auth_rejected", "sub2api_session_revoked", "sub2api_session_binding_mismatch", "sub2api_account_unavailable", "sub2api_refresh_identity_mismatch":
		default:
			return errors.New("invalid checkin session status")
		}
		if account.RefreshRetryAt != nil && (account.RefreshPending || account.RefreshError != "sub2api_refresh_deferred") {
			return errors.New("invalid checkin refresh retry")
		}
		if err := model.NormalizeCheckinAccountProxy(&account.CheckinAccount); err != nil {
			return err
		}
		if account.DeletedAt == nil {
			if vault == nil {
				var err error
				vault, err = checkinsecret.Open()
				if err != nil {
					return err
				}
			}
			plain, err := vault.Unseal(account.ID, account.Cipher)
			if err != nil {
				return err
			}
			var credentials model.CheckinCredentials
			if json.Unmarshal(plain, &credentials) != nil || model.ValidateCheckinCredentials(credentials) != nil || credentials.Type != account.CredentialType {
				return errors.New("invalid encrypted checkin backup credentials")
			}
			platform := sites[account.SiteID].Config.Adapter
			if !model.CheckinCredentialSupported(platform, credentials.Type) || (credentials.Session != nil && platform != "anyrouter" && platform != "agentrouter") {
				return errors.New("checkin credentials are incompatible with their platform")
			}
			account.CredentialUserID = credentials.UserID
			account.HasRefreshToken = credentials.RefreshToken != ""
			account.TokenExpiresAt = credentials.ExpiresAt
			account.HasSession = credentials.Session != nil && (credentials.Session.AccessToken != "" || len(credentials.Session.Cookies) > 0)
			if (account.HasRefreshToken || account.RefreshPending || account.RefreshError != "") && sites[account.SiteID].Config.Adapter != "sub2api" {
				return errors.New("session credentials require a Sub2API site")
			}
			if !account.HasRefreshToken && (account.RefreshPending || account.RefreshError != "") {
				return errors.New("refresh state requires a refresh token")
			}
		}
	}
	for _, run := range dump.Runs {
		for _, id := range []string{run.ID, run.SiteID, run.AccountID} {
			if err := validID(id); err != nil {
				return err
			}
		}
		if _, err := time.Parse("2006-01-02", run.CheckinDate); err != nil {
			return errors.New("invalid checkin date")
		}
		if run.RewardMicros != nil && *run.RewardMicros < 0 {
			return errors.New("negative checkin reward")
		}
		switch run.Status {
		case "running", "pending", "manual_required", "success", "already_checked_in", "failed", "skipped":
		default:
			return errors.New("invalid checkin result")
		}
	}
	for _, attempt := range dump.Attempts {
		if err := validID(attempt.ID); err != nil {
			return err
		}
		if err := validID(attempt.RunID); err != nil {
			return err
		}
		if len(attempt.Message) > 256 {
			return errors.New("invalid checkin attempt message")
		}
	}
	for _, job := range dump.Jobs {
		if err := validID(job.ID); err != nil {
			return err
		}
	}
	for _, item := range dump.Items {
		for _, id := range []string{item.ID, item.JobID, item.AccountID, item.SiteID} {
			if err := validID(id); err != nil {
				return err
			}
		}
	}
	if err := validateCheckinGraph(dump); err != nil {
		return err
	}
	if err := validateNotificationDump(dump); err != nil {
		return err
	}
	return validateTurnstileDump(dump)
}

func importCheckin(tx *gorm.DB, dump *model.CheckinDump, result *model.DBImportResult) error {
	if dump == nil {
		return nil
	}
	insert := func(name string, rows any) error {
		r := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 100)
		result.RowsAffected[name] += r.RowsAffected
		return r.Error
	}
	if len(dump.Sites) > 0 {
		if err := insert("checkin_sites", &dump.Sites); err != nil {
			return err
		}
	}
	newAccounts := map[string]bool{}
	backupSites := make(map[string]model.CheckinSiteConfig, len(dump.Sites))
	for _, site := range dump.Sites {
		backupSites[site.ID] = site.Config
	}
	for _, backup := range dump.Accounts {
		account := backup.CheckinAccount
		var existing model.CheckinAccount
		err := tx.First(&existing, "id = ?", account.ID).Error
		if err == nil {
			if existing.SiteID != account.SiteID {
				return errors.New("checkin account ID conflicts with another site")
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		account.CredentialCipher = backup.Cipher
		account.AutoCheckin, account.NextRunAt = false, nil
		account.LeaseOwner, account.LeaseUntil = "", nil
		interruptCheckinInspection(&account, time.Now().UTC())
		if account.Balance != nil || account.Inspection != nil {
			var targetSite model.CheckinSite
			if err := tx.First(&targetSite, "id = ?", account.SiteID).Error; err != nil {
				return err
			}
			original := backupSites[account.SiteID]
			if targetSite.Config.BaseURL != original.BaseURL || targetSite.Config.Adapter != original.Adapter {
				account.Balance, account.Inspection = nil, nil
			}
		}
		if account.DeletedAt != nil {
			account.CredentialCipher = ""
			account.HasRefreshToken, account.RefreshPending, account.RefreshError = false, false, ""
			account.TokenExpiresAt, account.TokenRefreshedAt, account.RefreshRetryAt = nil, nil, nil
			account.HasSession, account.SessionSavedAt = false, nil
		}
		if err := insert("checkin_accounts", &account); err != nil {
			return err
		}
		newAccounts[account.ID] = true
	}
	runIDs := map[string]string{}
	for _, incoming := range dump.Runs {
		var existingID model.CheckinRun
		err := tx.First(&existingID, "id = ?", incoming.ID).Error
		if err == nil && (existingID.AccountID != incoming.AccountID || existingID.CheckinDate != incoming.CheckinDate) {
			return errors.New("checkin run ID conflicts with another daily record")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		originalID := incoming.ID
		if incoming.Status == "running" || incoming.Status == "pending" {
			incoming.Status, incoming.Reason, incoming.Phase = "manual_required", "restored_pending", "finished"
		}
		incoming.NextConfirmAt = nil
		if err := insert("checkin_runs", &incoming); err != nil {
			return err
		}
		var existing model.CheckinRun
		if err := tx.First(&existing, "account_id = ? AND checkin_date = ?", incoming.AccountID, incoming.CheckinDate).Error; err != nil {
			return err
		}
		runIDs[originalID] = existing.ID
	}
	for _, backup := range dump.Accounts {
		if !newAccounts[backup.ID] || backup.LastRunID == "" {
			continue
		}
		id := runIDs[backup.LastRunID]
		var run model.CheckinRun
		if err := tx.First(&run, "id = ? AND account_id = ?", id, backup.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CheckinAccount{}).Where("id = ?", backup.ID).Updates(map[string]any{"last_run_id": run.ID, "last_status": run.Status}).Error; err != nil {
			return err
		}
	}
	newJobs := map[string]bool{}
	for _, job := range dump.Jobs {
		if job.Scope.Kind == "confirmation" {
			job.Scope.IDs = []string{runIDs[job.Scope.IDs[0]]}
		}
		var existing model.CheckinJob
		err := tx.First(&existing, "id = ?", job.ID).Error
		if err == nil {
			if existing.Source != job.Source || existing.Total != job.Total || !sameCheckinScope(existing.Scope, job.Scope) {
				return errors.New("checkin job ID conflicts with another scope")
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		job.IdempotencyKey = "restored:" + uuid.NewString()
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "interrupted"
		}
		if err := insert("checkin_jobs", &job); err != nil {
			return err
		}
		newJobs[job.ID] = true
	}
	for _, item := range dump.Items {
		if replacement := runIDs[item.RunID]; replacement != "" {
			item.RunID = replacement
		}
		var existing model.CheckinJobItem
		err := tx.First(&existing, "id = ?", item.ID).Error
		if err == nil && (existing.JobID != item.JobID || existing.AccountID != item.AccountID || existing.CheckinDate != item.CheckinDate) {
			return errors.New("checkin job item ID conflicts with another account")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !newJobs[item.JobID] {
			var count int64
			if err := tx.Model(&model.CheckinJobItem{}).Where("job_id = ? AND account_id = ? AND checkin_date = ?", item.JobID, item.AccountID, item.CheckinDate).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return errors.New("checkin job membership conflicts with existing data")
			}
			continue
		}
		item.LeaseOwner, item.LeaseUntil = "", nil
		if item.State != "done" {
			item.State, item.Outcome = "done", "skipped"
		}
		if err := insert("checkin_job_items", &item); err != nil {
			return err
		}
	}
	for _, attempt := range dump.Attempts {
		if replacement := runIDs[attempt.RunID]; replacement != "" {
			attempt.RunID = replacement
		}
		var existing model.CheckinAttempt
		err := tx.First(&existing, "id = ?", attempt.ID).Error
		if err == nil && (existing.RunID != attempt.RunID || existing.JobID != attempt.JobID) {
			return errors.New("checkin attempt ID conflicts with another run")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		attempt.Message = "Upstream response received"
		if attempt.Reason == "network_error" {
			attempt.Message = "Request failed, timed out, or exceeded the response limit"
		}
		if model.IsTurnstileReason(attempt.Reason) {
			attempt.Message = "Turnstile verification step completed"
		}
		if err := insert("checkin_attempts", &attempt); err != nil {
			return err
		}
	}
	for id := range newJobs {
		if err := reconcileCheckinJob(tx, id); err != nil {
			return err
		}
	}
	if err := importNotifications(tx, dump, runIDs, result); err != nil {
		return err
	}
	return importTurnstile(tx, dump, result)
}
