package checkin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/turnstile"
	"golang.org/x/net/html"
)

const turnstileBudget = 60 * time.Second
const checkinOperationTimeout = 140 * time.Second // leaves time for rotation, audit and final saves before lease expiry

type turnstileSession struct {
	settings  model.CheckinTurnstileSettings
	remaining time.Duration
}

type requestToken struct {
	location, field, value string
	cookies                []*http.Cookie
}

func (r *adapterRuntime) initTurnstile(ctx context.Context) error {
	if r.turnstile != nil {
		return nil
	}
	settings, err := r.s.Store.TurnstileSettings(ctx)
	if err != nil {
		return err
	}
	r.turnstile = &turnstileSession{settings: settings, remaining: turnstileBudget}
	return nil
}

func (r *adapterRuntime) turnstileQueryTimeout(base time.Duration) time.Duration {
	if r.turnstile != nil && r.turnstile.settings.Config.Enabled && r.turnstile.remaining > 0 {
		return base + r.turnstile.remaining
	}
	return base
}

// A mention in a successful JSON reply is not a rejection. Cloudflare WAF/JS
// challenge pages without a Turnstile widget cannot be solved with this token API.
func isTurnstileChallenge(response HTTPResult) bool {
	if response.Err != nil || response.CloudflareChallenge {
		return false
	}
	data, err := decodeObject(response.Body)
	if err == nil {
		if response.StatusCode < 200 || response.StatusCode >= 500 || response.StatusCode == 429 {
			return false
		}
		if success, _ := field(data, "success"); success == true {
			return false
		}
		if code, _ := field(data, "code"); code == json.Number("0") {
			return false
		}
		if checked, _ := field(data, "data.checked_in"); checked == true {
			return false
		}
		if checked, _ := field(data, "checked_in"); checked == true {
			return false
		}
		mentioned := false
		for _, path := range []string{"message", "msg", "error", "error.message", "error.code", "code", "data.message"} {
			value, _ := field(data, path)
			message, _ := value.(string)
			mentioned = mentioned || strings.Contains(strings.ToLower(message), "turnstile")
		}
		if !mentioned {
			return false
		}
		if success, _ := field(data, "success"); success == false {
			return true
		}
		if value, _ := field(data, "error"); value != nil && value != "" {
			return true
		}
		switch response.StatusCode {
		case 400, 401, 403, 409, 422:
			return true
		}
		return false
	}
	// HTML widgets are accepted only on an explicit rejected HTTP response.
	// A normal 200 page may contain a widget after a successful login/check-in.
	switch response.StatusCode {
	case 400, 401, 403, 409, 422, 503:
	default:
		return false
	}
	lower := strings.ToLower(string(response.Body))
	if strings.Contains(lower, "_cf_chl_opt") || strings.Contains(lower, "/cdn-cgi/challenge-platform/") || strings.Contains(lower, "cf-chl-") {
		return false
	}
	if renderWidget.Match(response.Body) {
		return true
	}
	z := html.NewTokenizer(bytes.NewReader(response.Body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			for _, attr := range z.Token().Attr {
				if attr.Key == "class" {
					for _, class := range strings.Fields(attr.Val) {
						if class == "cf-turnstile" {
							return true
						}
					}
				}
			}
		}
	}
}

func (r *adapterRuntime) turnstileChallenge(response HTTPResult) bool {
	if !isTurnstileChallenge(response) {
		return false
	}
	// Respect custom success/already rules as well as explicit platform evidence.
	data, err := decodeObject(response.Body)
	if err == nil {
		for _, rule := range []*model.CheckinMatch{r.site.Config.Success, r.site.Config.Already} {
			if rule != nil && matches(data, *rule) {
				return false
			}
		}
		if interpretSubmit(r.site.Config, response.StatusCode, response.Body).Status == "already_checked_in" {
			return false
		}
	}
	return true
}

var renderWidget = regexp.MustCompile(`(?is)turnstile\s*\.\s*render\s*\([^,]{1,512},\s*\{([^}]{1,1000})\}`)
var widgetProperty = regexp.MustCompile(`(?i)["']?(sitekey|action|cdata)["']?\s*:\s*["']([a-z0-9_-]+)["']`)

