package model

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/bestruirui/octopus/internal/clientip"
)

type SettingKey string

const (
	SettingKeyProxyURL                         SettingKey = "proxy_url"
	SettingKeyTrustedProxies                   SettingKey = "trusted_proxies"                      // 可信反向代理 IP/CIDR 列表（逗号或换行分隔）
	SettingKeyStatsSaveInterval                SettingKey = "stats_save_interval"                  // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval          SettingKey = "model_info_update_interval"           // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval                  SettingKey = "sync_llm_interval"                    // LLM 同步间隔(小时)
	SettingKeySiteSyncInterval                 SettingKey = "site_sync_interval"                   // 站点账号同步间隔(小时)
	SettingKeySiteCheckinInterval              SettingKey = "site_checkin_interval"                // 已弃用：保留用于兼容旧配置
	SettingKeyRelayLogKeepPeriod               SettingKey = "relay_log_keep_period"                // 日志保存时间范围(天)
	SettingKeyRelayLogKeepEnabled              SettingKey = "relay_log_keep_enabled"               // 是否保留历史日志
	SettingKeyCORSAllowOrigins                 SettingKey = "cors_allow_origins"                   // 跨域白名单(逗号分隔的完整 origin, 如 "https://example.com,https://example2.com"). 为空不允许跨域, "*"允许所有来源但不下发凭证
	SettingKeyCircuitBreakerThreshold          SettingKey = "circuit_breaker_threshold"            // 熔断触发阈值（连续失败次数）
	SettingKeyCircuitBreakerCooldown           SettingKey = "circuit_breaker_cooldown"             // 熔断基础冷却时间（秒）
	SettingKeyCircuitBreakerMaxCooldown        SettingKey = "circuit_breaker_max_cooldown"         // 熔断最大冷却时间（秒），指数退避上限
	SettingKeyChannelAffinityEnabled           SettingKey = "channel_affinity_enabled"             // 是否优先复用同一 API Key/分组/模型上次成功的渠道
	SettingKeyChannelAffinityTTLSeconds        SettingKey = "channel_affinity_ttl_seconds"         // 渠道亲和记录 TTL（秒）
	SettingKeyEmptyResponseDetectionEnabled    SettingKey = "empty_response_detection_enabled"     // 是否全局启用空回检测
	SettingKeyRelayMaxChannelAttempts          SettingKey = "relay_max_channel_attempts"           // 单个 HTTP 请求最多尝试的候选渠道数
	SettingKeyRelayMaxTotalAttempts            SettingKey = "relay_max_total_attempts"             // 单个 HTTP 请求最多发起的上游尝试总数
	SettingKeyRelayFailoverTimeoutSeconds      SettingKey = "relay_failover_timeout_seconds"       // HTTP 故障转移总预算（秒）
	SettingKeyCapabilityDegradationPolicy      SettingKey = "capability_degradation_policy"        // 能力降级策略：allow/warn/strict
	SettingKeyResponsesWSEnabled               SettingKey = "responses_ws_enabled"                 // 是否启用 OpenAI Responses WS 上游能力（仅客户端 WS 入站）
	SettingKeyResponsesWSDefaultMode           SettingKey = "responses_ws_default_mode"            // OpenAI Responses WS 默认模式：off/transform/passthrough
	SettingKeySSEHeartbeatInterval             SettingKey = "sse_heartbeat_interval"               // SSE 流式心跳间隔（秒），0 表示禁用
	SettingKeySSEPreStreamHeartbeatDelay       SettingKey = "sse_pre_stream_heartbeat_delay"       // SSE 上游流建立前心跳首次延迟（秒），0 表示禁用
	SettingKeyProjectedChannelAutoGroupEnabled SettingKey = "projected_channel_auto_group_enabled" // 全局站点投影渠道自动分组模式（0关闭/1模糊/2精确/3正则，兼容旧 true/false）
	SettingKeyJWTSecret                        SettingKey = "jwt_secret"                           // JWT 签名密钥（自动生成）
	SettingKeyStatsSiteModelBackfilled         SettingKey = "stats_site_model_backfilled"          // 站点渠道小时聚合是否已回填历史日志
	SettingKeyApiBaseUrl                       SettingKey = "api_base_url"                         // 对外服务基础地址，用于一键导出客户端配置，为空时不显示导出入口
	SettingKeyWebDAVURL                        SettingKey = "webdav_url"                           // WebDAV 服务器地址
	SettingKeyWebDAVUsername                   SettingKey = "webdav_username"                      // WebDAV 用户名
	SettingKeyWebDAVPassword                   SettingKey = "webdav_password"                      // WebDAV 密码
	SettingKeyWebDAVBackupPath                 SettingKey = "webdav_backup_path"                   // WebDAV 远程备份目录
	SettingKeyWebDAVBackupInterval             SettingKey = "webdav_backup_interval"               // WebDAV 自动备份间隔(小时)，0=禁用
	SettingKeyWebDAVRetentionCount             SettingKey = "webdav_retention_count"               // WebDAV 保留备份份数
	SettingKeyWebDAVIncludeStats               SettingKey = "webdav_include_stats"                 // WebDAV 备份是否包含统计数据
)

