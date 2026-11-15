package checkin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var Default *Service

type Event struct {
	Kind string `json:"kind"`
}

type Service struct {
	Store           *op.CheckinStore
	Vault           *checkinsecret.Vault
	Now             func() time.Time
	gate            sync.RWMutex
	mu              sync.Mutex
	scanMu          sync.Mutex
	dispatchMu      sync.Mutex
	active          map[string]bool
	activeSites     map[string]bool
	nextSite        map[string]time.Time
	closed          bool
	paused          bool
	wake            chan struct{}
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	loopDone        chan struct{}
	started         bool
	eventMu         sync.Mutex
	watchers        map[chan Event]bool
	sessionMu       sync.Mutex
	pendingSessions map[string]pendingCheckinSession
}

func New(store *op.CheckinStore, vault *checkinsecret.Vault) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Store: store, Vault: vault, Now: func() time.Time { return time.Now().UTC() }, active: map[string]bool{}, activeSites: map[string]bool{}, nextSite: map[string]time.Time{}, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel, loopDone: make(chan struct{}), watchers: map[chan Event]bool{}}
}

// Access also covers HTTP mutations, so importing a database cannot race with
// credential edits, job creation, or an in-flight worker.
func (s *Service) Access(fn func() error) error {
	s.mu.Lock()
	unavailable := s.closed || s.paused
	s.mu.Unlock()
	if unavailable {
		return op.ErrCheckinBusy
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	s.mu.Lock()
	unavailable = s.closed || s.paused
	s.mu.Unlock()
	if unavailable {
		return op.ErrCheckinBusy
	}
	return fn()
}

func (s *Service) Exclusive(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	if s.closed || s.paused {
		s.mu.Unlock()
		return op.ErrCheckinBusy
	}
	s.paused = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.paused = false; s.mu.Unlock(); s.Notify("refresh") }()
	s.gate.Lock()
	defer s.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func (s *Service) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()
	go func() {
		defer close(s.loopDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			case <-s.wake:
			}
			for {
				item, settings, ok := s.take(s.ctx)
				if !ok {
					break
				}
				s.wg.Add(1)
				go func() { defer s.wg.Done(); s.work(s.ctx, item, settings) }()
			}
		}
	}()
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	started := s.started
	s.mu.Unlock()
	s.cancel()
	if started {
		<-s.loopDone
	}
	s.wg.Wait()
	s.eventMu.Lock()
	for watcher := range s.watchers {
		close(watcher)
		delete(s.watchers, watcher)
	}
	s.eventMu.Unlock()
	return nil
}

func (s *Service) Notify(kind string) {
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for watcher := range s.watchers {
		select {
		case watcher <- Event{Kind: kind}:
		default:
			close(watcher)
			delete(s.watchers, watcher)
		}
	}
}

func (s *Service) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 16)
	s.eventMu.Lock()
	s.watchers[ch] = true
	s.eventMu.Unlock()
	return ch, func() {
		s.eventMu.Lock()
		defer s.eventMu.Unlock()
		if s.watchers[ch] {
			delete(s.watchers, ch)
			close(ch)
		}
	}
}

func (s *Service) take(ctx context.Context) (model.CheckinJobItem, model.CheckinSettings, bool) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	s.gate.RLock()
	ok := false
	defer func() {
		if !ok {
			s.gate.RUnlock()
		}
	}()
	s.mu.Lock()
	unavailable := s.closed || s.paused
	s.mu.Unlock()
	if unavailable || ctx.Err() != nil {
		return model.CheckinJobItem{}, model.CheckinSettings{}, false
	}
	settings, err := s.Store.Settings(ctx)
	if err != nil {
		return model.CheckinJobItem{}, settings, false
	}
	s.mu.Lock()
	full := len(s.active) >= settings.Concurrency
	s.mu.Unlock()
	if full {
		return model.CheckinJobItem{}, settings, false
	}
	if err := s.Store.Recover(ctx, s.Now()); err != nil {
		return model.CheckinJobItem{}, settings, false
	}
	items, err := s.Store.Queue(ctx)
	if err != nil {
		return model.CheckinJobItem{}, settings, false
	}
	for _, item := range items {
		s.mu.Lock()
		busy := s.active[item.AccountID] || s.activeSites[item.SiteID] || s.Now().Before(s.nextSite[item.SiteID])
		s.mu.Unlock()
		if busy {
			continue
		}
		claimed, err := s.Store.Claim(ctx, &item, s.Now())
		if err != nil || !claimed {
			continue
		}
		s.mu.Lock()
		s.active[item.AccountID] = true
		s.activeSites[item.SiteID] = true
		s.mu.Unlock()
		ok = true
		return item, settings, true // worker releases the read lock
	}
	return model.CheckinJobItem{}, settings, false
}

