package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	TaskPriceUpdate         = "price_update"
	TaskStatsSave           = "stats_save"
	TaskRelayLogSave        = "relay_log_save"
	TaskSyncLLM             = "sync_llm"
	TaskCleanLLM            = "clean_llm"
	TaskBaseUrlDelay        = "base_url_delay"
	TaskSiteSync            = "site_sync"
	TaskSiteCheckin         = "site_checkin"
	TaskWSAffinityCleanup   = "ws_affinity_cleanup"
	TaskWebDAVBackup        = "webdav_backup"
	SiteCheckinScanInterval = 10 * time.Minute
)

func Init() {
	priceUpdateIntervalHours, err := op.SettingGetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		log.Warnf("failed to get model info update interval, using 24h fallback: %v", err)
		priceUpdateIntervalHours = 24
	}
	priceUpdateInterval := time.Duration(priceUpdateIntervalHours) * time.Hour
	// 注册价格更新任务
	Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {
		if err := price.UpdateLLMPrice(context.Background()); err != nil {
			log.Warnf("failed to update price info: %v", err)
		}
	})

	// 注册基础URL延迟任务
	Register(TaskBaseUrlDelay, 24*time.Hour, true, ChannelBaseUrlDelayTask)

	// 注册LLM同步任务
	syncLLMIntervalHours, err := op.SettingGetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		log.Warnf("failed to get sync LLM interval, using 24h fallback: %v", err)
		syncLLMIntervalHours = 24
	}
	syncLLMInterval := time.Duration(syncLLMIntervalHours) * time.Hour
	Register(string(model.SettingKeySyncLLMInterval), syncLLMInterval, true, SyncModelsTask)

	siteSyncIntervalHours, err := op.SettingGetInt(model.SettingKeySiteSyncInterval)
	if err != nil {
		log.Warnf("failed to get site sync interval, using 12h fallback: %v", err)
		siteSyncIntervalHours = 12
	}
	siteSyncInterval := time.Duration(siteSyncIntervalHours) * time.Hour
	Register(string(model.SettingKeySiteSyncInterval), siteSyncInterval, true, SiteSyncTask)

	// 签到任务只负责高频扫描；每个账号的实际执行时间由 next_auto_checkin_at 控制。
	Register(TaskSiteCheckin, SiteCheckinScanInterval, true, SiteCheckinTask)

	// 注册统计保存任务
	statsSaveIntervalMinutes, err := op.SettingGetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		log.Warnf("failed to get stats save interval, using 10m fallback: %v", err)
		statsSaveIntervalMinutes = 10
	}
	statsSaveInterval := time.Duration(statsSaveIntervalMinutes) * time.Minute
	Register(TaskStatsSave, statsSaveInterval, false, op.StatsSaveDBTask)
	// 注册中继日志保存任务
	Register(TaskRelayLogSave, time.Hour, false, func() {
		if err := op.RelayLogSaveDBTask(context.Background()); err != nil {
			log.Warnf("relay log save db task failed: %v", err)
		}
	})

	Register(TaskWSAffinityCleanup, 10*time.Minute, false, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		deleted, err := op.WSResponseAffinityCleanup(ctx, time.Now())
		if err != nil {
			log.Warnf("ws response affinity cleanup failed: %v", err)
			return
		}
		if deleted > 0 {
			log.Debugf("ws response affinity cleanup removed %d expired rows", deleted)
		}
	})

	// 注册 WebDAV 自动备份任务（间隔为 0 时不运行）
	webdavIntervalHours, err := op.SettingGetInt(model.SettingKeyWebDAVBackupInterval)
	if err != nil {
		log.Warnf("failed to get webdav backup interval: %v", err)
	} else if webdavIntervalHours > 0 {
		webdavInterval := time.Duration(webdavIntervalHours) * time.Hour
		Register(string(model.SettingKeyWebDAVBackupInterval), webdavInterval, false, WebDAVBackupTask)
	}
}
