// Package turnstile obtains Cloudflare Turnstile tokens from third-party
// solving platforms. It knows nothing about the sites that require them.
package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Platform string

const (
	PlatformCloudflareSolver Platform = "Cloudflare Solver"
	PlatformSolverQ          Platform = "SolverQ"
)

const (
	CloudflareSolverBaseURL = "https://cloudflare.pigeonw.com"
	SolverQBaseURL          = "https://solver.000.moe"

	// AttemptTimeout bounds one platform attempt, including polling. SolverQ
	// documents 15-30 seconds per task.
	AttemptTimeout = 90 * time.Second
	requestTimeout = 30 * time.Second
	responseLimit  = 64 << 10
)

// Poll intervals follow each platform's documented recommendation.
// Overridable so tests do not have to wait them out.
var (
	cloudflareSolverPollInterval = time.Second
	solverQPollInterval          = 3 * time.Second
)

// Solver is one enabled platform. An empty BaseURL uses the public address.
type Solver struct {
	Platform Platform
	APIKey   string
	BaseURL  string
}

func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Solve tries each solver in order and returns the first token. Errors name
// only the platform and its error code, never keys, tokens or raw responses.
func Solve(ctx context.Context, client *http.Client, solvers []Solver, pageURL, siteKey string) (string, error) {
	if len(solvers) == 0 {
		return "", errors.New("no solver platform is configured")
	}
	if client == nil {
		client = NewHTTPClient()
	}
	// Enforce redirect behavior even for caller-provided clients: a redirect
	// could forward the key to another host.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	failures := make([]string, 0, len(solvers))
	for _, solver := range solvers {
		token, err := solver.solve(ctx, &bounded, pageURL, siteKey)
		if err == nil {
			return token, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s: %v", solver.Platform, err))
	}
	return "", errors.New(strings.Join(failures, "; "))
}

func (s Solver) solve(ctx context.Context, client *http.Client, pageURL, siteKey string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, AttemptTimeout)
	defer cancel()
	switch s.Platform {
	case PlatformCloudflareSolver:
		return solveCloudflareSolver(ctx, client, s, pageURL, siteKey)
	case PlatformSolverQ:
		return solveSolverQ(ctx, client, s, pageURL, siteKey)
	default:
		return "", errors.New("unsupported platform")
	}
}

// taskReply covers the task, result and error replies of both platforms.
// errorDescription is ignored so third-party text never reaches check-in logs.
type taskReply struct {
	ErrorID   int    `json:"errorId"`
	ErrorCode string `json:"errorCode"`
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	Solution  struct {
		Token string `json:"token"`
	} `json:"solution"`
}

func do(ctx context.Context, client *http.Client, method, endpoint, authorization string, body any) (taskReply, error) {
	var reply taskReply
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return reply, errors.New("could not encode request")
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return reply, errors.New("invalid request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "octopus-turnstile/1")
	resp, err := client.Do(req)
	if err != nil {
		// Transport errors embed the request URL; report the failure only.
		var netErr net.Error
		if ctx.Err() != nil || (errors.As(err, &netErr) && netErr.Timeout()) {
			return reply, errors.New("request timed out")
		}
		return reply, errors.New("request failed")
	}
	defer resp.Body.Close()
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, responseLimit)).Decode(&reply)
	if decodeErr == nil {
		// The platform error code is more specific than the HTTP status.
		if err := reply.platformError(); err != nil {
			return reply, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return reply, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if decodeErr != nil {
		return reply, errors.New("invalid response")
	}
	return reply, nil
}

func (r taskReply) platformError() error {
	code := strings.TrimSpace(r.ErrorCode)
	if r.ErrorID == 0 && code == "" {
		return nil
	}
	if code == "" || len(code) > 64 {
		return fmt.Errorf("error %d", r.ErrorID)
	}
	return errors.New(code)
}
