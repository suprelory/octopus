package op

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/turnstile"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
)

func TestTurnstileBackupEncryptsValidatesAndRestoresDisabled(t *testing.T) {
	store, _ := checkinBackupFixture(t)
	vault, err := checkinsecret.Open()
	if err != nil {
		t.Fatal(err)
	}
	row := model.DefaultCheckinTurnstileSettings()
	row.Config.Enabled, row.Config.CloudflareSolver.Enabled = true, true
	row.CloudflareKeyCipher, err = vault.Seal(model.TurnstileKeyAAD(turnstile.CloudflareSolver, row.Config.CloudflareSolver.BaseURL), []byte("private-backup-solver-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveTurnstileSettings(context.Background(), row, 0, true, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	dump, err := exportCheckin(context.Background(), store.DB)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(dump)
	if strings.Contains(string(raw), "private-backup-solver-key") || !strings.Contains(string(raw), "cloudflare_key_cipher") || dump.Version != 7 {
		t.Fatal("backup exposed or omitted solver keys")
	}
	for _, mutate := range []func(*model.CheckinDump){
		func(d *model.CheckinDump) { d.Turnstile.CloudflareCipher = "broken-cipher" },
		func(d *model.CheckinDump) { d.Turnstile.Config.CloudflareSolver.BaseURL = "https://changed.example" },
		func(d *model.CheckinDump) { d.Turnstile.CloudflareCipher = "" },
		func(d *model.CheckinDump) { d.Version = 6 },
	} {
		var invalid model.CheckinDump
		_ = json.Unmarshal(raw, &invalid)
		mutate(&invalid)
		if validateCheckinDump(&invalid) == nil {
			t.Fatal("invalid Turnstile backup accepted")
		}
	}
	conn := checkinTestDB(t)
	var backup model.CheckinDump
	_ = json.Unmarshal(raw, &backup)
	result := restoreCheckinFixture(t, conn, &backup)
	restoredStore := NewCheckinStore(conn)
	restored, err := restoredStore.TurnstileSettings(context.Background())
	if err != nil || result.RowsAffected["checkin_turnstile_settings"] != 1 || restored.Config.Enabled || !restored.HasCloudflareKey {
		t.Fatal("restore enabled paid solving or lost the key", err)
	}
	plain, err := vault.Unseal(model.TurnstileKeyAAD(turnstile.CloudflareSolver, restored.Config.CloudflareSolver.BaseURL), restored.CloudflareKeyCipher)
	if err != nil || string(plain) != "private-backup-solver-key" {
		t.Fatal("restored key does not decrypt")
	}
	restored.Config.Enabled = true
	restored.CloudflareKeyCipher, _ = vault.Seal(model.TurnstileKeyAAD(turnstile.CloudflareSolver, restored.Config.CloudflareSolver.BaseURL), []byte("new-local-key"))
	updated, err := restoredStore.SaveTurnstileSettings(context.Background(), restored, restored.Revision, true, false, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &backup)
	result = restoreCheckinFixture(t, conn, &backup)
	current, _ := restoredStore.TurnstileSettings(context.Background())
	if result.RowsAffected["checkin_turnstile_settings"] != 0 || current.CloudflareKeyCipher != updated.CloudflareKeyCipher || !current.Config.Enabled {
		t.Fatal("repeat restore overwrote local solver settings")
	}
	backup.Version, backup.Turnstile = 6, nil
	if err := validateCheckinDump(&backup); err != nil {
		t.Fatal("old backups without solver settings are no longer accepted", err)
	}
}
