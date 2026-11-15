package op

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func proxyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("OCTOPUS_PROXY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	conn := checkinTestDB(t)
	if err := conn.AutoMigrate(&model.Channel{}); err != nil {
		t.Fatal(err)
	}
	return conn
}

func proxySiteConfig(id int) model.CheckinSiteConfig {
	return model.CheckinSiteConfig{Name: "Pool site", BaseURL: "https://example.invalid", Adapter: "http", Enabled: true, ProxyMode: "pool", ProxyConfigID: &id, Submit: model.CheckinRequest{Path: "/checkin"}}
}

func TestProxyCredentialsReferencesAndLifecycle(t *testing.T) {
	ctx, conn := context.Background(), proxyTestDB(t)
	node, err := ProxySave(ctx, conn, 0, model.ProxyWrite{Name: "Authenticated", URL: "socks5h://alice:proxy-private-password@localhost:1080", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(node)
	if !node.HasAuth || node.AuthCipher == "" || strings.Contains(node.URL, "@") || bytes.Contains(raw, []byte("alice")) || bytes.Contains(raw, []byte("proxy-private-password")) || bytes.Contains(raw, []byte("AuthCipher")) {
		t.Fatal("proxy API exposes authentication")
	}
	selection := model.ProxySelection{Mode: "pool", ConfigID: &node.ID}
	resolved, err := ResolveProxy(ctx, conn, selection)
	if err != nil || !strings.Contains(resolved.URL, "alice:proxy-private-password@") {
		t.Fatal("encrypted proxy authentication was not restored", err)
	}
	updated, err := ProxySave(ctx, conn, node.ID, model.ProxyWrite{Name: "Renamed", URL: node.URL, Enabled: true, Revision: node.Revision})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := ResolveProxy(ctx, conn, selection)
	if after.Fingerprint != resolved.Fingerprint || updated.AuthCipher != node.AuthCipher {
		t.Fatal("metadata edit changed route or lost credentials")
	}
	if _, err := ProxySave(ctx, conn, node.ID, model.ProxyWrite{Name: "stale", URL: node.URL, Revision: node.Revision}); !errors.Is(err, ErrProxyConflict) {
		t.Fatal("stale edit accepted", err)
	}
	store := NewCheckinStore(conn)
	site, err := store.SaveSite(ctx, "", 0, proxySiteConfig(node.ID))
	if err != nil {
		t.Fatal(err)
	}
	channel := model.Channel{ChannelConfig: model.ChannelConfig{Name: "Pool channel", ProxyMode: "pool", ProxyConfigID: &node.ID}}
	if err := conn.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"inherit", "pool"} {
		account := model.CheckinAccount{ID: uuid.NewString(), SiteID: site.ID, Name: mode, ProxyMode: mode, CredentialType: "cookie"}
		if mode == "pool" {
			account.ProxyConfigID = &node.ID
		}
		if err := conn.Create(&account).Error; err != nil {
			t.Fatal(err)
		}
	}
	refs, err := ProxyReferences(ctx, conn, node.ID)
	if err != nil || len(refs) != 4 {
		t.Fatal("incomplete references", refs, err)
	}
	list, err := ProxyList(ctx, conn)
	if err != nil || len(list) != 1 || list[0].ReferenceCount != 3 {
		t.Fatal("inherited account double-counted", list, err)
	}
	if err := ProxyDelete(ctx, conn, node.ID); !errors.Is(err, ErrProxyConflict) {
		t.Fatal("referenced proxy deleted", err)
	}
	updated, err = ProxySave(ctx, conn, node.ID, model.ProxyWrite{Name: updated.Name, URL: updated.URL, Enabled: false, Revision: updated.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProxy(ctx, conn, selection); err == nil {
		t.Fatal("disabled proxy fell back to direct")
	}
	if _, err := ProxyURLForTest(ctx, conn, node.ID); err != nil {
		t.Fatal("disabled node cannot be tested", err)
	}
	if _, err := store.SaveSite(ctx, "", 0, proxySiteConfig(node.ID)); err == nil {
		t.Fatal("new binding to disabled node accepted")
	}
	if _, err := store.SaveSite(ctx, site.ID, site.Revision, site.Config); err != nil {
		t.Fatal("unchanged disabled binding cannot be edited", err)
	}
	if err := conn.Delete(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, site.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := ProxyDelete(ctx, conn, node.ID); err != nil {
		t.Fatal("soft-deleted sites retained proxy bindings", err)
	}
}

func TestProxyBindingAndDeleteAreAtomic(t *testing.T) {
	ctx, conn := context.Background(), proxyTestDB(t)
	store := NewCheckinStore(conn)
	for i := 0; i < 8; i++ {
		node, err := ProxySave(ctx, conn, 0, model.ProxyWrite{Name: uuid.NewString(), URL: "http://127.0.0.1:7890", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var saveErr, deleteErr error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, saveErr = store.SaveSite(ctx, "", 0, proxySiteConfig(node.ID)) }()
		go func() { defer wg.Done(); <-start; deleteErr = ProxyDelete(ctx, conn, node.ID) }()
		close(start)
		wg.Wait()
		if saveErr == nil && deleteErr == nil {
			t.Fatal("binding and deletion both succeeded")
		}
		var dangling int64
		conn.Table("checkin_sites").Joins("LEFT JOIN proxy_configurations ON checkin_sites.proxy_config_id = proxy_configurations.id").Where("checkin_sites.proxy_config_id IS NOT NULL AND proxy_configurations.id IS NULL").Count(&dangling)
		if dangling != 0 {
			t.Fatal("dangling proxy binding")
		}
	}
}

func TestProxyBackupRemapsCollidingIDsAndRetainsSecrets(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OCTOPUS_PROXY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	t.Setenv("OCTOPUS_CHECKIN_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32)))
	if err := db.InitDB("sqlite", filepath.ToSlash(filepath.Join(t.TempDir(), "source.db")), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	source := db.GetDB()
	node, err := ProxySave(ctx, source, 0, model.ProxyWrite{Name: "Backup node", URL: "http://test:backup-proxy-password@localhost:7890", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	selection := model.ProxySelection{Mode: "pool", ConfigID: &node.ID}
	original, _ := ResolveProxy(ctx, source, selection)
	channel := model.Channel{ID: 41, ChannelConfig: model.ChannelConfig{Name: "Imported channel", BaseURL: "https://example.invalid", ProxyMode: "pool", ProxyConfigID: &node.ID}}
	if err := source.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	store := NewCheckinStore(source)
	site, err := store.SaveSite(ctx, "", 0, proxySiteConfig(node.ID))
	if err != nil {
		t.Fatal(err)
	}
	account := model.CheckinAccount{ID: uuid.NewString(), SiteID: site.ID, Name: "Imported account", Enabled: true, AutoCheckin: true, ProxyMode: "pool", ProxyConfigID: &node.ID, CredentialType: "cookie"}
	vault, _ := checkinsecret.Open()
	account.CredentialCipher, _ = vault.Seal(account.ID, []byte(`{"type":"cookie","cookie":"session=backup-site-secret"}`))
	if _, err := store.SaveAccount(ctx, account, 0, 0, true); err != nil {
		t.Fatal(err)
	}
	notification := backupNotification(t, store, model.CheckinNotificationConfig{Name: "Proxy notifications", Kind: "webhook", Enabled: true, Events: []string{"failed"}, Scope: "all", ProxyMode: "pool", ProxyConfigID: &node.ID})
	refs, err := ProxyReferences(ctx, source, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundNotification := false
	for _, ref := range refs {
		if ref.Kind == "checkin_notification" && ref.ID == notification.ID {
			foundNotification = true
		}
	}
	if !foundNotification {
		t.Fatal("notification proxy reference missing")
	}
	dump, err := DBExportAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(dump)
	if bytes.Contains(raw, []byte("backup-proxy-password")) {
		t.Fatal("plaintext proxy credentials in backup")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.InitDB("sqlite", filepath.ToSlash(filepath.Join(t.TempDir(), "target.db")), false); err != nil {
		t.Fatal(err)
	}
	target := db.GetDB()
	occupied, err := ProxySave(ctx, target, 0, model.ProxyWrite{Name: "Backup node", URL: "http://127.0.0.1:1081", Enabled: false})
	if err != nil || occupied.ID != node.ID {
		t.Fatal("collision fixture failed", err)
	}
	for i := 0; i < 2; i++ {
		var decoded model.DBDump
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, err := DBImportIncremental(ctx, &decoded); err != nil {
			t.Fatal(err)
		}
	}
	var restored model.Channel
	if err := target.First(&restored, "id = ?", channel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.ProxyConfigID == nil || *restored.ProxyConfigID == occupied.ID {
		t.Fatal("import bound an unrelated existing node")
	}
	after, err := ResolveProxy(ctx, target, restored.ProxySelection())
	if err != nil || after.URL != original.URL || after.Fingerprint != original.Fingerprint {
		t.Fatal("route identity changed during remapping", err)
	}
	restoredSite, _ := NewCheckinStore(target).Site(ctx, site.ID)
	restoredAccount, _ := NewCheckinStore(target).Account(ctx, account.ID)
	restoredNotification, _ := NewCheckinStore(target).NotificationTarget(ctx, notification.ID)
	if !sameProxyID(restoredNotification.ProxyConfigID, restored.ProxyConfigID) || !sameProxyID(restoredNotification.Config.ProxyConfigID, restored.ProxyConfigID) || restoredNotification.Config.Enabled {
		t.Fatal("notification proxy mapping or disabled automation lost")
	}
	if !sameProxyID(restoredSite.ProxyConfigID, restored.ProxyConfigID) || !sameProxyID(restoredSite.Config.ProxyConfigID, restored.ProxyConfigID) || !sameProxyID(restoredAccount.ProxyConfigID, restored.ProxyConfigID) || restoredAccount.AutoCheckin {
		t.Fatal("checkin binding or disabled automation lost")
	}
	var missing model.DBDump
	_ = json.Unmarshal(raw, &missing)
	missing.ProxyConfigurations = nil
	if _, err := DBImportIncremental(ctx, &missing); err == nil {
		t.Fatal("missing reference silently fell back")
	}
	t.Setenv("OCTOPUS_PROXY_MASTER_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	_ = json.Unmarshal(raw, &missing)
	if _, err := DBImportIncremental(ctx, &missing); err == nil {
		t.Fatal("mismatched proxy credential key accepted")
	}
}
