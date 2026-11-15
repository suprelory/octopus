package migrate

import "gorm.io/gorm"

func init() {
	RegisterAfterAutoMigration(Migration{Version: 13, Up: migrateProxyModes})
}

func migrateProxyModes(conn *gorm.DB) error {
	// Keep the legacy URL columns for older API clients and backups. Sign-in JSON
	// intentionally keeps omitted proxy modes, preserving historical fingerprints.
	return conn.Transaction(func(tx *gorm.DB) error {
		return tx.Table("channels").Where("proxy_mode IS NULL OR proxy_mode = ?", "").Update("proxy_mode", gorm.Expr("CASE WHEN proxy = ? THEN CASE WHEN channel_proxy IS NULL OR TRIM(channel_proxy) = '' THEN 'system' ELSE 'custom' END ELSE 'direct' END", true)).Error
	})
}
