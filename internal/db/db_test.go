package db

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlCaptureLogger records SQL to verify that restarting does not mutate an
// existing database.
type sqlCaptureLogger struct {
	mu         sync.Mutex
	statements []string
}

func (l *sqlCaptureLogger) LogMode(logger.LogLevel) logger.Interface      { return l }
func (l *sqlCaptureLogger) Info(context.Context, string, ...interface{})  {}
func (l *sqlCaptureLogger) Warn(context.Context, string, ...interface{})  {}
func (l *sqlCaptureLogger) Error(context.Context, string, ...interface{}) {}
func (l *sqlCaptureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statements = append(l.statements, sql)
}
func (l *sqlCaptureLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.statements))
	copy(out, l.statements)
	return out
}
func (l *sqlCaptureLogger) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statements = l.statements[:0]
}

func TestInitializeSchemaCreatesCurrentTablesAndPreservesExistingData(t *testing.T) {
	capture := &sqlCaptureLogger{}
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: capture})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := initializeSchema(gormDB); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}
	for _, table := range []string{"channels", "channel_keys", "proxy_configurations", "sites", "site_accounts", "site_checkin_logs", "site_models", "groups", "group_items", "settings", "relay_logs", "stats_site_model_hourlies", "ws_response_affinities"} {
		if !gormDB.Migrator().HasTable(table) {
			t.Errorf("missing current table %s", table)
		}
	}
	for _, table := range []string{"migration_records", "site_prices", "site_disabled_models"} {
		if gormDB.Migrator().HasTable(table) {
			t.Errorf("obsolete table %s was created", table)
		}
	}
	for _, index := range []struct{ table, name string }{
		{"site_models", "idx_site_account_group_model"},
		{"site_checkin_logs", "idx_site_checkin_site_id"},
		{"site_checkin_logs", "idx_site_checkin_account_id"},
		{"stats_site_model_hourlies", "idx_stats_site_model_account_hour"},
	} {
		if !gormDB.Migrator().HasIndex(index.table, index.name) {
			t.Errorf("missing current index %s", index.name)
		}
	}
	entry := model.RelayLog{ID: 1, Success: true, RequestModelName: "current-model"}
	if err := gormDB.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	setting := model.Setting{Key: model.SettingKeyProjectedChannelAutoGroupEnabled, Value: "2"}
	if err := gormDB.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	capture.reset()
	if err := initializeSchema(gormDB); err != nil {
		t.Fatalf("reinitialize schema: %v", err)
	}
	for _, sql := range capture.snapshot() {
		upper := strings.ToUpper(strings.TrimSpace(sql))
		for _, mutation := range []string{"CREATE ", "ALTER ", "DROP ", "UPDATE ", "INSERT ", "DELETE "} {
			if strings.HasPrefix(upper, mutation) {
				t.Errorf("startup modified an existing database: %s", sql)
			}
		}
	}
	var saved model.RelayLog
	if err := gormDB.First(&saved, entry.ID).Error; err != nil || !saved.Success || saved.RequestModelName != entry.RequestModelName {
		t.Fatalf("existing log changed: %+v, error=%v", saved, err)
	}
	var savedSetting model.Setting
	if err := gormDB.First(&savedSetting, "key = ?", setting.Key).Error; err != nil || savedSetting.Value != setting.Value {
		t.Fatalf("existing setting changed: %+v, error=%v", savedSetting, err)
	}
}

func TestInitializeSchemaAddsCustomHTTPCheckinColumnsToExistingSites(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := gormDB.Exec("CREATE TABLE sites (id integer PRIMARY KEY, name text NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := gormDB.Exec("INSERT INTO sites (id, name) VALUES (7, 'legacy')").Error; err != nil {
		t.Fatal(err)
	}
	if err := initializeSchema(gormDB); err != nil {
		t.Fatalf("upgrade existing schema: %v", err)
	}
	for _, column := range []string{"checkin_http_enabled", "checkin_http_method", "checkin_http_path", "checkin_http_body", "checkin_http_headers"} {
		if !gormDB.Migrator().HasColumn(&model.Site{}, column) {
			t.Errorf("missing migrated site column %s", column)
		}
	}
	var name string
	if err := gormDB.Raw("SELECT name FROM sites WHERE id = 7").Scan(&name).Error; err != nil || name != "legacy" {
		t.Fatalf("legacy site data changed: name=%q error=%v", name, err)
	}
}
