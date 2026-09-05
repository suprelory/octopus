package op

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// DBExportZip streams the database dump as a ZIP archive: small tables become
// JSON files, relay_logs become NDJSON to avoid building a giant in-memory
// slice. The writer is consumed once; failures partway through cannot return a
// JSON error to the client, so callers should validate inputs before invoking.
func DBExportZip(ctx context.Context, w io.Writer, includeLogs, includeStats bool) (err error) {
	zw := zip.NewWriter(w)
	defer func() {
		if closeErr := zw.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	conn := db.GetDB().WithContext(ctx)

	manifest := map[string]any{
		"version":       dbDumpVersion,
		"exported_at":   time.Now().UTC().Format(time.RFC3339),
		"include_logs":  includeLogs,
		"include_stats": includeStats,
		"format":        "zip-v1",
	}
	if err := writeZipJSON(zw, "manifest.json", manifest); err != nil {
		return err
	}

	if err := writeZipTable(ctx, zw, conn, "channels.json", &[]model.Channel{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "channel_keys.json", &[]model.ChannelKey{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "proxy_configurations.json", &[]model.ProxyConfiguration{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "sites.json", &[]model.Site{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_accounts.json", &[]model.SiteAccount{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_tokens.json", &[]model.SiteToken{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_user_groups.json", &[]model.SiteUserGroup{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_models.json", &[]model.SiteModel{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "site_channel_bindings.json", &[]model.SiteChannelBinding{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "groups.json", &[]model.Group{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "group_items.json", &[]model.GroupItem{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "llm_infos.json", &[]model.LLMInfo{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "api_keys.json", &[]model.APIKey{}); err != nil {
		return err
	}
	if err := writeZipTable(ctx, zw, conn, "settings.json", &[]model.Setting{}); err != nil {
		return err
	}

	if includeStats {
		if err := writeZipTable(ctx, zw, conn, "stats_total.json", &[]model.StatsTotal{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_daily.json", &[]model.StatsDaily{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_hourly.json", &[]model.StatsHourly{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_model.json", &[]model.StatsModel{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_channel.json", &[]model.StatsChannel{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_api_key.json", &[]model.StatsAPIKey{}); err != nil {
			return err
		}
		if err := writeZipTable(ctx, zw, conn, "stats_site_model_hourly.json", &[]model.StatsSiteModelHourly{}); err != nil {
			return err
		}
	}

	if includeLogs {
		if err := writeZipRelayLogsNDJSON(ctx, zw, conn); err != nil {
			return err
		}
	}

	return nil
}

func writeZipJSON(zw *zip.Writer, name string, value any) error {
	f, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("zip create %s: %w", name, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "")
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("zip encode %s: %w", name, err)
	}
	return nil
}

func writeZipTable[T any](ctx context.Context, zw *zip.Writer, conn *gorm.DB, name string, dest *[]T) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := conn.Find(dest).Error; err != nil {
		return fmt.Errorf("zip read %s: %w", name, err)
	}
	return writeZipJSON(zw, name, dest)
}

func writeZipRelayLogsNDJSON(ctx context.Context, zw *zip.Writer, conn *gorm.DB) error {
	f, err := zw.Create("relay_logs.ndjson")
	if err != nil {
		return fmt.Errorf("zip create relay_logs.ndjson: %w", err)
	}
	enc := json.NewEncoder(f)
	var lastID int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var batch []model.RelayLog
		if err := conn.Where("id > ?", lastID).Order("id ASC").Limit(dbExportLogBatchSize).Find(&batch).Error; err != nil {
			return fmt.Errorf("zip read relay_logs: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("zip encode relay_log: %w", err)
			}
		}
		lastID = batch[len(batch)-1].ID
		if len(batch) < dbExportLogBatchSize {
			break
		}
	}
	return nil
}
