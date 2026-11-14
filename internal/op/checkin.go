package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCheckinConflict = errors.New("checkin data changed or is being used; reload and try again")
var ErrCheckinBusy = errors.New("checkin queue is full or temporarily paused")

const CheckinSettingKey model.SettingKey = "checkin_settings"

type CheckinStore struct {
	DB      *gorm.DB
	writeMu sync.Mutex
}

func (s *CheckinStore) Sites(ctx context.Context) ([]model.CheckinSite, error) {
	rows := []model.CheckinSite{}
	err := s.DB.WithContext(ctx).Where("deleted_at IS NULL").Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) Site(ctx context.Context, id string) (model.CheckinSite, error) {
	var row model.CheckinSite
	err := s.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&row).Error
	return row, err
}

func (s *CheckinStore) SaveSite(ctx context.Context, id string, revision int64, config model.CheckinSiteConfig) (model.CheckinSite, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := model.NormalizeCheckinSite(&config); err != nil {
		return model.CheckinSite{}, err
	}
	row := model.CheckinSite{ID: id, Config: config, Revision: revision + 1, Support: "unknown"}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == "" {
			row.ID = uuid.NewString()
			row.Revision = 1
			return tx.Create(&row).Error
		}
		var busy int64
		if err := tx.Model(&model.CheckinAccount{}).Where("site_id = ? AND lease_until > ?", id, time.Now().UTC()).Count(&busy).Error; err != nil {
			return err
		}
		if busy > 0 {
			return ErrCheckinConflict
		}
		result := tx.Model(&model.CheckinSite{}).Where("id = ? AND revision = ? AND deleted_at IS NULL", id, revision).Select("Config", "Revision", "Support", "SupportRevision", "VerifiedAt", "UpdatedAt").Updates(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCheckinConflict
		}
		return tx.Model(&model.CheckinAccount{}).Where("site_id = ?", id).Update("next_run_at", nil).Error
	})
	if err != nil {
		return row, err
	}
	return s.Site(ctx, row.ID)
}

func (s *CheckinStore) Accounts(ctx context.Context, siteID string) ([]model.CheckinAccount, error) {
	rows := []model.CheckinAccount{}
	q := s.DB.WithContext(ctx).Where("deleted_at IS NULL")
	if siteID != "" {
		q = q.Where("site_id = ?", siteID)
	}
	err := q.Order("created_at, id").Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) Account(ctx context.Context, id string) (model.CheckinAccount, error) {
	var row model.CheckinAccount
	err := s.DB.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&row).Error
	return row, err
}

func (s *CheckinStore) SaveAccount(ctx context.Context, row model.CheckinAccount, expectedRevision, expectedCredentials int64, replaceCredentials bool) (model.CheckinAccount, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	row.Name = strings.TrimSpace(row.Name)
	if row.Name == "" || len(row.Name) > 128 {
		return row, model.InvalidCheckin("account name is required (maximum 128 bytes)")
	}
	if row.ProxyMode == "" {
		row.ProxyMode = "inherit"
	}
	if row.ProxyMode != "inherit" && row.ProxyMode != "direct" && row.ProxyMode != "custom" {
		return row, model.InvalidCheckin("invalid proxy mode")
	}
	if row.ProxyMode == "custom" {
		if err := model.ValidateCheckinURL(row.ProxyURL, true); err != nil {
			return row, err
		}
	} else {
		row.ProxyURL = ""
	}
	if _, err := s.Site(ctx, row.SiteID); err != nil {
		return row, err
	}
	row.Revision = expectedRevision + 1
	if expectedRevision == 0 {
		if row.CredentialCipher == "" {
			return row, model.InvalidCheckin("account credentials are required")
		}
		row.CredentialVersion = 1
		if err := s.DB.WithContext(ctx).Create(&row).Error; err != nil {
			return row, err
		}
		return row, nil
	}
	columns := []string{"Name", "Enabled", "AutoCheckin", "ProxyMode", "ProxyURL", "Revision", "NextRunAt", "UpdatedAt"}
	q := s.DB.WithContext(ctx).Model(&model.CheckinAccount{}).Where("id = ? AND revision = ? AND deleted_at IS NULL AND (lease_until IS NULL OR lease_until <= ?)", row.ID, expectedRevision, time.Now().UTC())
	if replaceCredentials {
		q = q.Where("credential_version = ?", expectedCredentials)
		row.CredentialVersion = expectedCredentials + 1
		columns = append(columns, "CredentialCipher", "CredentialType", "CredentialUserID", "CredentialVersion")
	}
	row.NextRunAt = nil
	result := q.Select(columns).Updates(&row)
	if result.Error != nil {
		return row, result.Error
	}
	if result.RowsAffected != 1 {
		return row, ErrCheckinConflict
	}
	return s.Account(ctx, row.ID)
}

