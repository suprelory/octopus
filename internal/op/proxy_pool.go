package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/proxysecret"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

var ErrProxyConflict = errors.New("proxy changed or is still referenced; reload and try again")

// Serialize binding mutations and deletes within a process. Row locks and foreign
// keys also protect bindings across processes (including SQLite's write locking).
var proxyWriteMu sync.Mutex

func sameProxyID(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

var proxyCache = struct {
	sync.RWMutex
	conn           *gorm.DB
	epoch          uint64
	rows           map[int]model.ProxyConfiguration
	invalidateHTTP func()
}{rows: map[int]model.ProxyConfiguration{}}

// Registered by rhttp at startup, keeping op independent of the transport layer.
func RegisterProxyInvalidator(fn func()) {
	proxyCache.Lock()
	proxyCache.invalidateHTTP = fn
	proxyCache.Unlock()
}

func invalidateProxies() {
	proxyCache.Lock()
	proxyCache.rows = map[int]model.ProxyConfiguration{}
	proxyCache.epoch++
	fn := proxyCache.invalidateHTTP
	proxyCache.Unlock()
	if fn != nil {
		fn()
	}
}

func proxyRefreshCache(ctx context.Context) error {
	invalidateProxies()
	proxyCache.RLock()
	epoch := proxyCache.epoch
	proxyCache.RUnlock()
	var rows []model.ProxyConfiguration
	conn := db.GetDB()
	if err := conn.WithContext(ctx).Find(&rows).Error; err != nil {
		return err
	}
	proxyCache.Lock()
	defer proxyCache.Unlock()
	if proxyCache.epoch != epoch {
		return nil
	}
	proxyCache.conn = conn
	for _, row := range rows {
		proxyCache.rows[row.ID] = row
	}
	return nil
}

func proxyConfiguration(ctx context.Context, conn *gorm.DB, id int) (model.ProxyConfiguration, error) {
	useCache := conn == db.GetDB()
	proxyCache.RLock()
	row, found := proxyCache.rows[id]
	epoch := proxyCache.epoch
	found = found && proxyCache.conn == conn && useCache
	proxyCache.RUnlock()
	if found {
		return row, nil
	}
	if err := conn.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, errors.New("proxy node does not exist")
		}
		return row, errors.New("proxy configuration could not be loaded")
	}
	if useCache {
		proxyCache.Lock()
		if proxyCache.epoch == epoch {
			if proxyCache.conn != conn {
				proxyCache.rows = map[int]model.ProxyConfiguration{}
				proxyCache.conn = conn
			}
			proxyCache.rows[id] = row
		}
		proxyCache.Unlock()
	}
	return row, nil
}

func ValidateProxyBindingTx(tx *gorm.DB, selection model.ProxySelection, requireEnabled bool) error {
	if selection.Mode != "pool" {
		return nil
	}
	if selection.ConfigID == nil || *selection.ConfigID <= 0 {
		return errors.New("select a proxy node")
	}
	var row model.ProxyConfiguration
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", *selection.ConfigID).Error; err != nil {
		return errors.New("proxy node does not exist or could not be loaded")
	}
	if requireEnabled && !row.Enabled {
		return errors.New("proxy node is disabled")
	}
	return nil
}

func proxyURL(row model.ProxyConfiguration) (string, error) {
	u, err := model.ParseProxyURL(row.URL)
	if err != nil || u.User != nil {
		return "", errors.New("invalid proxy endpoint")
	}
	if row.AuthCipher != "" {
		vault, err := proxysecret.Open()
		if err != nil {
			return "", errors.New("proxy credential key is unavailable")
		}
		plain, err := vault.Unseal("proxy:"+row.UID, row.AuthCipher)
		if err != nil {
			return "", errors.New("proxy credentials unavailable; restore the matching proxy.key")
		}
		var auth struct {
			Username string
			Password string
		}
		if json.Unmarshal(plain, &auth) != nil {
			return "", errors.New("invalid encrypted proxy credentials")
		}
		u.User = url.UserPassword(auth.Username, auth.Password)
	}
	return u.String(), nil
}