func widgetParameters(body []byte) turnstile.Task {
	var task turnstile.Task
	if data, err := decodeObject(body); err == nil {
		for _, path := range []string{"data.turnstile_site_key", "turnstile_site_key", "sitekey", "site_key", "data.sitekey", "data.site_key", "error.sitekey", "error.site_key"} {
			if value, ok := field(data, path); ok {
				if key, ok := value.(string); ok && key != "" && turnstile.ValidWidgetValue(key, 256) {
					task.SiteKey = key
					break
				}
			}
		}
		for _, prefix := range []string{"", "data.", "error."} {
			if value, ok := field(data, prefix+"action"); ok {
				task.Action, _ = value.(string)
			}
			if value, ok := field(data, prefix+"cdata"); ok {
				task.CData, _ = value.(string)
			}
		}
		return validWidgetParameters(task)
	}
	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() != io.EOF {
				return turnstile.Task{}
			}
			// Parse static render options only; never fetch or execute page scripts.
			if match := renderWidget.FindSubmatch(body); len(match) > 1 {
				for _, property := range widgetProperty.FindAllSubmatch(match[1], -1) {
					switch strings.ToLower(string(property[1])) {
					case "sitekey":
						task.SiteKey = string(property[2])
					case "action":
						task.Action = string(property[2])
					case "cdata":
						task.CData = string(property[2])
					}
				}
			}
			return validWidgetParameters(task)
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			attrs := map[string]string{}
			for _, attr := range token.Attr {
				attrs[strings.ToLower(attr.Key)] = attr.Val
			}
			if !strings.Contains(attrs["class"], "cf-turnstile") {
				continue
			}
			task = validWidgetParameters(turnstile.Task{SiteKey: attrs["data-sitekey"], Action: attrs["data-action"], CData: attrs["data-cdata"]})
			if task.SiteKey != "" {
				return task
			}
		}
	}
}

func validWidgetParameters(task turnstile.Task) turnstile.Task {
	if !turnstile.ValidWidgetValue(task.SiteKey, 256) {
		task.SiteKey = ""
	}
	if !turnstile.ValidWidgetValue(task.Action, 32) {
		task.Action = ""
	}
	if !turnstile.ValidWidgetValue(task.CData, 255) {
		task.CData = ""
	}
	return task
}

