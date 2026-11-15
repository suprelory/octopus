package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *CheckinStore) NotificationTargets(ctx context.Context) ([]model.CheckinNotificationTarget, error) {
	rows := []model.CheckinNotificationTarget{}
	err := s.DB.WithContext(ctx).Where("deleted_at IS NULL").Order("created_at, id").Find(&rows).Error
	for i := range rows {
		rows[i].HasConnection = rows[i].ConnectionCipher != ""
	}
	return rows, err
}

func (s *CheckinStore) NotificationTarget(ctx context.Context, id string) (model.CheckinNotificationTarget, error) {
	var row model.CheckinNotificationTarget
	err := s.DB.WithContext(ctx).First(&row, "id = ? AND deleted_at IS NULL", id).Error
	row.HasConnection = row.ConnectionCipher != ""
	return row, err
}

func (s *CheckinStore) SaveNotificationTarget(ctx context.Context, row model.CheckinNotificationTarget, revision int64, replaceConnection bool, now time.Time) (model.CheckinNotificationTarget, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	proxyWriteMu.Lock()
	defer proxyWriteMu.Unlock()
	if err := notify.NormalizeConfig(&row.Config); err != nil {
		return row, err
	}
	row.ProxyConfigID = row.Config.ProxyConfigID
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous model.CheckinNotificationTarget
		if revision > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id = ? AND deleted_at IS NULL", row.ID).Error; err != nil {
				return err
			}
			if previous.Revision != revision || (previous.LeaseUntil != nil && previous.LeaseUntil.After(now)) {
				return ErrCheckinConflict
			}
			if previous.Config.Kind != row.Config.Kind && !replaceConnection {
				return model.InvalidCheckin("changing notification platform requires a new connection")
			}
		} else {
			var count int64
			if err := tx.Model(&model.CheckinNotificationTarget{}).Where("deleted_at IS NULL").Count(&count).Error; err != nil {
				return err
			}
			if count >= 20 {
				return model.InvalidCheckin("at most 20 notification targets are supported")
			}
			if !replaceConnection {
				return model.InvalidCheckin("notification connection is required")
			}
		}
		if err := ValidateProxyBindingTx(tx, row.Config.ProxySelection(), !sameProxyID(previous.ProxyConfigID, row.ProxyConfigID)); err != nil {
			return model.InvalidCheckin("notification proxy is unavailable")
		}
		for _, siteID := range row.Config.SiteIDs {
			var count int64
			q := tx.Model(&model.CheckinSite{}).Where("id = ?", siteID)
			// Keep existing deleted-site scopes editable without broadening them.
			if !slices.Contains(previous.Config.SiteIDs, siteID) {
				q = q.Where("deleted_at IS NULL")
			}
			if err := q.Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return model.InvalidCheckin("notification site does not exist")
			}
		}
		row.ConnectionVersion = previous.ConnectionVersion
		if !replaceConnection {
			row.ConnectionCipher = previous.ConnectionCipher
		}
		if row.ConnectionCipher == "" {
			return model.InvalidCheckin("notification connection is required")
		}
		if replaceConnection || previous.Config.ProxyMode != row.Config.ProxyMode || !sameProxyID(previous.ProxyConfigID, row.ProxyConfigID) {
			row.ConnectionVersion++
		}
		row.Revision, row.UpdatedAt = revision+1, now
		if revision == 0 {
			row.CreatedAt = now
			return tx.Create(&row).Error
		}
		if err := tx.Model(&previous).Select("Config", "ProxyConfigID", "ConnectionCipher", "ConnectionVersion", "Revision", "UpdatedAt").Updates(&row).Error; err != nil {
			return err
		}
		return skipNotificationQueue(tx, row.ID, "config_changed", now)
	})
	if err != nil {
		return row, err
	}
	return s.NotificationTarget(ctx, row.ID)
}

func skipNotificationQueue(tx *gorm.DB, id, reason string, now time.Time) error {
	return tx.Model(&model.CheckinNotificationDelivery{}).Where("target_id = ? AND status = ?", id, "queued").Updates(map[string]any{"status": "skipped", "reason": reason, "next_attempt_at": nil, "finished_at": now, "updated_at": now}).Error
}

