package op

import (
	"context"
	"fmt"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	dbDumpVersion = 1

	// Keep import batches small enough for SQLite builds with low SQL variable limits.
	// Some exported tables (for example relay_logs) have many columns, so a conservative
	// row count avoids "too many SQL variables" during bulk insert/upsert.
	dbImportBatchSize    = 20
	dbExportLogBatchSize = 1000
)

func DBImportIncremental(ctx context.Context, dump *model.DBDump) (*model.DBImportResult, error) {
	if dump == nil {
		return nil, fmt.Errorf("empty dump")
	}

	if dump.Version != 0 && dump.Version != dbDumpVersion {
		return nil, fmt.Errorf("unsupported dump version: %d", dump.Version)
	}

	conn := db.GetDB().WithContext(ctx)
	res := &model.DBImportResult{RowsAffected: map[string]int64{}}

	err := conn.Transaction(func(tx *gorm.DB) error {
		state := newDBImportState(tx, dump, res)
		stages := []func() error{
			state.importProxies,
			state.importChannels,
			state.importChannelKeys,
			state.importSites,
			state.importAccounts,
			state.importTokens,
			state.importUserGroups,
			state.importModels,
			state.importBindings,
			state.importGroups,
			state.importGroupItems,
			state.importModelPrices,
			state.importAPIKeys,
			state.importSettings,
			state.importStats,
			state.importLogs,
		}
		for _, stage := range stages {
			if err := stage(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The import transaction has already committed; cache refresh failures are non-fatal
	// and can be recovered by a later InitCache/refresh cycle.
	if err := proxyConfigurationRefreshCache(ctx); err != nil {
		log.Warnw("refresh proxy configuration cache after import failed",
			"operation", "db_import_incremental",
			"error", err,
		)
	}
	return res, nil
}

// dbImportState belongs to one import transaction. Stages run in dependency order
// so all foreign-key remapping and writes share the same rollback boundary.
type dbImportState struct {
	tx                    *gorm.DB
	dump                  *model.DBDump
	result                *model.DBImportResult
	channelIDs            map[int]int
	unsupportedChannelIDs map[int]struct{}
	resolvedChannels      map[int]resolvedImportChannel
	proxyIDs              map[int]int
	siteIDs               map[int]int
	accountIDs            map[int]int
	userGroupIDs          map[int]int
	groupIDs              map[int]int
	apiKeyIDs             map[int]int
}

func newDBImportState(tx *gorm.DB, dump *model.DBDump, result *model.DBImportResult) *dbImportState {
	return &dbImportState{
		tx:                    tx,
		dump:                  dump,
		result:                result,
		channelIDs:            make(map[int]int),
		unsupportedChannelIDs: make(map[int]struct{}),
		resolvedChannels:      make(map[int]resolvedImportChannel),
		proxyIDs:              make(map[int]int),
		siteIDs:               make(map[int]int),
		accountIDs:            make(map[int]int),
		userGroupIDs:          make(map[int]int),
		groupIDs:              make(map[int]int),
		apiKeyIDs:             make(map[int]int),
	}
}

func createDoNothing[T any](tx *gorm.DB, rows []T) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

func createUpsertAll[T any](tx *gorm.DB, rows []T, columns []clause.Column) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   columns,
		UpdateAll: true,
	}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}

func createUpsertSettings(tx *gorm.DB, rows []model.Setting) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).CreateInBatches(&rows, dbImportBatchSize)
	return result.RowsAffected, result.Error
}
