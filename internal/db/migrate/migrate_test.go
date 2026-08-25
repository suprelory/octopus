package migrate

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newMigrateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// Success and Failed must be distinct: a const group that omits the expression
// repeats the previous one, which made both equal 1.
func TestMigrationRecordStatusValuesAreDistinct(t *testing.T) {
	if MigrationRecordStatusSuccess == MigrationRecordStatusFailed {
		t.Fatalf("Success and Failed share value %d", MigrationRecordStatusSuccess)
	}
}

// A migration that fails must be recorded as failed and retried on the next
// run, not recorded as success and skipped forever.
func TestFailedMigrationIsRetriedOnNextRun(t *testing.T) {
	db := newMigrateTestDB(t)

	runs := 0
	failing := []Migration{{Version: 900, Up: func(*gorm.DB) error {
		runs++
		return fmt.Errorf("boom")
	}}}
	if err := runMigrationsWithRecord(db, failing); err == nil {
		t.Fatal("expected the failing migration to return an error")
	}

	var record MigrationRecord
	if err := db.Where("version = ?", 900).First(&record).Error; err != nil {
		t.Fatalf("load migration record: %v", err)
	}
	if record.Status != MigrationRecordStatusFailed {
		t.Fatalf("expected recorded status %d, got %d", MigrationRecordStatusFailed, record.Status)
	}

	succeeding := []Migration{{Version: 900, Up: func(*gorm.DB) error {
		runs++
		return nil
	}}}
	if err := runMigrationsWithRecord(db, succeeding); err != nil {
		t.Fatalf("rerun after failure: %v", err)
	}
	if runs != 2 {
		t.Fatalf("expected the failed migration to run again (runs=%d)", runs)
	}

	if err := db.Where("version = ?", 900).First(&record).Error; err != nil {
		t.Fatalf("reload migration record: %v", err)
	}
	if record.Status != MigrationRecordStatusSuccess {
		t.Fatalf("expected success after retry, got %d", record.Status)
	}
}

// A migration already recorded as successful must not run again.
func TestSuccessfulMigrationIsSkipped(t *testing.T) {
	db := newMigrateTestDB(t)

	runs := 0
	build := func() []Migration {
		return []Migration{{Version: 901, Up: func(*gorm.DB) error {
			runs++
			return nil
		}}}
	}
	if err := runMigrationsWithRecord(db, build()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := runMigrationsWithRecord(db, build()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if runs != 1 {
		t.Fatalf("expected the migration to run once, ran %d times", runs)
	}
}