func (s *CheckinStore) DeleteNotificationTarget(ctx context.Context, id string, revision int64, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	proxyWriteMu.Lock()
	defer proxyWriteMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.CheckinNotificationTarget
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
			return err
		}
		if row.Revision != revision || (row.LeaseUntil != nil && row.LeaseUntil.After(now)) {
			return ErrCheckinConflict
		}
		row.Config.Enabled, row.Config.ProxyMode, row.Config.ProxyConfigID = false, "direct", nil
		row.ConnectionCipher, row.ProxyConfigID, row.DeletedAt, row.UpdatedAt = "", nil, &now, now
		row.Revision++
		if err := tx.Model(&row).Select("Config", "ProxyConfigID", "ConnectionCipher", "DeletedAt", "Revision", "UpdatedAt").Updates(&row).Error; err != nil {
			return err
		}
		return skipNotificationQueue(tx, id, "target_unavailable", now)
	})
}

func notificationHash(parts ...any) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func notificationEventKey(targetID string, event model.CheckinNotificationEvent) string {
	return notificationHash(targetID, event.Identity, event.Kind, event.Status)
}

func matchesNotification(c model.CheckinNotificationConfig, event model.CheckinNotificationEvent) bool {
	if !c.Enabled || !slices.Contains(c.Events, event.Kind) || (c.Scope != "all" && !slices.Contains(c.SiteIDs, event.SiteID)) {
		return false
	}
	if !c.IncludeManual && event.Source != "scheduled" && event.Source != "confirmation_auto" {
		return false
	}
	if event.Kind == "low_balance" {
		return event.BalanceMicros != nil && c.ThresholdMicros != nil && event.BalanceUnit == c.ThresholdUnit && *event.BalanceMicros < *c.ThresholdMicros
	}
	return true
}

func notificationRow(target model.CheckinNotificationTarget, event model.CheckinNotificationEvent, now time.Time) model.CheckinNotificationDelivery {
	row := model.CheckinNotificationDelivery{ID: uuid.NewString(), TargetID: target.ID, TargetName: target.Config.Name, TargetKind: target.Config.Kind, TargetRevision: target.Revision, EventKey: notificationEventKey(target.ID, event), Bucket: notificationHash(target.ID, target.ConnectionVersion, event.AccountID, event.Kind), Event: event, Status: "queued", Reason: "queued", CreatedAt: now, UpdatedAt: now}
	var err error
	row.Title, row.Message, err = notify.Render(target.Config, event)
	if err != nil {
		row.Status, row.Reason, row.FinishedAt = "failed", "invalid_template", &now
	}
	return row
}

func notificationCapacity(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&model.CheckinNotificationDelivery{}).Where("status IN ?", []string{"queued", "sending"}).Count(&count).Error; err != nil {
		return err
	}
	if count >= 1000 {
		return ErrCheckinBusy
	}
	return nil
}

