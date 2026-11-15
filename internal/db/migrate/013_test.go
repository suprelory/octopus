package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLegacyProxyModeMigration(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open(filepath.ToSlash(filepath.Join(t.TempDir(), "migration.db"))), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	defer sqlDB.Close()
	if err := conn.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY, proxy BOOLEAN, channel_proxy TEXT, proxy_mode TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		ID           int
		Proxy        bool
		ChannelProxy string
		ProxyMode    string
	}{
		{1, false, "http://unused:7890", ""}, {2, true, "", ""}, {3, true, " http://custom:7890 ", ""}, {4, true, "", "pool"},
	} {
		if err := conn.Table("channels").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrateProxyModes(conn); err != nil {
			t.Fatal(err)
		}
	}
	var modes []string
	conn.Table("channels").Order("id").Pluck("proxy_mode", &modes)
	for i, expected := range []string{"direct", "system", "custom", "pool"} {
		if modes[i] != expected {
			t.Fatalf("row %d: got %s want %s", i, modes[i], expected)
		}
	}
}
