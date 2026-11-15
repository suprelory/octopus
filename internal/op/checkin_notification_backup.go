package op

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validateNotificationDump(dump *model.CheckinDump) error {
	fail := func() error { return errors.New("invalid checkin notification backup") }
	if len(dump.NotificationTargets) > 10000 || len(dump.NotificationDeliveries) > 1000000 {
		return fail()
	}
	if dump.Version < 6 && (len(dump.NotificationTargets) != 0 || len(dump.NotificationDeliveries) != 0) {
		return fail()
	}
	validID := func(value string) bool { _, err := uuid.Parse(value); return err == nil }
	sites, accounts, runs := map[string]bool{}, map[string]string{}, map[string]model.CheckinRun{}
	for _, row := range dump.Sites {
		sites[row.ID] = true
	}
	for _, row := range dump.Accounts {
		accounts[row.ID] = row.SiteID
	}
	for _, row := range dump.Runs {
		runs[row.ID] = row
	}
	targets := map[string]model.CheckinNotificationTarget{}
	var vault *checkinsecret.Vault
	active := 0
	for i := range dump.NotificationTargets {
		row := &dump.NotificationTargets[i]
		if !validID(row.ID) || row.Revision < 1 || row.ConnectionVersion < 1 || row.ConnectionVersion > row.Revision || len(row.Cipher) > 100000 {
			return fail()
		}
		if _, exists := targets[row.ID]; exists {
			return fail()
		}
		if err := notify.NormalizeConfig(&row.Config); err != nil {
			return fail()
		}
		for _, id := range row.Config.SiteIDs {
			if !sites[id] {
				return fail()
			}
		}
		row.ProxyConfigID = row.Config.ProxyConfigID
		if row.DeletedAt == nil {
			active++
			if active > 20 {
				return fail()
			}
			if vault == nil {
				var err error
				vault, err = checkinsecret.Open()
				if err != nil {
					return err
				}
			}
			plain, err := vault.Unseal("notification:"+row.ID, row.Cipher)
			if err != nil {
				return err
			}
			var connection notify.Connection
			if json.Unmarshal(plain, &connection) != nil || notify.NormalizeConnection(row.Config.Kind, &connection) != nil {
				return fail()
			}
		}
		targets[row.ID] = row.CheckinNotificationTarget
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i := range dump.NotificationDeliveries {
		row := &dump.NotificationDeliveries[i]
		_, exists := targets[row.TargetID]
		// Incremental imports retain local target configuration. Historical
		// revisions from another snapshot may therefore exceed the local revision.
		if !validID(row.ID) || ids[row.ID] || !exists || row.TargetRevision < 1 || !slices.Contains(notify.Kinds, row.TargetKind) || len(row.TargetName) > 128 {
			return fail()
		}
		ids[row.ID] = true
		e := row.Event
		if !validID(e.Identity) || len(e.SiteName) > 128 || len(e.AccountName) > 128 || e.OccurredAt.IsZero() {
			return fail()
		}
		if _, err := time.Parse("2006-01-02", e.CheckinDate); err != nil {
			return fail()
		}
		if _, err := time.LoadLocation(e.Timezone); err != nil {
			return fail()
		}
		switch e.Kind {
		case "test":
			if e.Status != "test" || e.Source != "test" || e.AccountID != "" || e.SiteID != "" {
				return fail()
			}
		case "low_balance":
			if e.Status != "low_balance" || e.BalanceMicros == nil || accounts[e.AccountID] != e.SiteID || !sites[e.SiteID] {
				return fail()
			}
		default:
			if !slices.Contains(notify.Events, e.Kind) {
				return fail()
			}
			if e.Kind == "attention" {
				if e.Status != "pending" && e.Status != "manual_required" {
					return fail()
				}
			} else if e.Kind != e.Status {
				return fail()
			}
			run, exists := runs[e.Identity]
			if !exists || run.AccountID != e.AccountID || run.SiteID != e.SiteID || run.CheckinDate != e.CheckinDate {
				return fail()
			}
		}
		if e.Kind != "test" && !slices.Contains([]string{"manual", "scheduled", "confirmation", "confirmation_auto"}, e.Source) {
			return fail()
		}
		if len(e.RewardUnit) > 32 || strings.ContainsAny(e.RewardUnit, "\r\n\x00") || (e.RewardMicros != nil && (*e.RewardMicros < 0 || e.RewardUnit == "")) {
			return fail()
		}
		if e.BalanceMicros != nil {
			if (e.BalanceUnit != "USD" && e.BalanceUnit != "quota") || e.BalanceAt == nil || e.BalanceAt.IsZero() {
				return fail()
			}
		} else if e.BalanceAt != nil || e.BalanceUnit != "" {
			return fail()
		}
		if row.Attempts < 0 || row.Attempts > 100 || row.HTTPStatus < 0 || row.HTTPStatus > 599 || len(row.Title) > 2048 || len(row.Message) > 32768 || strings.ContainsAny(row.Title, "\r\n\x00") || strings.ContainsRune(row.Message, 0) {
			return fail()
		}
		if !slices.Contains([]string{"queued", "sending", "sent", "failed", "uncertain", "skipped"}, row.Status) {
			return fail()
		}
		if (row.Status == "queued" || row.Status == "sending") != (row.FinishedAt == nil) || (row.NextAttemptAt != nil && row.Status != "queued") {
			return fail()
		}
		if row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return fail()
		}
		if !slices.Contains([]string{"queued", "sending", "accepted", "invalid_template", "invalid_connection", "connection_unavailable", "invalid_proxy", "rate_limited", "rate_limit_exhausted", "network_uncertain", "remote_uncertain", "remote_rejected", "redirect_blocked", "invalid_response", "smtp_rejected", "smtp_unavailable", "smtp_tls_failed", "smtp_auth_failed", "smtp_uncertain", "delivery_interrupted", "cooldown", "queue_full", "config_changed", "target_unavailable", "restored_delivery"}, row.Reason) {
			return fail()
		}
		row.EventKey = notificationEventKey(row.TargetID, e)
		if keys[row.EventKey] {
			return fail()
		}
		keys[row.EventKey] = true
	}
	return nil
}

