package model

import (
	"math"
	"strconv"
	"testing"
	"time"
)

func TestTaskIntervalSettingsRejectInvalidAndOverflowingValues(t *testing.T) {
	for _, key := range []SettingKey{SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeySiteSyncInterval, SettingKeyWebDAVBackupInterval, SettingKeyStatsSaveInterval} {
		unit := time.Hour
		if key == SettingKeyStatsSaveInterval {
			unit = time.Minute
		}
		maximum := int64(math.MaxInt64) / int64(unit)
		for _, value := range []string{"0", "1", strconv.FormatInt(maximum, 10)} {
			setting := Setting{Key: key, Value: value}
			if err := setting.Validate(); err != nil {
				t.Fatalf("%s rejected %s: %v", key, value, err)
			}
		}
		for _, value := range []string{"-1", "1.5", "bad", strconv.FormatInt(maximum+1, 10), "9223372036854775808"} {
			setting := Setting{Key: key, Value: value}
			if err := setting.Validate(); err == nil {
				t.Fatalf("%s accepted invalid/overflowing interval %s", key, value)
			}
		}
	}
}