// Disabled nodes may be tested before they are enabled for business traffic.
func ProxyURLForTest(ctx context.Context, conn *gorm.DB, id int) (string, error) {
	row, err := proxyConfiguration(ctx, conn, id)
	if err != nil {
		return "", err
	}
	return proxyURL(row)
}

func ResolveProxy(ctx context.Context, conn *gorm.DB, selection model.ProxySelection) (model.ResolvedProxy, error) {
	var result model.ResolvedProxy
	selection, err := model.NormalizeProxySelection(selection, false)
	if err != nil {
		return result, err
	}
	identity := selection.Mode
	switch selection.Mode {
	case "direct":
	case "custom":
		result.URL = selection.URL
		identity += ":" + result.URL
	case "system":
		var value string
		if conn == db.GetDB() {
			value, err = SettingGetString(model.SettingKeyProxyURL)
		} else {
			var setting model.Setting
			err = conn.WithContext(ctx).First(&setting, "key = ?", model.SettingKeyProxyURL).Error
			value = setting.Value
		}
		if err != nil || strings.TrimSpace(value) == "" {
			return result, errors.New("system proxy is not configured")
		}
		u, err := model.ParseProxyURL(value)
		if err != nil {
			return result, err
		}
		result.URL = u.String()
		identity += ":" + result.URL
	case "pool":
		row, err := proxyConfiguration(ctx, conn, *selection.ConfigID)
		if err != nil {
			return result, err
		}
		if !row.Enabled {
			return result, errors.New("proxy node is disabled")
		}
		result.URL, err = proxyURL(row)
		if err != nil {
			return result, err
		}
		result.ConfigID = row.ID
		// Stable across backup ID remapping, and independent of display metadata.
		identity += ":" + row.UID + ":" + strconv.FormatInt(row.RouteRevision, 10)
	}
	hash := sha256.Sum256([]byte(identity))
	result.Fingerprint = hex.EncodeToString(hash[:])
	return result, nil
}

func ProxySave(ctx context.Context, conn *gorm.DB, id int, input model.ProxyWrite) (model.ProxyConfiguration, error) {
	proxyWriteMu.Lock()
	defer proxyWriteMu.Unlock()
	row := model.ProxyConfiguration{UID: uuid.NewString(), Revision: 1, RouteRevision: 1}
	input.Name, input.Remark = strings.TrimSpace(input.Name), strings.TrimSpace(input.Remark)
	if input.Name == "" || len(input.Name) > 128 || len(input.Remark) > 1024 || len(input.URL) > 4096 {
		return row, errors.New("invalid proxy name, URL or remark length")
	}
	u, err := model.ParseProxyURL(input.URL)
	if err != nil {
		return row, err
	}
	auth := u.User
	u.User = nil
	if auth != nil && input.ClearAuth {
		return row, errors.New("supply credentials or clear them, not both")
	}
	err = conn.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
		if id != 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
				return errors.New("proxy node does not exist")
			}
			if row.Revision != input.Revision {
				return ErrProxyConflict
			}
			row.Revision++
			if row.URL != u.String() || auth != nil || (input.ClearAuth && row.AuthCipher != "") {
				row.RouteRevision++
			}
		}
		row.Name, row.URL, row.Enabled, row.Remark = input.Name, u.String(), input.Enabled, input.Remark
		if input.ClearAuth {
			row.AuthCipher = ""
		}
		if auth != nil {
			vault, err := proxysecret.Open()
			if err != nil {
				return errors.New("proxy credential key is unavailable")
			}
			password, _ := auth.Password()
			plain, _ := json.Marshal(struct {
				Username string
				Password string
			}{auth.Username(), password})
			row.AuthCipher, err = vault.Seal("proxy:"+row.UID, plain)
			if err != nil {
				return errors.New("proxy credentials could not be encrypted")
			}
		}
		var count int64
		if err := tx.Model(&model.ProxyConfiguration{}).Where("name = ? AND id <> ?", row.Name, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("proxy name already exists")
		}
		if id == 0 {
			return tx.Create(&row).Error
		}
		return tx.Model(&model.ProxyConfiguration{}).Where("id = ?", id).Select("Name", "URL", "AuthCipher", "Enabled", "Remark", "Revision", "RouteRevision", "UpdatedAt").Updates(&row).Error
	})
	if err != nil {
		return row, err
	}
	invalidateProxies()
	row.HasAuth = row.AuthCipher != ""
	return row, nil
}