func (s *CheckinStore) EnqueueNotifications(ctx context.Context, event model.CheckinNotificationEvent, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var targets []model.CheckinNotificationTarget
		if err := tx.Where("deleted_at IS NULL").Find(&targets).Error; err != nil {
			return err
		}
		for _, target := range targets {
			if !matchesNotification(target.Config, event) {
				continue
			}
			row := notificationRow(target, event, now)
			var existing int64
			if err := tx.Model(&model.CheckinNotificationDelivery{}).Where("event_key = ?", row.EventKey).Count(&existing).Error; err != nil {
				return err
			}
			if existing != 0 {
				continue
			}
			if target.Config.CooldownSeconds > 0 {
				var recent int64
				if err := tx.Model(&model.CheckinNotificationDelivery{}).Where("bucket = ? AND status <> ? AND updated_at > ?", row.Bucket, "skipped", now.Add(-time.Duration(target.Config.CooldownSeconds)*time.Second)).Count(&recent).Error; err != nil {
					return err
				}
				if recent > 0 {
					row.Status, row.Reason, row.FinishedAt = "skipped", "cooldown", &now
				}
			}
			if row.Status == "queued" {
				if err := notificationCapacity(tx); err != nil {
					if !errors.Is(err, ErrCheckinBusy) {
						return err
					}
					row.Status, row.Reason, row.FinishedAt = "skipped", "queue_full", &now
				}
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CheckinStore) TestNotification(ctx context.Context, id string, revision int64, now time.Time) (model.CheckinNotificationDelivery, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var row model.CheckinNotificationDelivery
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target model.CheckinNotificationTarget
		if err := tx.First(&target, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
			return err
		}
		if target.Revision != revision {
			return ErrCheckinConflict
		}
		// Debounce active/recent tests without reserving a business cooldown bucket.
		err := tx.Where("target_id = ? AND target_revision = ? AND bucket = ? AND (status IN ? OR created_at > ?)", id, revision, notificationHash("test", id, revision), []string{"queued", "sending"}, now.Add(-10*time.Second)).Order("created_at DESC").First(&row).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := notificationCapacity(tx); err != nil {
			return err
		}
		event := NotificationTestEvent(now)
		row = notificationRow(target, event, now)
		row.Bucket = notificationHash("test", id, revision)
		return tx.Create(&row).Error
	})
	return row, err
}

func NotificationTestEvent(now time.Time) model.CheckinNotificationEvent {
	reward, balance := int64(1500000), int64(12000000)
	return model.CheckinNotificationEvent{Kind: "test", Identity: uuid.NewString(), SiteName: "Example site", AccountName: "Example account", CheckinDate: now.Format("2006-01-02"), Timezone: "UTC", Status: "test", Source: "test", RewardMicros: &reward, RewardUnit: "USD", BalanceMicros: &balance, BalanceUnit: "USD", BalanceAt: &now, OccurredAt: now}
}

type NotificationDeliveryFilter struct {
	TargetID, Before string
	Limit            int
}

func (s *CheckinStore) NotificationDeliveries(ctx context.Context, filter NotificationDeliveryFilter) ([]model.CheckinNotificationDelivery, error) {
	rows := []model.CheckinNotificationDelivery{}
	q := s.DB.WithContext(ctx).Model(&model.CheckinNotificationDelivery{})
	if filter.TargetID != "" {
		q = q.Where("target_id = ?", filter.TargetID)
	}
	if filter.Before != "" {
		var cursor model.CheckinNotificationDelivery
		if err := s.DB.WithContext(ctx).First(&cursor, "id = ?", filter.Before).Error; err != nil {
			return rows, err
		}
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	err := q.Order("created_at DESC, id DESC").Limit(filter.Limit).Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) RetryNotification(ctx context.Context, id string, attempts int, revision int64, now time.Time) (model.CheckinNotificationDelivery, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var row model.CheckinNotificationDelivery
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Attempts != attempts || attempts >= 100 || !slices.Contains([]string{"failed", "uncertain", "skipped"}, row.Status) {
			return ErrCheckinConflict
		}
		var target model.CheckinNotificationTarget
		if err := tx.First(&target, "id = ? AND deleted_at IS NULL", row.TargetID).Error; err != nil {
			return err
		}
		if target.Revision != revision || (!target.Config.Enabled && row.Event.Kind != "test") {
			return ErrCheckinConflict
		}
		if err := notificationCapacity(tx); err != nil {
			return err
		}
		title, message, err := notify.Render(target.Config, row.Event)
		if err != nil {
			return err
		}
		row.Title, row.Message = title, message
		row.TargetRevision, row.TargetName, row.TargetKind = target.Revision, target.Config.Name, target.Config.Kind
		row.Status, row.Reason, row.HTTPStatus = "queued", "queued", 0
		row.NextAttemptAt, row.FinishedAt, row.UpdatedAt = nil, nil, now
		row.Bucket = notificationHash(target.ID, target.ConnectionVersion, row.Event.AccountID, row.Event.Kind)
		if row.Event.Kind == "test" {
			row.Bucket = notificationHash("test", target.ID, target.Revision)
		}
		return tx.Model(&row).Select("Title", "Message", "TargetRevision", "TargetName", "TargetKind", "Status", "Reason", "HTTPStatus", "NextAttemptAt", "FinishedAt", "UpdatedAt", "Bucket").Updates(&row).Error
	})
	return row, err
}

