package op

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func checkinTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	conn, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "backup.db"))+"?_pragma=foreign_keys(ON)"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.CheckinSite{}, &model.CheckinAccount{}, &model.CheckinRun{}, &model.CheckinAttempt{}, &model.CheckinJob{}, &model.CheckinJobItem{}, &model.CheckinNotificationTarget{}, &model.CheckinNotificationDelivery{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return conn
}

func checkinBackupFixture(t *testing.T) (*CheckinStore, *model.CheckinDump) {
	t.Helper()
	key := bytes.Repeat([]byte{7}, 32)
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(key))
	vault, _ := checkinsecret.New(key)
	store := NewCheckinStore(checkinTestDB(t))
	ctx := context.Background()
	site, err := store.SaveSite(ctx, "", 0, model.CheckinSiteConfig{Name: "Backup site", BaseURL: "https://example.invalid", Adapter: "http", Enabled: true, Timezone: "UTC", Submit: model.CheckinRequest{Path: "/checkin"}})
	if err != nil {
		t.Fatal(err)
	}
	account := model.CheckinAccount{ID: uuid.NewString(), SiteID: site.ID, Name: "Backup account", Enabled: true, AutoCheckin: true, CredentialType: "cookie", CredentialUserID: "42"}
	plain, _ := json.Marshal(model.CheckinCredentials{Type: "cookie", Cookie: "session=private-backup-value", UserID: "42"})
	account.CredentialCipher, err = vault.Seal(account.ID, plain)
	if err != nil {
		t.Fatal(err)
	}
	account, err = store.SaveAccount(ctx, account, 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job, err := store.Enqueue(ctx, model.CheckinScope{Kind: "accounts", IDs: []string{account.ID}}, "manual", "", []model.CheckinJobItem{{AccountID: account.ID, SiteID: site.ID, AccountName: account.Name, SiteName: site.Config.Name, CheckinDate: now.Format("2006-01-02")}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.Queue(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal("queue fixture failed", err)
	}
	item := items[0]
	if ok, err := store.Claim(ctx, &item, now); !ok || err != nil {
		t.Fatal("claim fixture failed", err)
	}
	run, err := store.BeginRun(ctx, item, site, account, strings.Repeat("a", 64), "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(time.Hour)
	run.ConfirmDeadline = &deadline
	if err := store.MarkSubmitted(ctx, item, &run, now); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAttempt(ctx, model.CheckinAttempt{RunID: run.ID, JobID: job.ID, Stage: "submit", Reason: "network_error", Message: "Request failed, timed out, or exceeded the response limit", CreatedAt: now}); err != nil {
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
	if bytes.Contains(raw, []byte("private-backup-value")) || bytes.Contains(raw, []byte("lease_owner")) || bytes.Contains(raw, []byte("lease_until")) {
		t.Fatal("backup leaks plaintext or process leases")
	}
	var decoded model.CheckinDump
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return store, &decoded
}

func restoreCheckinFixture(t *testing.T, target *gorm.DB, dump *model.CheckinDump) *model.DBImportResult {
	t.Helper()
	if err := validateCheckinDump(dump); err != nil {
		t.Fatal(err)
	}
	result := &model.DBImportResult{RowsAffected: map[string]int64{}}
	if err := target.Transaction(func(tx *gorm.DB) error { return importCheckin(tx, dump, result) }); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCheckinBackupRoundTripDisablesAutomationAndNeverReplays(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	if dump.Runs[0].SubmitIntentAt == nil || dump.Runs[0].Fingerprint == "" || dump.Accounts[0].Cipher == "" {
		t.Fatal("durable evidence missing from backup")
	}
	target := checkinTestDB(t)
	restoreCheckinFixture(t, target, dump)
	store := NewCheckinStore(target)
	accounts, err := store.Accounts(context.Background(), "")
	if err != nil || len(accounts) != 1 {
		t.Fatal(err)
	}
	account := accounts[0]
	if account.AutoCheckin || account.NextRunAt != nil || account.LeaseOwner != "" || account.LeaseUntil != nil || account.CredentialUserID != "42" {
		t.Fatalf("unsafe restored account: %+v", account)
	}
	run, err := store.Run(context.Background(), dump.Runs[0].ID)
	if err != nil || run.Status != "manual_required" || run.Reason != "restored_pending" || run.SubmitIntentAt == nil || run.NextConfirmAt != nil {
		t.Fatalf("unsafe restored ledger: %+v %v", run, err)
	}
	jobs, _ := store.Jobs(context.Background())
	if len(jobs) != 1 || jobs[0].Status != "interrupted" || jobs[0].Completed != 1 || jobs[0].Skipped != 1 {
		t.Fatalf("restored job counters wrong: %+v", jobs)
	}
	queue, _ := store.Queue(context.Background())
	if len(queue) != 0 {
		t.Fatal("backup restoration requeued old work")
	}
	firstCipher := account.CredentialCipher
	account.AutoCheckin = true
	account, err = store.SaveAccount(context.Background(), account, account.Revision, account.CredentialVersion, false)
	if err != nil {
		t.Fatal(err)
	}
	again := restoreCheckinFixture(t, target, dump)
	for table, n := range again.RowsAffected {
		if n != 0 {
			t.Fatalf("reimport inserted %d rows into %s", n, table)
		}
	}
	current, _ := store.Account(context.Background(), account.ID)
	if !current.AutoCheckin || current.CredentialCipher != firstCipher || current.Revision != account.Revision {
		t.Fatal("repeat import overwrote local changes")
	}
}

func TestRouterBackupPreservesEncryptedLoginAndDisablesReplay(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	dump.Sites[0].Config.Adapter = "anyrouter"
	dump.Sites[0].Config.Submit = model.CheckinRequest{Method: "POST", Path: "/api/user/sign_in"}
	dump.Attempts[0].Stage = "authenticate"
	account := &dump.Accounts[0]
	account.CredentialType = "password"
	now := time.Now().UTC()
	account.SessionSavedAt = &now
	credentials := model.CheckinCredentials{Type: "password", Username: "private-router-user", Password: "private-router-password", UserID: "42", Session: &model.CheckinLoginSession{BaseURL: dump.Sites[0].Config.BaseURL, Auth: "cookie", Cookies: []model.CheckinSessionCookie{{Name: "session", Value: "private-router-cookie", Path: "/", Secure: true, Expires: now.Add(time.Hour)}}}}
	vault, err := checkinsecret.Open()
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(credentials)
	account.Cipher, err = vault.Seal(account.ID, plain)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(dump)
	if strings.Contains(string(raw), "private-router") {
		t.Fatal("router backup exposed plaintext credentials")
	}
	target := checkinTestDB(t)
	restoreCheckinFixture(t, target, dump)
	restored, err := NewCheckinStore(target).Account(context.Background(), account.ID)
	if err != nil || restored.AutoCheckin || !restored.HasSession || restored.SessionSavedAt == nil || restored.CredentialCipher != account.Cipher {
		t.Fatal("router backup lost credentials or resumed automation", err)
	}
	opened, err := vault.Unseal(restored.ID, restored.CredentialCipher)
	if err != nil || string(opened) != string(plain) {
		t.Fatal("router session did not round trip")
	}
	dump.Sites[0].Config.Adapter = "new-api"
	dump.Sites[0].Config.Submit = model.CheckinRequest{Method: "POST", Path: "/api/user/checkin"}
	if validateCheckinDump(dump) == nil {
		t.Fatal("backup accepted password credentials on incompatible platform")
	}
}

func TestCheckinBackupStillAcceptsEarlierSubversions(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	for _, version := range []int{1, 2, 3} {
		dump.Version = version
		if err := validateCheckinDump(dump); err != nil {
			t.Fatalf("version %d rejected: %v", version, err)
		}
	}
}

func TestCheckinBackupRejectsWrongKeyAndBrokenGraph(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	if err := validateCheckinDump(dump); err == nil {
		t.Fatal("backup accepted with wrong key")
	}
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	raw, _ := json.Marshal(dump)
	for _, test := range []struct {
		name   string
		change func(*model.CheckinDump)
	}{
		{"duplicate site", func(d *model.CheckinDump) { d.Sites = append(d.Sites, d.Sites[0]) }},
		{"unrelated account", func(d *model.CheckinDump) { d.Runs[0].AccountID = uuid.NewString() }},
		{"missing run", func(d *model.CheckinDump) { d.Items[0].RunID = uuid.NewString() }},
		{"wrong task total", func(d *model.CheckinDump) { d.Jobs[0].Total++ }},
		{"duplicate daily ledger", func(d *model.CheckinDump) { r := d.Runs[0]; r.ID = uuid.NewString(); d.Runs = append(d.Runs, r) }},
		{"invalid confirmation scope", func(d *model.CheckinDump) {
			d.Jobs[0].Scope = model.CheckinScope{Kind: "confirmation", IDs: []string{uuid.NewString()}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var candidate model.CheckinDump
			_ = json.Unmarshal(raw, &candidate)
			test.change(&candidate)
			if err := validateCheckinDump(&candidate); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}

func TestCheckinBackupRemapsDailyConflictsWithoutOverwritingRewards(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	target := checkinTestDB(t)
	site := dump.Sites[0]
	account := dump.Accounts[0].CheckinAccount
	account.CredentialCipher = dump.Accounts[0].Cipher
	if err := target.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	if err := target.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	existing := dump.Runs[0]
	existing.ID, existing.Status, existing.Phase, existing.RewardState = uuid.NewString(), "success", "finished", "known"
	reward := int64(99_000_000)
	existing.RewardMicros = &reward
	existing.RewardUnit = "USD"
	if err := target.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	restoreCheckinFixture(t, target, dump)
	var runs []model.CheckinRun
	target.Find(&runs)
	if len(runs) != 1 || runs[0].ID != existing.ID || runs[0].RewardMicros == nil || *runs[0].RewardMicros != reward {
		t.Fatal("daily ledger overwritten or duplicated")
	}
	var items []model.CheckinJobItem
	var attempts []model.CheckinAttempt
	target.Find(&items)
	target.Find(&attempts)
	if len(items) != 1 || items[0].RunID != existing.ID || len(attempts) != 1 || attempts[0].RunID != existing.ID {
		t.Fatal("history references not remapped")
	}
	restoreCheckinFixture(t, target, dump)
}

func TestCheckinBackupKeepsDeletedTombstonesAndHistory(t *testing.T) {
	store, dump := checkinBackupFixture(t)
	item := dump.Items[0]
	var running model.CheckinJobItem
	store.DB.First(&running, "id = ?", item.ID)
	run := dump.Runs[0]
	run.Status, run.Phase, run.Reason = "manual_required", "finished", "pending_confirmation"
	if err := store.Complete(context.Background(), running, &run, "manual_required", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), dump.Sites[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	exported, err := exportCheckin(context.Background(), store.DB)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	var decoded model.CheckinDump
	_ = json.Unmarshal(raw, &decoded)
	if decoded.Sites[0].DeletedAt == nil || decoded.Accounts[0].DeletedAt == nil || decoded.Accounts[0].Cipher != "" {
		t.Fatal("deletion not preserved")
	}
	target := checkinTestDB(t)
	restoreCheckinFixture(t, target, &decoded)
	restored := NewCheckinStore(target)
	sites, _ := restored.Sites(context.Background())
	accounts, _ := restored.Accounts(context.Background(), "")
	if len(sites) != 0 || len(accounts) != 0 {
		t.Fatal("deleted configuration reappeared")
	}
	var count int64
	target.Model(&model.CheckinRun{}).Count(&count)
	if count != 1 {
		t.Fatal("history lost")
	}
}

func TestDatabaseBackupVersionsRemainCompatible(t *testing.T) {
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err := db.InitDB("sqlite", filepath.ToSlash(filepath.Join(t.TempDir(), "legacy.db")), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, version := range []int{0, 5, 6, 7} {
		if _, err := DBImportIncremental(ctx, &model.DBDump{Version: version}); err != nil {
			t.Fatalf("version %d rejected: %v", version, err)
		}
	}
	if _, err := DBImportIncremental(ctx, &model.DBDump{Version: 99}); err == nil {
		t.Fatal("unknown backup version accepted")
	}
	dump, err := DBExportAll(ctx)
	if err != nil || dump.Version != 7 || dump.Checkin == nil || dump.Checkin.Version != 6 {
		t.Fatalf("new export missing versioned checkin data: %+v %v", dump, err)
	}
}

func TestSub2APISessionBackupPreservesIdentityAndUncertainRefresh(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	dump.Sites[0].Config.Adapter = "sub2api"
	vault, err := checkinsecret.Open()
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(time.Hour)
	credentials := model.CheckinCredentials{Type: "bearer", AccessToken: "backup-access", RefreshToken: "backup-refresh", ExpiresAt: &expiry}
	plain, _ := json.Marshal(credentials)
	account := &dump.Accounts[0]
	account.Cipher, err = vault.Seal(account.ID, plain)
	if err != nil {
		t.Fatal(err)
	}
	account.CredentialType, account.CredentialVersion, account.IdentityVersion = "bearer", 7, 2
	account.RefreshPending, account.RefreshError = true, "sub2api_refresh_uncertain"
	balance := int64(-12345679)
	account.Balance = &model.CheckinBalanceSnapshot{AmountMicros: &balance, AmountState: "known", UsedState: "unknown", Unit: "USD", UpdatedAt: time.Now().UTC()}
	account.Inspection = &model.CheckinInspection{ID: uuid.NewString(), Kind: "balance", Status: "running", Authentication: "unknown", CheckinSupport: "not_checked", CheckinStatus: "unknown", CheckinDate: dump.Runs[0].CheckinDate, Timezone: "UTC", StartedAt: time.Now().UTC(), Steps: []model.CheckinInspectionStep{{Stage: "refresh", HTTPStatus: 200, Reason: "response_received", CreatedAt: time.Now().UTC()}}}
	dump.Attempts[0].Stage = "refresh"
	raw, _ := json.Marshal(dump)
	if bytes.Contains(raw, []byte("backup-access")) || bytes.Contains(raw, []byte("backup-refresh")) {
		t.Fatal("session backup exposes tokens")
	}
	var decoded model.CheckinDump
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	target := checkinTestDB(t)
	restoreCheckinFixture(t, target, &decoded)
	restored, err := NewCheckinStore(target).Account(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.AutoCheckin || restored.IdentityVersion() != 2 || restored.CredentialVersion != 7 || !restored.RefreshPending || !restored.HasRefreshToken || restored.TokenExpiresAt == nil || !restored.TokenExpiresAt.Equal(expiry) {
		t.Fatal("backup lost session identity, uncertainty or expiry")
	}
	if restored.CredentialCipher != account.Cipher {
		t.Fatal("backup did not preserve the encrypted pair")
	}
	if restored.Balance == nil || restored.Balance.AmountMicros == nil || *restored.Balance.AmountMicros != balance || restored.Balance.UsedMicros != nil || restored.Inspection == nil || restored.Inspection.Status != "interrupted" || restored.Inspection.BalanceUpdated || restored.Inspection.FinishedAt == nil {
		t.Fatal("backup lost balance precision/unknown usage or resumed an unfinished inspection")
	}
	exported, err := exportCheckin(context.Background(), target)
	if err != nil || validateCheckinDump(exported) != nil {
		t.Fatal("restored observations cannot be exported again", err)
	}
	decoded.Accounts[0].IdentityVersion = 8
	if validateCheckinDump(&decoded) == nil {
		t.Fatal("invalid identity version accepted")
	}
}
func TestLegacyCheckinBackupsStillImportWithoutObservations(t *testing.T) {
	for version := 1; version <= 4; version++ {
		_, dump := checkinBackupFixture(t)
		dump.Version = version
		dump.Accounts[0].IdentityVersion = 0
		target := checkinTestDB(t)
		restoreCheckinFixture(t, target, dump)
		account, err := NewCheckinStore(target).Account(context.Background(), dump.Accounts[0].ID)
		if err != nil || account.IdentityVersion() != account.CredentialVersion || account.HasRefreshToken || account.Balance != nil || account.Inspection != nil {
			t.Fatal("legacy credential identity or unknown observation was not preserved")
		}
	}
}

func TestCheckinObservationBackupRejectsInconsistentOrUnredactedData(t *testing.T) {
	_, dump := checkinBackupFixture(t)
	now, zero := time.Now().UTC(), int64(0)
	dump.Accounts[0].Balance = &model.CheckinBalanceSnapshot{AmountMicros: &zero, AmountState: "known", UsedState: "unknown", Unit: "quota", UpdatedAt: now}
	dump.Accounts[0].Inspection = &model.CheckinInspection{ID: uuid.NewString(), Kind: "balance", Status: "success", Reason: "account_verified", Authentication: "valid", UserID: "42", BalanceUpdated: true, CheckinSupport: "not_checked", CheckinStatus: "unknown", CheckinDate: dump.Runs[0].CheckinDate, Timezone: "UTC", StartedAt: now, FinishedAt: &now, Steps: []model.CheckinInspectionStep{{Stage: "account", HTTPStatus: 200, Reason: "response_received", CreatedAt: now}}}
	if err := validateCheckinDump(dump); err != nil {
		t.Fatal("valid zero/unknown observation was rejected", err)
	}
	raw, _ := json.Marshal(dump)
	if !bytes.Contains(raw, []byte(`"amount_micros":"0"`)) {
		t.Fatal("backup lost the string micro-unit contract")
	}
	for _, modify := range []func(*model.CheckinAccountBackup){
		func(a *model.CheckinAccountBackup) { a.Balance.AmountState = "unknown" },
		func(a *model.CheckinAccountBackup) { a.Balance.Unit = "guessed-currency" },
		func(a *model.CheckinAccountBackup) { a.Inspection.Steps[0].Stage = "submit" },
		func(a *model.CheckinAccountBackup) { a.Inspection.Reason = "upstream-private-token" },
		func(a *model.CheckinAccountBackup) { a.Inspection.Steps[0].Reason = "upstream-private-token" },
		func(a *model.CheckinAccountBackup) { a.Inspection.FinishedAt = nil },
	} {
		var invalid model.CheckinDump
		if err := json.Unmarshal(raw, &invalid); err != nil {
			t.Fatal(err)
		}
		modify(&invalid.Accounts[0])
		if err := validateCheckinDump(&invalid); err == nil {
			t.Fatal("invalid or unredacted observation was accepted")
		}
	}
	target := checkinTestDB(t)
	existingSite := dump.Sites[0]
	existingSite.Config.BaseURL = "https://different-tenant.example.invalid"
	if err := target.Create(&existingSite).Error; err != nil {
		t.Fatal(err)
	}
	restoreCheckinFixture(t, target, dump)
	account, err := NewCheckinStore(target).Account(context.Background(), dump.Accounts[0].ID)
	if err != nil || account.Balance != nil || account.Inspection != nil {
		t.Fatal("import attributed an old site's balance to a different upstream tenant", err)
	}
}

func TestCheckinObservationSchemaUpgradePreservesPendingSubmission(t *testing.T) {
	store, dump := checkinBackupFixture(t)
	// Recreate the pre-observation schema, including its existing account/ledger.
	for _, column := range []string{"balance", "inspection"} {
		// Native SQLite DROP COLUMN leaves the existing foreign-key graph intact.
		if err := store.DB.Exec("ALTER TABLE checkin_accounts DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := store.DB.AutoMigrate(&model.CheckinAccount{}); err != nil {
			t.Fatal(err)
		}
		account, err := store.Account(context.Background(), dump.Accounts[0].ID)
		if err != nil || account.CredentialCipher != dump.Accounts[0].Cipher || account.Balance != nil || account.Inspection != nil {
			t.Fatal("schema upgrade changed credentials or invented observations", err)
		}
		run, err := store.Run(context.Background(), dump.Runs[0].ID)
		if err != nil || run.SubmitIntentAt == nil || !run.SubmitIntentAt.Equal(*dump.Runs[0].SubmitIntentAt) {
			t.Fatal("schema upgrade lost the pending submission marker", err)
		}
	}
}
