package sitesync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

type checkinNotificationConfig struct {
	enabled   bool
	webhook   string
	cooldown  time.Duration
	threshold float64
}

// Payloads contain only the persisted outcome and display names. Credentials,
// site URLs, configured HTTP headers and raw upstream responses are excluded.
type checkinNotification struct {
	Event        string                    `json:"event"`
	LogID        int64                     `json:"log_id,string"`
	SiteID       int                       `json:"site_id"`
	AccountID    int                       `json:"account_id"`
	SiteName     string                    `json:"site_name"`
	AccountName  string                    `json:"account_name"`
	Status       model.SiteExecutionStatus `json:"status"`
	Reason       string                    `json:"reason"`
	Message      string                    `json:"message"`
	FailureCount int                       `json:"failure_count"`
	Balance      *float64                  `json:"balance,omitempty"`
	Threshold    *float64                  `json:"threshold,omitempty"`
	OccurredAt   time.Time                 `json:"occurred_at"`
}

type checkinNotificationKey struct {
	destination [32]byte
	accountID   int
	event       string
	reason      string
}

type checkinNotificationReservation struct {
	inFlight bool
	until    time.Time
}

type checkinNotifier struct {
	config  func() checkinNotificationConfig
	client  *http.Client
	mu      sync.Mutex
	recent  map[checkinNotificationKey]checkinNotificationReservation
	slots   chan struct{}
	workers chan struct{}
	wg      sync.WaitGroup
}

var checkinNotifications = newCheckinNotifier(loadCheckinNotificationConfig)

func newCheckinNotifier(config func() checkinNotificationConfig) *checkinNotifier {
	return &checkinNotifier{
		config: config,
		client: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		recent: make(map[checkinNotificationKey]checkinNotificationReservation),
		slots:  make(chan struct{}, 128), workers: make(chan struct{}, 4),
	}
}

func loadCheckinNotificationConfig() checkinNotificationConfig {
	enabled, _ := op.SettingGetBool(model.SettingKeyCheckinNotifyEnabled)
	webhook, _ := op.SettingGetString(model.SettingKeyCheckinNotifyWebhookURL)
	urlSetting := model.Setting{Key: model.SettingKeyCheckinNotifyWebhookURL, Value: webhook}
	if urlSetting.Validate() != nil || urlSetting.Value == "" {
		enabled = false
	}
	seconds, err := op.SettingGetInt(model.SettingKeyCheckinNotifyCooldownSeconds)
	if err != nil || seconds < 0 || seconds > 7*24*60*60 {
		seconds = 3600
	}
	raw, _ := op.SettingGetString(model.SettingKeyCheckinLowBalanceThreshold)
	threshold, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 {
		threshold = 0
	}
	return checkinNotificationConfig{enabled: enabled, webhook: urlSetting.Value, cooldown: time.Duration(seconds) * time.Second, threshold: threshold}
}

// Called only after a scheduled execution has committed. Delivery is bounded
// and asynchronous, so webhook latency or failure never changes its outcome.
func (n *checkinNotifier) notify(site *model.Site, account *model.SiteAccount, result *model.SiteCheckinResult, balance siteBalanceFetchResult) {
	config := n.config()
	if !config.enabled || config.webhook == "" || result == nil || result.LogID == 0 {
		return
	}
	event := checkinNotification{
		LogID: result.LogID, SiteID: site.ID, AccountID: account.ID,
		SiteName: site.Name, AccountName: account.Name, Status: result.Status,
		Reason: result.Reason, Message: result.Message, FailureCount: account.CheckinFailureCount,
		OccurredAt: time.Now().UTC(),
	}
	if result.Status == model.SiteExecutionStatusFailed {
		event.Event = "site_checkin_failed"
	} else if result.Status == model.SiteExecutionStatusSuccess && balance.ok && config.threshold > 0 && balance.balance < config.threshold {
		event.Event, event.Reason = "site_checkin_low_balance", "low_balance"
		event.Balance, event.Threshold = &balance.balance, &config.threshold
	} else {
		return
	}
	n.enqueue(config, event)
}

func (n *checkinNotifier) enqueue(config checkinNotificationConfig, event checkinNotification) bool {
	key := checkinNotificationKey{sha256.Sum256([]byte(config.webhook)), event.AccountID, event.Event, event.Reason}
	now := time.Now()
	n.mu.Lock()
	for key, item := range n.recent {
		if !item.inFlight && !now.Before(item.until) {
			delete(n.recent, key)
		}
	}
	if _, exists := n.recent[key]; exists {
		n.mu.Unlock()
		return false
	}
	// Bound both outstanding work and cooldown state on large installations.
	if len(n.recent) >= 10000 {
		n.mu.Unlock()
		log.Warnf("checkin notification capacity reached for account %d", event.AccountID)
		return false
	}
	select {
	case n.slots <- struct{}{}:
	default:
		n.mu.Unlock()
		log.Warnf("checkin notification queue full for account %d", event.AccountID)
		return false
	}
	n.recent[key] = checkinNotificationReservation{inFlight: true}
	n.mu.Unlock()
	n.wg.Add(1)
	safe.Go("site-checkin-notification", func() {
		defer n.wg.Done()
		delivered := false
		defer func() {
			n.mu.Lock()
			if delivered && config.cooldown > 0 {
				n.recent[key] = checkinNotificationReservation{until: time.Now().Add(config.cooldown)}
			} else {
				delete(n.recent, key)
			}
			n.mu.Unlock()
			<-n.slots
		}()
		n.workers <- struct{}{}
		defer func() { <-n.workers }()
		if err := n.deliver(config.webhook, event); err != nil {
			// HTTP errors may embed a URL containing a webhook secret. Only log
			// the stable event identity, never the URL, body or transport error.
			log.Warnf("checkin notification delivery failed for account %d, event %s", event.AccountID, event.Event)
			return
		}
		delivered = true
	})
	return true
}

func (n *checkinNotifier) deliver(webhook string, event checkinNotification) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("notification HTTP status %d", response.StatusCode)
	}
	return nil
}