func (s *CheckinStore) Delete(ctx context.Context, siteID, accountID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&model.CheckinAccount{}).Where("deleted_at IS NULL")
		if accountID != "" {
			q = q.Where("id = ?", accountID)
		} else {
			q = q.Where("site_id = ?", siteID)
		}
		var busy int64
		if err := q.Session(&gorm.Session{}).Where("lease_until > ?", time.Now().UTC()).Count(&busy).Error; err != nil {
			return err
		}
		if busy > 0 {
			return ErrCheckinConflict
		}
		now := time.Now().UTC()
		if err := q.Updates(map[string]any{"deleted_at": now, "enabled": false, "auto_checkin": false, "credential_cipher": "", "next_run_at": nil}).Error; err != nil {
			return err
		}
		if siteID != "" {
			return tx.Model(&model.CheckinSite{}).Where("id = ? AND deleted_at IS NULL", siteID).Update("deleted_at", now).Error
		}
		return nil
	})
}

func (s *CheckinStore) Settings(ctx context.Context) (model.CheckinSettings, error) {
	result := model.DefaultCheckinSettings()
	var row model.Setting
	err := s.DB.WithContext(ctx).First(&row, "key = ?", CheckinSettingKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(row.Value), &result); err != nil {
		return result, err
	}
	return result, result.Validate()
}

func (s *CheckinStore) SaveSettings(ctx context.Context, settings model.CheckinSettings) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := settings.Validate(); err != nil {
		return err
	}
	value, _ := json.Marshal(settings)
	row := model.Setting{Key: CheckinSettingKey, Value: string(value)}
	if err := s.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&row).Error; err != nil {
		return err
	}
	settingCache.Set(CheckinSettingKey, row.Value)
	return nil
}

func NormalizeCheckinScope(scope *model.CheckinScope) error {
	scope.IDs = slices.Clone(scope.IDs)
	slices.Sort(scope.IDs)
	scope.IDs = slices.Compact(scope.IDs)
	if scope.Kind == "all" {
		if len(scope.IDs) != 0 {
			return model.InvalidCheckin("all scope must not contain IDs")
		}
		return nil
	}
	if scope.Kind != "sites" && scope.Kind != "accounts" {
		return model.InvalidCheckin("scope must be all, sites or accounts")
	}
	if len(scope.IDs) == 0 || len(scope.IDs) > 1000 {
		return model.InvalidCheckin("select between 1 and 1000 IDs")
	}
	for _, id := range scope.IDs {
		if _, err := uuid.Parse(id); err != nil {
			return model.InvalidCheckin("invalid selection ID")
		}
	}
	return nil
}

func (s *CheckinStore) SelectAccounts(ctx context.Context, scope model.CheckinScope) ([]model.CheckinAccount, error) {
	if err := NormalizeCheckinScope(&scope); err != nil {
		return nil, err
	}
	if scope.Kind != "all" {
		var count int64
		q := s.DB.WithContext(ctx).Model(&model.CheckinAccount{})
		if scope.Kind == "sites" {
			q = s.DB.WithContext(ctx).Model(&model.CheckinSite{})
		}
		if err := q.Where("id IN ? AND deleted_at IS NULL", scope.IDs).Count(&count).Error; err != nil {
			return nil, err
		}
		if int(count) != len(scope.IDs) {
			return nil, model.InvalidCheckin("selection contains unavailable entries")
		}
	}
	accounts, err := s.Accounts(ctx, "")
	if err != nil {
		return nil, err
	}
	sites, err := s.Sites(ctx)
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, site := range sites {
		enabled[site.ID] = site.Config.Enabled
	}
	result := []model.CheckinAccount{}
	for _, account := range accounts {
		if !account.Enabled || !enabled[account.SiteID] || (scope.OnlyAuto && !account.AutoCheckin) {
			continue
		}
		if scope.Kind == "sites" && !slices.Contains(scope.IDs, account.SiteID) || scope.Kind == "accounts" && !slices.Contains(scope.IDs, account.ID) {
			continue
		}
		result = append(result, account)
	}
	if len(result) > 1000 {
		return nil, model.InvalidCheckin("a job can contain at most 1000 accounts")
	}
	return result, nil
}

