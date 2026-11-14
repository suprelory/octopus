package op

import (
	"errors"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func TestSubmitIntentStrengthensSQLiteDurabilityAndRestoresPoolMode(t *testing.T) {
	conn := checkinTestDB(t)
	if err := conn.Exec("PRAGMA synchronous = NORMAL").Error; err != nil {
		t.Fatal(err)
	}
	for _, rollback := range []bool{false, true} {
		err := checkinIntentTransaction(conn, func(tx *gorm.DB) error {
			var mode int
			if err := tx.Session(&gorm.Session{NewDB: true}).Raw("PRAGMA synchronous").Scan(&mode).Error; err != nil {
				return err
			}
			if mode < 2 {
				t.Fatal("submit intent transaction is not fully synchronized")
			}
			if rollback {
				return errors.New("simulated canceled write")
			}
			return tx.Create(&model.Setting{Key: "checkin_durability_test", Value: "committed"}).Error
		})
		if (err != nil) != rollback {
			t.Fatalf("unexpected transaction result: %v", err)
		}
		var mode int
		if err := conn.Raw("PRAGMA synchronous").Scan(&mode).Error; err != nil || mode != 1 {
			t.Fatal("pool mode was not restored", err, mode)
		}
	}
}