func importNotifications(tx *gorm.DB, dump *model.CheckinDump, runIDs map[string]string, result *model.DBImportResult) error {
	now := time.Now().UTC()
	for _, backup := range dump.NotificationTargets {
		var existing model.CheckinNotificationTarget
		err := tx.First(&existing, "id = ?", backup.ID).Error
		if err == nil {
			continue
		} // Preserve local configuration and secrets.
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row := backup.CheckinNotificationTarget
		row.ConnectionCipher = backup.Cipher
		row.Config.Enabled, row.LeaseOwner, row.LeaseUntil = false, "", nil
		if row.DeletedAt != nil {
			row.ConnectionCipher = ""
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		result.RowsAffected["checkin_notification_targets"]++
	}
	if len(dump.NotificationTargets) > 0 {
		var count int64
		if err := tx.Model(&model.CheckinNotificationTarget{}).Where("deleted_at IS NULL").Count(&count).Error; err != nil {
			return err
		}
		if count > 20 {
			return errors.New("import would exceed 20 notification targets")
		}
	}
	for _, row := range dump.NotificationDeliveries {
		if row.Event.Kind != "test" && row.Event.Kind != "low_balance" {
			if replacement := runIDs[row.Event.Identity]; replacement != "" {
				row.Event.Identity = replacement
			}
		}
		row.EventKey = notificationEventKey(row.TargetID, row.Event)
		var existing model.CheckinNotificationDelivery
		err := tx.First(&existing, "id = ?", row.ID).Error
		if err == nil {
			if existing.TargetID != row.TargetID || existing.EventKey != row.EventKey {
				return errors.New("notification delivery ID conflicts with another event")
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row.LeaseOwner, row.LeaseUntil, row.NextAttemptAt, row.Bucket = "", nil, nil, ""
		if row.Status == "queued" || row.Status == "sending" {
			row.Status, row.Reason, row.FinishedAt, row.UpdatedAt = "skipped", "restored_delivery", &now, now
		}
		r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if r.Error != nil {
			return r.Error
		}
		result.RowsAffected["checkin_notification_deliveries"] += r.RowsAffected
	}
	return nil
}