func (r *adapterRuntime) solveTurnstile(ctx context.Context, response HTTPResult) (*requestToken, error) {
	if err := r.initTurnstile(ctx); err != nil {
		return nil, err
	}
	session := r.turnstile
	if !session.settings.Config.Enabled {
		return nil, needsAttention("turnstile_disabled")
	}
	if session.remaining <= 0 {
		return nil, needsAttention(string(turnstile.ErrTimeout))
	}
	started := time.Now()
	solveCtx, cancel := context.WithTimeout(ctx, session.remaining)
	defer func() { cancel(); session.remaining -= time.Since(started) }()
	options := model.CheckinTurnstileSite{}
	if r.site.Config.Turnstile != nil {
		options = *r.site.Config.Turnstile
	}
	if err := model.NormalizeCheckinTurnstileSite(&options); err != nil {
		return nil, needsAttention("turnstile_invalid_config")
	}
	page, err := checkinRequestURL(r.site.Config.BaseURL, options.PagePath, "")
	if err != nil {
		return nil, needsAttention("turnstile_invalid_config")
	}
	task := widgetParameters(response.Body)
	if options.SiteKey != "" {
		task.SiteKey = options.SiteKey
	}
	if task.SiteKey == "" {
		// Discovery is anonymous, stays on this origin/prefix and uses the account's
		// effective network route. It never submits the original request again.
		for _, path := range []string{"/api/status", options.PagePath} {
			if r.site.Config.Submit.Method == http.MethodGet {
				submit, submitErr := checkinRequestURL(r.site.Config.BaseURL, r.site.Config.Submit.Path, r.item.CheckinDate)
				public, publicErr := checkinRequestURL(r.site.Config.BaseURL, path, "")
				if submitErr != nil || publicErr != nil || submit.Path == public.Path {
					continue
				}
			}
			discoverCtx, stop := context.WithTimeout(solveCtx, 10*time.Second)
			at := time.Now()
			result := doRequestHTTP(discoverCtx, r.client, r.site.Config, model.CheckinRequest{Method: http.MethodGet, Path: path}, model.CheckinCredentials{}, "", false)
			stop()
			if err := r.audit(solveCtx, "turnstile_discover", result, time.Since(at)); err != nil {
				return nil, err
			}
			if result.Err == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
				discovered := widgetParameters(result.Body)
				if discovered.SiteKey != "" {
					task.SiteKey = discovered.SiteKey
					if task.Action == "" {
						task.Action = discovered.Action
					}
					if task.CData == "" {
						task.CData = discovered.CData
					}
					break
				}
			}
			if solveCtx.Err() != nil {
				return nil, needsAttention(string(turnstile.ErrTimeout))
			}
		}
	}
	if task.SiteKey == "" {
		return nil, needsAttention("turnstile_sitekey_missing")
	}
	task.PageURL = page.String()
	if options.Action != "" {
		task.Action = options.Action
	}
	if options.CData != "" {
		task.CData = options.CData
	}
	// Solver traffic has its own client: no site proxy credentials, cookies,
	// account headers, cookie jar, or environment proxy is forwarded to a provider.
	client, err := newHTTPClient("", 30*time.Second)
	if err != nil {
		return nil, needsAttention(string(turnstile.ErrNetwork))
	}
	defer client.CloseIdleConnections()
	providers := []struct {
		platform, stage string
		config          model.CheckinTurnstileProvider
		cipher          string
	}{}
	if session.settings.Config.CloudflareSolver.Enabled {
		providers = append(providers, struct {
			platform, stage string
			config          model.CheckinTurnstileProvider
			cipher          string
		}{turnstile.CloudflareSolver, "turnstile_cloudflare_solver", session.settings.Config.CloudflareSolver, session.settings.CloudflareKeyCipher})
	}
	if session.settings.Config.SolverQ.Enabled {
		providers = append(providers, struct {
			platform, stage string
			config          model.CheckinTurnstileProvider
			cipher          string
		}{turnstile.SolverQ, "turnstile_solverq", session.settings.Config.SolverQ, session.settings.SolverQKeyCipher})
	}
	reason := "turnstile_invalid_config"
	for i, provider := range providers {
		if err := r.turnstileRetryAllowed(solveCtx, false); err != nil {
			return nil, err
		}
		at := time.Now()
		key, keyErr := r.s.Vault.Unseal(model.TurnstileKeyAAD(provider.platform, provider.config.BaseURL), provider.cipher)
		var token string
		if keyErr != nil {
			err = turnstile.ErrKey
		} else {
			deadline, _ := solveCtx.Deadline()
			// Reserve time for fallback instead of letting the first provider consume
			// the entire remaining budget. All request stages share this same budget.
			providerCtx, stop := context.WithTimeout(solveCtx, time.Until(deadline)/time.Duration(len(providers)-i))
			token, err = turnstile.Solve(providerCtx, client, turnstile.Solver{Platform: provider.platform, BaseURL: provider.config.BaseURL, APIKey: string(key)}, task)
			stop()
		}
		reason = "turnstile_solved"
		if err != nil {
			reason = string(turnstile.ErrNetwork)
			var safeError turnstile.Error
			if errors.As(err, &safeError) {
				reason = string(safeError)
			}
		}
		if auditErr := r.auditReason(ctx, provider.stage, HTTPResult{}, time.Since(at), reason); auditErr != nil {
			return nil, auditErr
		}
		if err == nil {
			return &requestToken{location: options.TokenLocation, field: options.TokenField, value: token}, nil
		}
	}
	return nil, needsAttention(reason)
}

func (r *adapterRuntime) turnstileRetryAllowed(ctx context.Context, submit bool) error {
	if ctx.Err() != nil {
		return needsAttention(string(turnstile.ErrTimeout))
	}
	if r.item.JobID != "" {
		job, _, err := r.s.Store.Job(ctx, r.item.JobID)
		if err != nil {
			return err
		}
		if job.Status == "canceled" {
			return &adapterError{"skipped", "job_canceled"}
		}
	}
	if submit && (businessDate(r.site.Config, r.s.Now()) != r.run.CheckinDate || (r.source == "scheduled" && !inWindow(r.site.Config, r.s.Now()))) {
		return &adapterError{"skipped", "date_expired"}
	}
	return nil
}

func (r *adapterRuntime) markTurnstileRejection(ctx context.Context, stage string) error {
	if stage != "submit" || r.run == nil {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.s.Store.MarkTurnstileRejected(saveCtx, r.item, r.run, r.s.Now())
}
