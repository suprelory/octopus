package op

import (
	"errors"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/turnstile"
	"github.com/bestruirui/octopus/internal/utils/checkinsecret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validateTurnstileDump(dump *model.CheckinDump) error {
	row := dump.Turnstile
	if row == nil {
		return nil
	}
	if dump.Version < 7 || row.Revision < 1 || row.UpdatedAt.IsZero() {
		return errors.New("invalid Turnstile backup metadata")
	}
	if err := model.NormalizeCheckinTurnstile(&row.Config); err != nil {
		return err
	}
	if row.Config.Enabled && ((row.Config.CloudflareSolver.Enabled && row.CloudflareCipher == "") || (row.Config.SolverQ.Enabled && row.SolverQCipher == "")) {
		return errors.New("Turnstile backup is missing an enabled solver key")
	}
	for _, key := range []struct{ cipher, platform, baseURL string }{
		{row.CloudflareCipher, turnstile.CloudflareSolver, row.Config.CloudflareSolver.BaseURL},
		{row.SolverQCipher, turnstile.SolverQ, row.Config.SolverQ.BaseURL},
	} {
		if key.cipher == "" {
			continue
		}
		if len(key.cipher) > 16384 {
			return errors.New("invalid encrypted Turnstile key")
		}
		vault, err := checkinsecret.Open()
		if err != nil {
			return err
		}
		plain, err := vault.Unseal(model.TurnstileKeyAAD(key.platform, key.baseURL), key.cipher)
		if err != nil || !turnstile.ValidText(string(plain), 4096) {
			return errors.New("Turnstile backup key could not be decrypted")
		}
	}
	return nil
}

func importTurnstile(tx *gorm.DB, dump *model.CheckinDump, result *model.DBImportResult) error {
	if dump.Turnstile == nil {
		return nil
	}
	row := dump.Turnstile.CheckinTurnstileSettings
	row.ID, row.Config.Enabled = 1, false
	row.CloudflareKeyCipher, row.SolverQKeyCipher = dump.Turnstile.CloudflareCipher, dump.Turnstile.SolverQCipher
	// Restore never enables paid solving and never overwrites existing local keys.
	r := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	result.RowsAffected["checkin_turnstile_settings"] += r.RowsAffected
	return r.Error
}