func (s *CheckinStore) Enqueue(ctx context.Context, scope model.CheckinScope, source, key string, items []model.CheckinJobItem) (model.CheckinJob, error) {
	// Serialize queue admission and scope reuse within the application's worker.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if key == "" {
		key = uuid.NewString()
	}
	if len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
		return model.CheckinJob{}, model.InvalidCheckin("invalid idempotency key")
	}
	payload, _ := json.Marshal(scope)
	digest := sha256.Sum256(append([]byte(source), payload...))
	hash := hex.EncodeToString(digest[:])
	job := model.CheckinJob{ID: uuid.NewString(), IdempotencyKey: key, ScopeHash: hash, Scope: scope, Source: source, Status: "queued", Total: len(items)}
	if len(items) == 0 {
		job.Status = "completed"
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.CheckinJob
		err := tx.Where("idempotency_key = ?", key).First(&existing).Error
		if err == nil {
			if existing.ScopeHash != hash {
				return ErrCheckinConflict
			}
			job = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if source == "manual" {
			err = tx.Where("scope_hash = ? AND status IN ?", hash, []string{"queued", "running"}).First(&existing).Error
			if err == nil {
				job = existing
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		var queued int64
		if err := tx.Model(&model.CheckinJobItem{}).Where("state IN ?", []string{"queued", "running"}).Count(&queued).Error; err != nil {
			return err
		}
		if queued+int64(len(items)) > 2000 {
			return ErrCheckinBusy
		}
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].ID = uuid.NewString()
			items[i].JobID = job.ID
			items[i].State = "queued"
		}
		if len(items) > 0 {
			return tx.CreateInBatches(&items, 100).Error
		}
		return nil
	})
	return job, err
}

func (s *CheckinStore) Jobs(ctx context.Context) ([]model.CheckinJob, error) {
	rows := []model.CheckinJob{}
	err := s.DB.WithContext(ctx).Order("created_at DESC, id DESC").Limit(50).Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) Job(ctx context.Context, id string) (model.CheckinJob, []model.CheckinJobItem, error) {
	var job model.CheckinJob
	items := []model.CheckinJobItem{}
	if err := s.DB.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		return job, items, err
	}
	err := s.DB.WithContext(ctx).Where("job_id = ?", id).Order("created_at, id").Find(&items).Error
	return job, items, err
}

func (s *CheckinStore) CancelJob(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.CheckinJob{}).Where("id = ? AND status IN ?", id, []string{"queued", "running"}).Update("status", "canceled").Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CheckinJobItem{}).Where("job_id = ? AND state = ?", id, "queued").Updates(map[string]any{"state": "done", "outcome": "skipped"}).Error; err != nil {
			return err
		}
		return reconcileCheckinJob(tx, id)
	})
}

type CheckinRunFilter struct {
	SiteID, AccountID, Status, Before, From, Until string
	Limit                                          int
}

func (s *CheckinStore) Runs(ctx context.Context, f CheckinRunFilter) ([]model.CheckinRun, error) {
	rows := []model.CheckinRun{}
	q := s.DB.WithContext(ctx)
	if f.SiteID != "" {
		q = q.Where("site_id = ?", f.SiteID)
	}
	if f.AccountID != "" {
		q = q.Where("account_id = ?", f.AccountID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.From != "" {
		q = q.Where("checkin_date >= ?", f.From)
	}
	if f.Until != "" {
		q = q.Where("checkin_date <= ?", f.Until)
	}
	if f.Before != "" {
		var cursor model.CheckinRun
		if err := s.DB.WithContext(ctx).First(&cursor, "id = ?", f.Before).Error; err != nil {
			return nil, err
		}
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	err := q.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) Run(ctx context.Context, id string) (model.CheckinRun, error) {
	var row model.CheckinRun
	err := s.DB.WithContext(ctx).First(&row, "id = ?", id).Error
	return row, err
}

func (s *CheckinStore) Attempts(ctx context.Context, runID string) ([]model.CheckinAttempt, error) {
	rows := []model.CheckinAttempt{}
	err := s.DB.WithContext(ctx).Where("run_id = ?", runID).Order("created_at DESC, id DESC").Limit(100).Find(&rows).Error
	return rows, err
}

func (s *CheckinStore) String() string {
	return fmt.Sprintf("checkin store (%s)", s.DB.Dialector.Name())
}
