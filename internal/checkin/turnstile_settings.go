package checkin

import (
	"context"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/turnstile"
)

type TurnstileWrite struct {
	Config   model.CheckinTurnstileConfig `json:"config"`
	Revision int64                        `json:"revision"`
	// nil preserves a stored key, an explicit empty string clears it.
	CloudflareKey *string `json:"cloudflare_key"`
	SolverQKey    *string `json:"solverq_key"`
}

func (s *Service) SaveTurnstile(ctx context.Context, input TurnstileWrite) (model.CheckinTurnstileSettings, error) {
	row := model.CheckinTurnstileSettings{Config: input.Config}
	if err := model.NormalizeCheckinTurnstile(&row.Config); err != nil {
		return row, err
	}
	for _, key := range []struct {
		value             *string
		cipher            *string
		platform, baseURL string
	}{
		{input.CloudflareKey, &row.CloudflareKeyCipher, turnstile.CloudflareSolver, row.Config.CloudflareSolver.BaseURL},
		{input.SolverQKey, &row.SolverQKeyCipher, turnstile.SolverQ, row.Config.SolverQ.BaseURL},
	} {
		if key.value == nil {
			continue
		}
		plain := strings.TrimSpace(*key.value)
		if plain == "" {
			continue
		}
		if !turnstile.ValidText(plain, 4096) {
			return row, model.InvalidCheckin("invalid Turnstile solver API key")
		}
		cipher, err := s.Vault.Seal(model.TurnstileKeyAAD(key.platform, key.baseURL), []byte(plain))
		if err != nil {
			return row, err
		}
		*key.cipher = cipher
	}
	saved, err := s.Store.SaveTurnstileSettings(ctx, row, input.Revision, input.CloudflareKey != nil, input.SolverQKey != nil, s.Now())
	if err == nil {
		s.Notify("settings")
	}
	return saved, err
}
