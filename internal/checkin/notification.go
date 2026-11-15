package checkin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/charmbracelet/log"
	"github.com/google/uuid"
)

type NotificationWrite struct {
	Config     model.CheckinNotificationConfig `json:"config"`
	Revision   int64                           `json:"revision"`
	Connection *notify.Connection              `json:"connection"`
}

func (s *Service) SaveNotification(ctx context.Context, id string, input NotificationWrite) (model.CheckinNotificationTarget, error) {
	row := model.CheckinNotificationTarget{ID: id, Config: input.Config}
	if id == "" {
		if input.Revision != 0 {
			return row, op.ErrCheckinConflict
		}
		row.ID = uuid.NewString()
	} else if input.Revision <= 0 {
		return row, op.ErrCheckinConflict
	}
	if err := notify.NormalizeConfig(&row.Config); err != nil {
		return row, err
	}
	if input.Connection != nil {
		if err := notify.NormalizeConnection(row.Config.Kind, input.Connection); err != nil {
			return row, err
		}
		plain, err := json.Marshal(input.Connection)
		if err != nil {
			return row, err
		}
		row.ConnectionCipher, err = s.Vault.Seal("notification:"+row.ID, plain)
		if err != nil {
			return row, err
		}
	}
	saved, err := s.Store.SaveNotificationTarget(ctx, row, input.Revision, input.Connection != nil, s.Now())
	if err == nil {
		s.Notify("notification")
	}
	return saved, err
}

func (s *Service) enqueueNotification(ctx context.Context, event model.CheckinNotificationEvent) {
	if err := s.Store.EnqueueNotifications(ctx, event, s.Now()); err != nil {
		log.Warn("notification could not be queued; the saved checkin result is unchanged")
	}
}

func (s *Service) notifyRun(ctx context.Context, run model.CheckinRun, source string) {
	kind := run.Status
	switch kind {
	case "success", "already_checked_in", "failed":
	case "pending", "manual_required":
		kind = "attention"
	default:
		return
	}
	event := model.CheckinNotificationEvent{Kind: kind, Identity: run.ID, SiteID: run.SiteID, AccountID: run.AccountID, SiteName: run.SiteName, AccountName: run.AccountName, CheckinDate: run.CheckinDate, Timezone: run.Timezone, Status: run.Status, Source: source, OccurredAt: s.Now()}
	if run.RewardState == "known" {
		event.RewardMicros, event.RewardUnit = run.RewardMicros, run.RewardUnit
	}
	s.enqueueNotification(ctx, event)
}

func (s *Service) notifyBalance(ctx context.Context, site model.CheckinSite, account model.CheckinAccount) {
	inspection, balance := account.Inspection, account.Balance
	if inspection == nil || inspection.FinishedAt == nil || !inspection.BalanceUpdated || balance == nil || balance.AmountState != "known" || balance.AmountMicros == nil {
		return
	}
	event := model.CheckinNotificationEvent{Kind: "low_balance", Identity: inspection.ID, SiteID: site.ID, AccountID: account.ID, SiteName: site.Config.Name, AccountName: account.Name, CheckinDate: inspection.CheckinDate, Timezone: inspection.Timezone, Status: "low_balance", Source: "manual", BalanceMicros: balance.AmountMicros, BalanceUnit: balance.Unit, BalanceAt: &balance.UpdatedAt, OccurredAt: s.Now()}
	s.enqueueNotification(ctx, event)
}

func (s *Service) takeNotification(ctx context.Context) (model.CheckinNotificationDelivery, model.CheckinNotificationTarget, bool) {
	s.gate.RLock()
	claimed := false
	defer func() {
		if !claimed {
			s.gate.RUnlock()
		}
	}()
	s.notificationDispatch.Lock()
	defer s.notificationDispatch.Unlock()
	s.mu.Lock()
	unavailable := s.closed || s.paused || s.activeNotifications >= 2
	s.mu.Unlock()
	if unavailable || ctx.Err() != nil {
		return model.CheckinNotificationDelivery{}, model.CheckinNotificationTarget{}, false
	}
	row, target, ok, err := s.Store.ClaimNotification(ctx, s.Now())
	if err != nil || !ok {
		return row, target, false
	}
	s.mu.Lock()
	s.activeNotifications++
	s.mu.Unlock()
	claimed = true
	return row, target, true // notification worker releases the backup gate
}

func (s *Service) sendNotification(parent context.Context, row model.CheckinNotificationDelivery, target model.CheckinNotificationTarget) {
	result := notify.Result{Status: "uncertain", Reason: "delivery_interrupted"}
	defer func() {
		if recover() != nil {
			log.Warn("notification worker interrupted unexpectedly")
		}
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		if err := s.Store.FinishNotification(saveCtx, row, result, s.Now()); err != nil {
			log.Warn("notification result could not be persisted; delivery will require review")
		}
		cancel()
		s.mu.Lock()
		s.activeNotifications--
		s.mu.Unlock()
		s.gate.RUnlock()
		s.Notify("notification")
	}()
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	plain, err := s.Vault.Unseal("notification:"+target.ID, target.ConnectionCipher)
	if err != nil {
		result = notify.Result{Status: "failed", Reason: "connection_unavailable"}
		return
	}
	var connection notify.Connection
	if json.Unmarshal(plain, &connection) != nil {
		result = notify.Result{Status: "failed", Reason: "invalid_connection"}
		return
	}
	resolved, err := op.ResolveProxy(ctx, s.Store.DB, target.Config.ProxySelection())
	if err != nil {
		result = notify.Result{Status: "failed", Reason: "invalid_proxy"}
		return
	}
	client, err := newHTTPClient(resolved.URL, 20*time.Second)
	if err != nil {
		result = notify.Result{Status: "failed", Reason: "invalid_proxy"}
		return
	}
	defer client.CloseIdleConnections()
	result = notify.Deliver(ctx, client, target.Config.Kind, connection, row.Title, row.Message, notify.DeliveryValues(row, target.Config.Language), s.Now())
}

// ProcessNotification uses the durable worker without a background timer.
func (s *Service) ProcessNotification(ctx context.Context) bool {
	row, target, ok := s.takeNotification(ctx)
	if !ok {
		return false
	}
	s.sendNotification(ctx, row, target)
	return true
}
