// Package turnstile implements the two task APIs also supported by octopus-plus.
// It sends only public widget parameters to a solver, never site credentials.
package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	CloudflareSolver        = "cloudflare-solver"
	SolverQ                 = "solverq"
	CloudflareSolverBaseURL = "https://cloudflare.pigeonw.com"
	SolverQBaseURL          = "https://solver.000.moe"
)

// Error values are fixed codes safe to persist. Provider-controlled messages,
// URLs (which may contain tokens), and transport errors never escape this package.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrConfig     Error = "turnstile_invalid_config"
	ErrTimeout    Error = "turnstile_timeout"
	ErrNetwork    Error = "turnstile_solver_unavailable"
	ErrProtocol   Error = "turnstile_solver_response_invalid"
	ErrKey        Error = "turnstile_solver_key_invalid"
	ErrBalance    Error = "turnstile_solver_balance_insufficient"
	ErrBusy       Error = "turnstile_solver_busy"
	ErrUnsolvable Error = "turnstile_unsolvable"
)

type Task struct {
	PageURL string
	SiteKey string
	Action  string
	CData   string
}

type Solver struct {
	Platform string
	BaseURL  string
	APIKey   string
}

func ValidText(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

func ValidWidgetValue(value string, max int) bool {
	if len(value) > max {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func NormalizeBaseURL(raw, fallback string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(raw), "/")
	if value == "" {
		value = fallback
	}
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(value, "\\\r\n\x00") {
		return "", ErrConfig
	}
	return value, nil
}

type taskReply struct {
	ErrorID   *int   `json:"errorId"`
	ErrorCode string `json:"errorCode"`
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	Solution  struct {
		Token string `json:"token"`
	} `json:"solution"`
}

// Solve uses a single task and bounded polling. Callers own fallback and their
// shared operation budget. No token or task is cached or reused.
func Solve(ctx context.Context, client *http.Client, solver Solver, task Task) (string, error) {
	return solve(ctx, client, solver, task, 0)
}

func solve(ctx context.Context, client *http.Client, solver Solver, task Task, pollInterval time.Duration) (string, error) {
	fallback := CloudflareSolverBaseURL
	if solver.Platform == SolverQ {
		fallback = SolverQBaseURL
	} else if solver.Platform != CloudflareSolver {
		return "", ErrConfig
	}
	base, err := NormalizeBaseURL(solver.BaseURL, fallback)
	u, urlErr := url.Parse(task.PageURL)
	if err != nil || urlErr != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || !ValidText(solver.APIKey, 4096) || task.SiteKey == "" || !ValidWidgetValue(task.SiteKey, 256) || !ValidWidgetValue(task.Action, 32) || !ValidWidgetValue(task.CData, 255) || client == nil {
		return "", ErrConfig
	}
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	copyClient := *client
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 || copyClient.Timeout > 30*time.Second {
		copyClient.Timeout = 30 * time.Second
	}
	var created taskReply
	var fetch func() (taskReply, error)
	if solver.Platform == CloudflareSolver {
		query := url.Values{"url": {task.PageURL}, "sitekey": {task.SiteKey}}
		if task.Action != "" {
			query.Set("action", task.Action)
		}
		if task.CData != "" {
			query.Set("cdata", task.CData)
		}
		created, err = request(bounded, &copyClient, http.MethodGet, base+"/turnstile?"+query.Encode(), solver.APIKey, nil)
		fetch = func() (taskReply, error) {
			return request(bounded, &copyClient, http.MethodGet, base+"/result?"+url.Values{"id": {created.TaskID}}.Encode(), solver.APIKey, nil)
		}
		if pollInterval <= 0 {
			pollInterval = time.Second
		}
	} else {
		payload := map[string]any{"type": "TurnstileTaskProxyless", "websiteURL": task.PageURL, "websiteKey": task.SiteKey}
		metadata := map[string]string{}
		if task.Action != "" {
			metadata["action"] = task.Action
		}
		if task.CData != "" {
			metadata["cdata"] = task.CData
		}
		if len(metadata) > 0 {
			payload["metadata"] = metadata
		}
		created, err = request(bounded, &copyClient, http.MethodPost, base+"/createTask", "", map[string]any{"clientKey": solver.APIKey, "task": payload})
		fetch = func() (taskReply, error) {
			return request(bounded, &copyClient, http.MethodPost, base+"/getTaskResult", "", map[string]string{"clientKey": solver.APIKey, "taskId": created.TaskID})
		}
		if pollInterval <= 0 {
			pollInterval = 3 * time.Second
		}
	}
	if err != nil {
		return "", err
	}
	if created.ErrorID == nil || *created.ErrorID != 0 || !ValidText(created.TaskID, 256) {
		return "", ErrProtocol
	}
	timer := time.NewTimer(pollInterval)
	defer timer.Stop()
	for {
		select {
		case <-bounded.Done():
			return "", ErrTimeout
		case <-timer.C:
		}
		reply, err := fetch()
		if err != nil {
			return "", err
		}
		switch reply.Status {
		case "processing":
			// Cloudflare Solver legitimately omits errorId while processing.
			if solver.Platform == SolverQ && reply.ErrorID == nil {
				return "", ErrProtocol
			}
		case "ready":
			if reply.ErrorID == nil || *reply.ErrorID != 0 || !ValidText(reply.Solution.Token, 8192) {
				return "", ErrProtocol
			}
			return reply.Solution.Token, nil
		default:
			return "", ErrProtocol
		}
		timer.Reset(pollInterval)
	}
}

func request(ctx context.Context, client *http.Client, method, endpoint, key string, payload any) (taskReply, error) {
	var reply taskReply
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return reply, ErrConfig
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return reply, ErrTimeout
		}
		return reply, ErrNetwork
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil && (ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded)) {
		return reply, ErrTimeout
	}
	if err != nil || len(raw) > 64<<10 || json.Unmarshal(raw, &reply) != nil {
		return reply, ErrProtocol
	}
	if reply.ErrorCode != "" || (reply.ErrorID != nil && *reply.ErrorID != 0) {
		switch reply.ErrorCode {
		case "ERROR_KEY_DOES_NOT_EXIST", "ERROR_KEY_INVALID", "ERROR_INVALID_CLIENT_KEY":
			return reply, ErrKey
		case "ERROR_ZERO_BALANCE":
			return reply, ErrBalance
		case "ERROR_NO_SLOT_AVAILABLE", "ERROR_TOO_MUCH_REQUESTS", "ERROR_SERVICE_UNAVALIABLE", "ERROR_SERVICE_UNAVAILABLE":
			return reply, ErrBusy
		case "ERROR_CAPTCHA_UNSOLVABLE":
			return reply, ErrUnsolvable
		default:
			return reply, ErrProtocol
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return reply, ErrNetwork
	}
	return reply, nil
}