// A sending lease that expires is deliberately uncertain, never auto-replayed.
func recoverNotifications(tx *gorm.DB, now time.Time) error {
	if err := tx.Model(&model.CheckinNotificationDelivery{}).Where("status = ? AND (lease_until IS NULL OR lease_until <= ?)", "sending", now).Updates(map[string]any{"status": "uncertain", "reason": "delivery_interrupted", "lease_owner": "", "lease_until": nil, "next_attempt_at": nil, "finished_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&model.CheckinNotificationTarget{}).Where("lease_until <= ?", now).Updates(map[string]any{"lease_owner": "", "lease_until": nil}).Error
}

func (s *CheckinStore) ClaimNotification(ctx context.Context, now time.Time) (model.CheckinNotificationDelivery, model.CheckinNotificationTarget, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var delivery model.CheckinNotificationDelivery
	var target model.CheckinNotificationTarget
	claimed := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := recoverNotifications(tx, now); err != nil {
			return err
		}
		var rows []model.CheckinNotificationDelivery
		if err := tx.Where("status = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", "queued", now).Order("created_at, id").Limit(1000).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			// A populated GORM primary key becomes an implicit query predicate.
			// Reset it when skipping a busy target and trying another destination.
			target = model.CheckinNotificationTarget{}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", row.TargetID).Error; err != nil {
				return err
			}
			if target.DeletedAt != nil || target.Revision != row.TargetRevision || (!target.Config.Enabled && row.Event.Kind != "test") {
				if err := tx.Model(&row).Updates(map[string]any{"status": "skipped", "reason": "target_unavailable", "finished_at": now, "updated_at": now, "next_attempt_at": nil}).Error; err != nil {
					return err
				}
				continue
			}
			if target.LeaseUntil != nil && target.LeaseUntil.After(now) {
				continue
			}
			owner, until := uuid.NewString(), now.Add(45*time.Second)
			r := tx.Model(&row).Where("status = ?", "queued").Updates(map[string]any{"status": "sending", "reason": "sending", "attempts": gorm.Expr("attempts + 1"), "lease_owner": owner, "lease_until": until, "next_attempt_at": nil, "updated_at": now})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				continue
			}
			if err := tx.Model(&target).Updates(map[string]any{"lease_owner": owner, "lease_until": until}).Error; err != nil {
				return err
			}
			row.Status, row.Reason, row.Attempts, row.LeaseOwner, row.LeaseUntil, row.NextAttemptAt = "sending", "sending", row.Attempts+1, owner, &until, nil
			delivery, claimed = row, true
			return nil
		}
		return nil
	})
	return delivery, target, claimed, err
}

func (s *CheckinStore) FinishNotification(ctx context.Context, row model.CheckinNotificationDelivery, result notify.Result, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	owner := row.LeaseOwner // GORM Updates also mutates the model's lease fields.
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var next *time.Time
		var finished *time.Time = &now
		if result.Status == "queued" {
			if row.Attempts >= 3 {
				result.Status, result.Reason = "failed", "rate_limit_exhausted"
			} else {
				value := now.Add(result.RetryAfter)
				next, finished = &value, nil
			}
		}
		r := tx.Model(&row).Where("status = ? AND lease_owner = ?", "sending", row.LeaseOwner).Updates(map[string]any{"status": result.Status, "reason": result.Reason, "http_status": result.HTTPStatus, "next_attempt_at": next, "finished_at": finished, "lease_owner": "", "lease_until": nil, "updated_at": now})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		return tx.Model(&model.CheckinNotificationTarget{}).Where("id = ? AND lease_owner = ?", row.TargetID, owner).Updates(map[string]any{"lease_owner": "", "lease_until": nil}).Error
	})
}