func (s *Service) work(parent context.Context, item model.CheckinJobItem, settings model.CheckinSettings) {
	defer func() {
		if recover() != nil {
			log.Error("checkin worker interrupted unexpectedly")
		}
		s.mu.Lock()
		delete(s.active, item.AccountID)
		delete(s.activeSites, item.SiteID)
		s.nextSite[item.SiteID] = s.Now().Add(time.Duration(settings.SiteIntervalSeconds) * time.Second)
		s.mu.Unlock()
		s.gate.RUnlock()
		s.Notify("progress")
	}()
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	if err := s.execute(ctx, item, settings); err != nil {
		log.Warn("checkin state could not be persisted; its lease will be recovered", "job_id", item.JobID)
	}
}

// ProcessOne drives the same durable worker synchronously, useful to embedders
// and deterministic integration tests without starting background timers.
func (s *Service) ProcessOne(ctx context.Context) bool {
	item, settings, ok := s.take(ctx)
	if !ok {
		return false
	}
	s.work(ctx, item, settings)
	return true
}

type AccountWrite struct {
	Name              *string                   `json:"name"`
	Enabled           *bool                     `json:"enabled"`
	AutoCheckin       *bool                     `json:"auto_checkin"`
	ProxyMode         *string                   `json:"proxy_mode"`
	ProxyURL          *string                   `json:"proxy_url"`
	Revision          int64                     `json:"revision"`
	CredentialVersion int64                     `json:"credential_version"`
	Credentials       *model.CheckinCredentials `json:"credentials"`
}

func (s *Service) SaveAccount(ctx context.Context, siteID, id string, input AccountWrite) (model.CheckinAccount, error) {
	account := model.CheckinAccount{ID: uuid.NewString(), SiteID: siteID, Enabled: true, ProxyMode: "inherit"}
	if id != "" {
		var err error
		account, err = s.Store.Account(ctx, id)
		if err != nil {
			return account, err
		}
		if input.Revision <= 0 {
			return account, op.ErrCheckinConflict
		}
	}
	if input.Name != nil {
		account.Name = *input.Name
	}
	if input.Enabled != nil {
		account.Enabled = *input.Enabled
	}
	if input.AutoCheckin != nil {
		account.AutoCheckin = *input.AutoCheckin
	}
	if input.ProxyMode != nil {
		account.ProxyMode = *input.ProxyMode
	}
	if input.ProxyURL != nil {
		account.ProxyURL = *input.ProxyURL
	}
	if input.Credentials != nil {
		site, err := s.Store.Site(ctx, account.SiteID)
		if err != nil {
			return account, err
		}
		credentials := *input.Credentials
		if site.Config.Adapter == "sub2api" && credentials.Type == "bearer" {
			normalizeSub2APICredentials(&credentials)
		} else if credentials.RefreshToken != "" {
			return account, model.InvalidCheckin("refresh tokens require a Sub2API Bearer account")
		}
		input.Credentials = &credentials
		if err := model.ValidateCheckinCredentials(*input.Credentials); err != nil {
			return account, err
		}
		body, _ := json.Marshal(input.Credentials)
		encrypted, err := s.Vault.Seal(account.ID, body)
		if err != nil {
			return account, err
		}
		account.CredentialCipher, account.CredentialType = encrypted, input.Credentials.Type
		account.CredentialUserID = input.Credentials.UserID
		account.HasRefreshToken = credentials.RefreshToken != ""
		account.TokenExpiresAt = credentials.ExpiresAt
		account.TokenRefreshedAt, account.RefreshRetryAt = nil, nil
		account.RefreshPending, account.RefreshError = false, ""
	}
	saved, err := s.Store.SaveAccount(ctx, account, input.Revision, input.CredentialVersion, input.Credentials != nil)
	if err == nil && input.Credentials != nil {
		s.sessionMu.Lock()
		delete(s.pendingSessions, account.ID)
		s.sessionMu.Unlock()
	}
	return saved, err
}

func (s *Service) Enqueue(ctx context.Context, scope model.CheckinScope, source, key string) (model.CheckinJob, error) {
	if err := op.NormalizeCheckinScope(&scope); err != nil {
		return model.CheckinJob{}, err
	}
	accounts, err := s.Store.SelectAccounts(ctx, scope)
	if err != nil {
		return model.CheckinJob{}, err
	}
	items := make([]model.CheckinJobItem, 0, len(accounts))
	for _, account := range accounts {
		site, err := s.Store.Site(ctx, account.SiteID)
		if err != nil {
			return model.CheckinJob{}, err
		}
		items = append(items, model.CheckinJobItem{AccountID: account.ID, SiteID: site.ID, SiteName: site.Config.Name, AccountName: account.Name, CheckinDate: businessDate(site.Config, s.Now())})
	}
	job, err := s.Store.Enqueue(ctx, scope, source, key, items)
	if err == nil {
		s.Notify("job")
	}
	return job, err
}