func ProxyReferences(ctx context.Context, conn *gorm.DB, id int) ([]model.ProxyReference, error) {
	refs := []model.ProxyReference{}
	var channels []model.Channel
	if err := conn.WithContext(ctx).Select("id", "name").Where("proxy_config_id = ?", id).Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, row := range channels {
		refs = append(refs, model.ProxyReference{Kind: "channel", ID: strconv.Itoa(row.ID), Name: row.Name})
	}
	var sites []model.CheckinSite
	if err := conn.WithContext(ctx).Where("deleted_at IS NULL").Find(&sites).Error; err != nil {
		return nil, err
	}
	siteNames, inheritedSites := map[string]string{}, []string{}
	for _, row := range sites {
		siteNames[row.ID] = row.Config.Name
		if row.ProxyConfigID != nil && *row.ProxyConfigID == id {
			refs = append(refs, model.ProxyReference{Kind: "checkin_site", ID: row.ID, Name: row.Config.Name})
			inheritedSites = append(inheritedSites, row.ID)
		}
	}
	var accounts []model.CheckinAccount
	query := conn.WithContext(ctx).Where("deleted_at IS NULL").Where("proxy_config_id = ? OR (site_id IN ? AND (proxy_mode = ? OR proxy_mode = ?))", id, inheritedSites, "inherit", "")
	if err := query.Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, row := range accounts {
		refs = append(refs, model.ProxyReference{Kind: "checkin_account", ID: row.ID, Name: row.Name, SiteID: row.SiteID, SiteName: siteNames[row.SiteID], Inherited: row.ProxyMode == "inherit" || row.ProxyMode == ""})
	}
	return refs, nil
}

func ProxyList(ctx context.Context, conn *gorm.DB) ([]model.ProxyConfiguration, error) {
	rows := []model.ProxyConfiguration{}
	if err := conn.WithContext(ctx).Order("name, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	counts := map[int]int{}
	for _, table := range []string{"channels", "checkin_sites", "checkin_accounts"} {
		var grouped []struct {
			ID    int
			Total int
		}
		q := conn.WithContext(ctx).Table(table).Select("proxy_config_id AS id, COUNT(*) AS total").Where("proxy_config_id IS NOT NULL").Group("proxy_config_id")
		if table != "channels" {
			q = q.Where("deleted_at IS NULL")
		}
		if err := q.Scan(&grouped).Error; err != nil {
			return nil, err
		}
		for _, entry := range grouped {
			counts[entry.ID] += entry.Total
		}
	}
	for i := range rows {
		rows[i].HasAuth = rows[i].AuthCipher != ""
		rows[i].ReferenceCount = counts[rows[i].ID]
	}
	return rows, nil
}

func ProxyDelete(ctx context.Context, conn *gorm.DB, id int) error {
	proxyWriteMu.Lock()
	defer proxyWriteMu.Unlock()
	err := conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.ProxyConfiguration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return errors.New("proxy node does not exist")
		}
		refs, err := ProxyReferences(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(refs) != 0 {
			return ErrProxyConflict
		}
		if err := tx.Delete(&row).Error; err != nil {
			return fmt.Errorf("proxy could not be deleted: %w", ErrProxyConflict)
		}
		return nil
	})
	if err == nil {
		invalidateProxies()
	}
	return err
}
