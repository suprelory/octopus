package op

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func validateProxyDump(rows []model.ProxyConfigurationBackup) error {
	if len(rows) > 10000 {
		return errors.New("proxy backup exceeds resource limits")
	}
	ids, identities := map[int]bool{}, map[string]bool{}
	for i := range rows {
		row := &rows[i]
		if row.ID <= 0 || ids[row.ID] || identities[row.Identity] || row.Revision < 1 || row.RouteRevision < 1 || row.RouteRevision > row.Revision {
			return errors.New("invalid or duplicate proxy backup identity")
		}
		if _, err := uuid.Parse(row.Identity); err != nil {
			return errors.New("invalid proxy backup identity")
		}
		ids[row.ID], identities[row.Identity] = true, true
		row.Name, row.Remark = strings.TrimSpace(row.Name), strings.TrimSpace(row.Remark)
		if row.Name == "" || len(row.Name) > 128 || len(row.Remark) > 1024 || len(row.URL) > 4096 || len(row.Cipher) > 16384 {
			return errors.New("invalid proxy backup metadata")
		}
		u, err := model.ParseProxyURL(row.URL)
		if err != nil || u.User != nil {
			return errors.New("invalid proxy backup endpoint")
		}
		row.URL, row.UID, row.AuthCipher = u.String(), row.Identity, row.Cipher
		if _, err := proxyURL(row.ProxyConfiguration); err != nil {
			return err
		}
	}
	return nil
}

func importProxies(tx *gorm.DB, dump *model.DBDump, result *model.DBImportResult) error {
	ids := map[int]int{}
	for _, backup := range dump.ProxyConfigurations {
		row := backup.ProxyConfiguration
		oldID := row.ID
		var existing model.ProxyConfiguration
		err := tx.First(&existing, "uid = ?", row.UID).Error
		if err == nil {
			if row.URL != existing.URL || row.AuthCipher != existing.AuthCipher || row.RouteRevision != existing.RouteRevision {
				return errors.New("proxy backup conflicts with an existing node; restore requires matching route versions")
			}
			ids[oldID] = existing.ID
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		for suffix := 1; ; suffix++ {
			var count int64
			if err := tx.Model(&model.ProxyConfiguration{}).Where("name = ?", row.Name).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				break
			}
			row.Name = fmt.Sprintf("Imported proxy %s (%d)", row.UID[:8], suffix)
		}
		row.ID, row.ReferenceCount, row.HasAuth = 0, 0, false
		if err := tx.Create(&row).Error; err != nil {
			return errors.New("proxy backup could not be imported")
		}
		ids[oldID] = row.ID
		result.RowsAffected["proxy_configurations"]++
	}
	remap := func(mode string, id **int) error {
		if mode != "pool" {
			*id = nil
			return nil
		}
		if *id == nil {
			return errors.New("missing proxy reference in backup")
		}
		value, ok := ids[**id]
		if !ok {
			return errors.New("proxy reference is missing from backup")
		}
		*id = &value
		return nil
	}
	for i := range dump.Channels {
		if err := remap(dump.Channels[i].ProxyMode, &dump.Channels[i].ProxyConfigID); err != nil {
			return err
		}
	}
	if dump.Checkin != nil {
		for i := range dump.Checkin.NotificationTargets {
			row := &dump.Checkin.NotificationTargets[i]
			if row.DeletedAt != nil {
				row.Config.ProxyMode, row.Config.ProxyConfigID = "direct", nil
			}
			if err := remap(row.Config.ProxyMode, &row.Config.ProxyConfigID); err != nil {
				return err
			}
			row.ProxyConfigID = row.Config.ProxyConfigID
		}
		for i := range dump.Checkin.Sites {
			row := &dump.Checkin.Sites[i]
			if row.DeletedAt != nil {
				row.Config.ProxyMode, row.Config.ProxyConfigID, row.Config.ProxyURL = "direct", nil, ""
			}
			if err := remap(row.Config.ProxyMode, &row.Config.ProxyConfigID); err != nil {
				return err
			}
			row.ProxyConfigID = row.Config.ProxyConfigID
		}
		for i := range dump.Checkin.Accounts {
			row := &dump.Checkin.Accounts[i]
			if row.DeletedAt != nil {
				row.ProxyMode, row.ProxyConfigID, row.ProxyURL = "direct", nil, ""
			}
			if err := remap(row.ProxyMode, &row.ProxyConfigID); err != nil {
				return err
			}
		}
	}
	return nil
}
