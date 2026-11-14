package op

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// The regular SQLite pool uses NORMAL synchronization. A submit marker must be
// flushed before an external side effect, including when the host loses power.
// Reserve one connection and strengthen only this transaction's synchronization.
func checkinIntentTransaction(conn *gorm.DB, fn func(*gorm.DB) error) error {
	if conn.Dialector.Name() != "sqlite" {
		return conn.Transaction(fn)
	}
	return conn.Connection(func(reserved *gorm.DB) (result error) {
		var previous int
		if err := reserved.Session(&gorm.Session{NewDB: true}).Raw("PRAGMA synchronous").Scan(&previous).Error; err != nil {
			return err
		}
		if previous >= 2 {
			return reserved.Session(&gorm.Session{NewDB: true}).Transaction(fn)
		}
		if previous < 0 || previous > 3 {
			return fmt.Errorf("unsupported SQLite synchronization mode")
		}
		if err := reserved.Session(&gorm.Session{NewDB: true}).Exec("PRAGMA synchronous = FULL").Error; err != nil {
			return err
		}
		defer func() {
			// Returning a reserved connection must not leave the pool's settings
			// changed, even if the request's context was canceled.
			restore := "PRAGMA synchronous = OFF"
			if previous == 1 {
				restore = "PRAGMA synchronous = NORMAL"
			}
			if err := reserved.WithContext(context.WithoutCancel(conn.Statement.Context)).Exec(restore).Error; result == nil {
				result = err
			}
		}()
		return reserved.Session(&gorm.Session{NewDB: true}).Transaction(fn)
	})
}
