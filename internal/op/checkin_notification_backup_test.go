package op

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/google/uuid"
)

func TestNotificationProxyReferenceProtectsDeletion(t *testing.T) {
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	conn := proxyTestDB(t)
	ctx := context.Background()
	node, err := ProxySave(ctx, conn, 0, model.ProxyWrite{Name: "Notification proxy", URL: "http://127.0.0.1:7890", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	store := NewCheckinStore(conn)
	target := backupNotification(t, store, model.CheckinNotificationConfig{Name: "Proxied destination", Kind: "webhook", Events: []string{"failed"}, Scope: "all", ProxyMode: "pool", ProxyConfigID: &node.ID})
	rows, err := ProxyList(ctx, conn)
	if err != nil || len(rows) != 1 || rows[0].ReferenceCount != 1 {
		t.Fatal("notification binding was not counted", err)
	}
	if err := ProxyDelete(ctx, conn, node.ID); !errors.Is(err, ErrProxyConflict) {
		t.Fatal("deleted a proxy referenced by a notification", err)
	}
	if err := store.DeleteNotificationTarget(ctx, target.ID, target.Revision, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ProxyDelete(ctx, conn, node.ID); err != nil {
		t.Fatal("deleted notification retained a proxy binding", err)
	}
}

func backupNotification(t *testing.T, store *CheckinStore, config model.CheckinNotificationConfig) model.CheckinNotificationTarget {
	t.Helper()
	vault, err := checkinsecret.Open()
	if err != nil {
		t.Fatal(err)
	}
	row := model.CheckinNotificationTarget{ID: uuid.NewString(), Config: config}
	row.ConnectionCipher, err = vault.Seal("notification:"+row.ID, []byte(`{"url":"https://example.invalid/private-notify-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	row, err = store.SaveNotificationTarget(context.Background(), row, 0, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func notificationBackupFixture(t *testing.T) (*CheckinStore, []byte) {
	t.Helper()
	store, base := checkinBackupFixture(t)
	config := model.CheckinNotificationConfig{Name: "Backup notifications", Kind: "webhook", Enabled: true, Events: []string{"attention"}, IncludeManual: true, Scope: "sites", SiteIDs: []string{base.Sites[0].ID}}
	target := backupNotification(t, store, config)
	ctx, now := context.Background(), time.Now().UTC()
	if _, err := store.TestNotification(ctx, target.ID, target.Revision, now); err != nil {
		t.Fatal(err)
	}
	row, _, ok, err := store.ClaimNotification(ctx, now)
	if err != nil || !ok {
		t.Fatal("claim failed", err)
	}
	if err := store.FinishNotification(ctx, row, notify.Result{Status: "sent", Reason: "accepted", HTTPStatus: 204}, now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Second)
	if _, err := store.TestNotification(ctx, target.ID, target.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := store.ClaimNotification(ctx, now); err != nil || !ok {
		t.Fatal("second claim failed", err)
	}
	run := base.Runs[0]
	event := model.CheckinNotificationEvent{Identity: run.ID, Kind: "attention", Status: "pending", Source: "manual", SiteID: run.SiteID, AccountID: run.AccountID, SiteName: run.SiteName, AccountName: run.AccountName, CheckinDate: run.CheckinDate, Timezone: run.Timezone, OccurredAt: now}
	if err := store.EnqueueNotifications(ctx, event, now); err != nil {
		t.Fatal(err)
	}
	dump, err := exportCheckin(ctx, store.DB)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(dump)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-notify-token") || strings.Contains(string(raw), "lease_owner") || !strings.Contains(string(raw), "connection_cipher") {
		t.Fatal("notification backup leaked a connection or omitted encryption")
	}
	return store, raw
}

func TestNotificationBackupDisablesNewTargetsAndDoesNotReplay(t *testing.T) {
	_, raw := notificationBackupFixture(t)
	var dump model.CheckinDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	conn := checkinTestDB(t)
	restored := restoreCheckinFixture(t, conn, &dump)
	if restored.RowsAffected["checkin_notification_targets"] != 1 || restored.RowsAffected["checkin_notification_deliveries"] != 3 {
		t.Fatal("notification backup records missing")
	}
	store := NewCheckinStore(conn)
	target, err := store.NotificationTarget(context.Background(), dump.NotificationTargets[0].ID)
	if err != nil || target.Config.Enabled || !target.HasConnection || target.LeaseUntil != nil {
		t.Fatal("restored destination was enabled or lost its connection", err)
	}
	rows, _ := store.NotificationDeliveries(context.Background(), NotificationDeliveryFilter{})
	sent, skipped := 0, 0
	for _, row := range rows {
		if row.Status == "sent" {
			sent++
		}
		if row.Status == "skipped" && row.Reason == "restored_delivery" {
			skipped++
		}
		if row.LeaseOwner != "" || row.LeaseUntil != nil || row.NextAttemptAt != nil {
			t.Fatal("restored process state")
		}
	}
	if sent != 1 || skipped != 2 {
		t.Fatal("backup lost history or retained queued deliveries")
	}
	if _, _, ok, err := store.ClaimNotification(context.Background(), time.Now().Add(time.Hour)); ok || err != nil {
		t.Fatal("backup replayed a notification", err)
	}
	// Importing the old snapshot again must preserve local edits and secrets.
	target.Config.Name, target.Config.Enabled = "Local destination", true
	vault, _ := checkinsecret.Open()
	target.ConnectionCipher, _ = vault.Seal("notification:"+target.ID, []byte(`{"url":"https://example.invalid/local-connection"}`))
	updated, err := store.SaveNotificationTarget(context.Background(), target, target.Revision, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &dump)
	restored = restoreCheckinFixture(t, conn, &dump)
	current, _ := store.NotificationTarget(context.Background(), target.ID)
	if current.Config.Name != "Local destination" || !current.Config.Enabled || current.ConnectionCipher != updated.ConnectionCipher || restored.RowsAffected["checkin_notification_deliveries"] != 0 {
		t.Fatal("repeat import replaced local configuration or duplicated delivery records")
	}
	// Merge a newer history snapshot while preserving the local (lower) target
	// revision. The resulting export must remain a valid, importable backup.
	dump.NotificationTargets[0].Revision = 20
	historical := dump.NotificationDeliveries[0]
	for _, candidate := range dump.NotificationDeliveries {
		if candidate.Event.Kind == "test" {
			historical = candidate
			break
		}
	}
	historical.ID, historical.Event.Identity, historical.TargetRevision = uuid.NewString(), uuid.NewString(), 20
	dump.NotificationDeliveries = append(dump.NotificationDeliveries, historical)
	restoreCheckinFixture(t, conn, &dump)
	merged, err := exportCheckin(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCheckinDump(merged); err != nil {
		t.Fatal("merged notification history cannot be backed up again", err)
	}
}

func TestNotificationBackupValidatesCipherSnapshotsAndReferences(t *testing.T) {
	_, raw := notificationBackupFixture(t)
	for _, mutation := range []func(*model.CheckinDump){
		func(d *model.CheckinDump) { d.NotificationTargets[0].Cipher = "invalid-cipher" },
		func(d *model.CheckinDump) { d.NotificationTargets[0].ID = uuid.NewString() },
		func(d *model.CheckinDump) { d.NotificationTargets[0].Config.SiteIDs = []string{uuid.NewString()} },
		func(d *model.CheckinDump) { d.NotificationTargets[0].Config.BodyTemplate = "{{access_token}}" },
		func(d *model.CheckinDump) { d.NotificationDeliveries[0].Reason = "private-upstream-error" },
		func(d *model.CheckinDump) { d.NotificationDeliveries[0].TargetID = uuid.NewString() },
		func(d *model.CheckinDump) {
			d.NotificationDeliveries = append(d.NotificationDeliveries, d.NotificationDeliveries[0])
		},
		func(d *model.CheckinDump) { d.Version = 5 },
	} {
		var dump model.CheckinDump
		_ = json.Unmarshal(raw, &dump)
		mutation(&dump)
		if err := validateCheckinDump(&dump); err == nil {
			t.Fatal("invalid notification backup accepted")
		}
	}
	var old model.CheckinDump
	_ = json.Unmarshal(raw, &old)
	old.Version, old.NotificationTargets, old.NotificationDeliveries = 5, nil, nil
	if err := validateCheckinDump(&old); err != nil {
		t.Fatal("legacy backup no longer imports", err)
	}
}