func (s *Service) Confirm(ctx context.Context, runID, key string, automatic bool) (model.CheckinJob, error) {
	run, err := s.Store.Run(ctx, runID)
	if err != nil {
		return model.CheckinJob{}, err
	}
	account, err := s.Store.Account(ctx, run.AccountID)
	if err != nil {
		return model.CheckinJob{}, err
	}
	site, err := s.Store.Site(ctx, account.SiteID)
	if err != nil {
		return model.CheckinJob{}, err
	}
	if !account.Enabled || !site.Config.Enabled {
		return model.CheckinJob{}, model.InvalidCheckin("site or account is disabled")
	}
	if site.Config.Status == nil {
		return model.CheckinJob{}, model.InvalidCheckin("configure a status query before confirming this run")
	}
	source := "confirmation"
	if automatic {
		source = "confirmation_auto"
	}
	job, err := s.Store.Enqueue(ctx, model.CheckinScope{Kind: "confirmation", IDs: []string{run.ID}}, source, key, []model.CheckinJobItem{{SiteID: run.SiteID, AccountID: run.AccountID, SiteName: run.SiteName, AccountName: run.AccountName, CheckinDate: run.CheckinDate, RunID: run.ID, ConfirmOnly: true}})
	if err == nil {
		s.Notify("job")
	}
	return job, err
}

func fingerprint(site model.CheckinSite, account model.CheckinAccount) string {
	// The opaque ciphertext is never included in logs or returned to clients.
	data, _ := json.Marshal([]any{site.Config, account.ID, account.IdentityVersion(), account.ProxyMode, account.ProxyURL})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *Service) Scan(ctx context.Context) error {
	if !s.scanMu.TryLock() {
		return nil
	}
	defer s.scanMu.Unlock()
	return s.Access(func() error {
		now := s.Now()
		if err := s.Store.ExpireConfirmations(ctx, now); err != nil {
			return err
		}
		var pending []model.CheckinRun
		if err := s.Store.DB.WithContext(ctx).Where("status = ? AND submit_intent_at IS NOT NULL AND next_confirm_at <= ?", "pending", now).Limit(1000).Find(&pending).Error; err != nil {
			return err
		}
		for _, run := range pending {
			busy, err := s.Store.HasQueuedAccount(ctx, run.AccountID)
			if err != nil {
				return err
			}
			if busy {
				continue
			}
			_, _ = s.Confirm(ctx, run.ID, uuid.NewString(), true)
		}
		accounts, err := s.Store.Accounts(ctx, "")
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if !account.Enabled || !account.AutoCheckin {
				continue
			}
			site, err := s.Store.Site(ctx, account.SiteID)
			if err != nil || !site.Config.Enabled || (site.Support == "unsupported" && site.SupportRevision == site.Revision) {
				continue
			}
			busy, err := s.Store.HasQueuedAccount(ctx, account.ID)
			if err != nil {
				return err
			}
			if busy {
				continue
			}
			var today model.CheckinRun
			err = s.Store.DB.WithContext(ctx).First(&today, "account_id = ? AND checkin_date = ?", account.ID, businessDate(site.Config, now)).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				if today.Status == "manual_required" || (today.SubmitIntentAt != nil && !today.SafeToRetry && today.Status != "success" && today.Status != "already_checked_in") {
					continue
				}
				if today.Status == "pending" {
					// A precheck that never submitted may resume inside its window.
					// Submitted runs are handled only by the confirmation loop above.
					if today.NextConfirmAt != nil && !now.Before(*today.NextConfirmAt) && inWindow(site.Config, now) {
						if _, err := s.Enqueue(ctx, model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}, OnlyAuto: true}, "scheduled", uuid.NewString()); err != nil && !errors.Is(err, op.ErrCheckinBusy) {
							return err
						}
					}
					continue
				}
			}
			if account.NextRunAt == nil || (!account.NextRunAt.After(now) && (!inWindow(site.Config, now) || account.LastSuccessDate >= businessDate(site.Config, now))) {
				next := nextSchedule(site.Config, account, now, nil)
				if err := s.Store.Schedule(ctx, account, next); err != nil {
					return err
				}
				account.NextRunAt = &next
			}
			if account.NextRunAt != nil && !account.NextRunAt.After(now) && inWindow(site.Config, now) {
				if _, err := s.Enqueue(ctx, model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}, OnlyAuto: true}, "scheduled", uuid.NewString()); err != nil && !errors.Is(err, op.ErrCheckinBusy) {
					return err
				}
			}
		}
		return nil
	})
}