type Setting struct {
	Key   SettingKey `json:"key" gorm:"primaryKey"`
	Value string     `json:"value" gorm:"not null"`
}

func DefaultSettings() []Setting {
	return []Setting{
		{Key: SettingKeyProxyURL, Value: ""},
		{Key: SettingKeyTrustedProxies, Value: ""},
		{Key: SettingKeyStatsSaveInterval, Value: "10"},               // 默认10分钟保存一次统计信息
		{Key: SettingKeyCORSAllowOrigins, Value: ""},                  // CORS 默认不允许跨域，设置为 "*" 才允许所有来源
		{Key: SettingKeyModelInfoUpdateInterval, Value: "24"},         // 默认24小时更新一次模型信息
		{Key: SettingKeySyncLLMInterval, Value: "24"},                 // 默认24小时同步一次LLM
		{Key: SettingKeySiteSyncInterval, Value: "12"},                // 默认12小时同步一次站点账号信息
		{Key: SettingKeySiteCheckinInterval, Value: "24"},             // 兼容旧版本；账号级调度不再读取此值
		{Key: SettingKeyRelayLogKeepPeriod, Value: "7"},               // 默认日志保存7天
		{Key: SettingKeyRelayLogKeepEnabled, Value: "true"},           // 默认保留历史日志
		{Key: SettingKeyCircuitBreakerThreshold, Value: "5"},          // 默认连续失败5次触发熔断
		{Key: SettingKeyCircuitBreakerCooldown, Value: "60"},          // 默认基础冷却60秒
		{Key: SettingKeyCircuitBreakerMaxCooldown, Value: "600"},      // 默认最大冷却600秒（10分钟）
		{Key: SettingKeyChannelAffinityEnabled, Value: "true"},        // 默认启用同 API Key/分组/模型的成功渠道亲和
		{Key: SettingKeyChannelAffinityTTLSeconds, Value: "3600"},     // 默认保留 1 小时
		{Key: SettingKeyEmptyResponseDetectionEnabled, Value: "true"}, // 默认启用空回检测
		{Key: SettingKeyRelayMaxChannelAttempts, Value: "4"},          // 默认最多尝试 4 个候选渠道
		{Key: SettingKeyRelayMaxTotalAttempts, Value: "12"},           // 默认最多发起 12 次上游转发尝试
		{Key: SettingKeyRelayFailoverTimeoutSeconds, Value: "300"},    // 默认故障转移预算 5 分钟
		{Key: SettingKeyCapabilityDegradationPolicy, Value: "warn"},   // 默认允许降级并写入诊断日志
		{Key: SettingKeyResponsesWSEnabled, Value: "false"},           // 默认关闭 OpenAI Responses WS 新路径
		{Key: SettingKeyResponsesWSDefaultMode, Value: "passthrough"}, // 启用后默认使用协议保真的 passthrough
		{Key: SettingKeySSEHeartbeatInterval, Value: "0"},             // 默认禁用 SSE 流式心跳
		{Key: SettingKeySSEPreStreamHeartbeatDelay, Value: "0"},       // 默认禁用 SSE 上游流建立前心跳
		{Key: SettingKeyProjectedChannelAutoGroupEnabled, Value: "0"}, // 默认不强制站点投影渠道自动分组
		{Key: SettingKeyJWTSecret, Value: ""},                         // 为空时自动生成
		{Key: SettingKeyStatsSiteModelBackfilled, Value: "false"},
		{Key: SettingKeyApiBaseUrl, Value: ""},                       // 默认为空，不显示客户端导出入口
		{Key: SettingKeyWebDAVURL, Value: ""},                        // 默认为空，未配置
		{Key: SettingKeyWebDAVUsername, Value: ""},                   // 默认为空
		{Key: SettingKeyWebDAVPassword, Value: ""},                   // 默认为空
		{Key: SettingKeyWebDAVBackupPath, Value: "/octopus-backups"}, // 默认远程目录
		{Key: SettingKeyWebDAVBackupInterval, Value: "0"},            // 默认禁用自动备份
		{Key: SettingKeyWebDAVRetentionCount, Value: "10"},           // 默认保留10份
		{Key: SettingKeyWebDAVIncludeStats, Value: "true"},           // 默认包含统计数据
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeySiteSyncInterval,
		SettingKeySiteCheckinInterval, SettingKeyRelayLogKeepPeriod,
		SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown, SettingKeyCircuitBreakerMaxCooldown:
		_, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("setting value must be an integer")
		}
		return nil
	case SettingKeyWebDAVRetentionCount, SettingKeyChannelAffinityTTLSeconds:
		return validateIntMin(s.Value, 1)
	case SettingKeyRelayMaxChannelAttempts:
		return validateIntRange(s.Value, 1, 64)
	case SettingKeyRelayMaxTotalAttempts:
		return validateIntRange(s.Value, 1, 256)
	case SettingKeyRelayFailoverTimeoutSeconds:
		return validateIntRange(s.Value, 1, 3600)
	case SettingKeySSEHeartbeatInterval, SettingKeySSEPreStreamHeartbeatDelay, SettingKeyWebDAVBackupInterval:
		value, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("setting value must be an integer")
		}
		if value < 0 {
			return fmt.Errorf("setting value must be non-negative")
		}
		return nil
	case SettingKeyRelayLogKeepEnabled, SettingKeyResponsesWSEnabled, SettingKeyStatsSiteModelBackfilled, SettingKeyWebDAVIncludeStats, SettingKeyChannelAffinityEnabled, SettingKeyEmptyResponseDetectionEnabled:
		if s.Value != "true" && s.Value != "false" {
			return fmt.Errorf("setting value must be true or false")
		}
		return nil
	case SettingKeyCapabilityDegradationPolicy:
		switch s.Value {
		case "allow", "warn", "strict":
			return nil
		default:
			return fmt.Errorf("setting value must be one of allow, warn, strict")
		}
	case SettingKeyProjectedChannelAutoGroupEnabled:
		if _, ok := ParseAutoGroupSettingValue(s.Value); !ok {
			return fmt.Errorf("setting value must be one of 0, 1, 2, 3, true, false")
		}
		return nil
	case SettingKeyResponsesWSDefaultMode:
		switch s.Value {
		case "off", "transform", "passthrough":
			return nil
		default:
			return fmt.Errorf("setting value must be one of off, transform, passthrough")
		}
	case SettingKeyProxyURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("proxy URL is invalid: %w", err)
		}
		validSchemes := map[string]bool{
			"http":   true,
			"https":  true,
			"socks5": true,
		}
		if !validSchemes[parsedURL.Scheme] {
			return fmt.Errorf("proxy URL scheme must be http, https, socks, or socks5")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("proxy URL must have a host")
		}
		return nil
	case SettingKeyTrustedProxies:
		_, normalized, err := clientip.ParseTrustedProxies(s.Value)
		if err != nil {
			return err
		}
		s.Value = normalized
		return nil
	case SettingKeyApiBaseUrl:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("api base URL is invalid: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("api base URL scheme must be http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("api base URL must have a host")
		}
		return nil
	case SettingKeyWebDAVURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("WebDAV URL is invalid: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("WebDAV URL scheme must be http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("WebDAV URL must have a host")
		}
		return nil
	}

	return nil
}

// validateIntMin 校验 v 为整数且不小于 lo。
func validateIntMin(v string, lo int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("setting value must be an integer")
	}
	if n < lo {
		return fmt.Errorf("setting value must be at least %d", lo)
	}
	return nil
}

// validateIntRange 校验 v 为整数且位于闭区间 [lo, hi]。
func validateIntRange(v string, lo, hi int) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("setting value must be an integer")
	}
	if n < lo || n > hi {
		return fmt.Errorf("setting value must be between %d and %d", lo, hi)
	}
	return nil
}
