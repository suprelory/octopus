package turnstile

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare Solver: GET /turnstile submits a task and GET /result?id= polls it.
// The key travels in the Authorization header.
func solveCloudflareSolver(ctx context.Context, client *http.Client, solver Solver, pageURL, siteKey string) (string, error) {
	baseURL := baseURLOr(solver.BaseURL, CloudflareSolverBaseURL)
	authorization := "Bearer " + strings.TrimSpace(solver.APIKey)
	query := url.Values{"url": {pageURL}, "sitekey": {siteKey}}
	created, err := do(ctx, client, http.MethodGet, baseURL+"/turnstile?"+query.Encode(), authorization, nil)
	if err != nil {
		return "", err
	}
	if created.TaskID == "" {
		return "", errors.New("platform returned no task ID")
	}
	resultURL := baseURL + "/result?" + url.Values{"id": {created.TaskID}}.Encode()
	return poll(ctx, cloudflareSolverPollInterval, func() (taskReply, error) {
		return do(ctx, client, http.MethodGet, resultURL, authorization, nil)
	})
}

// SolverQ: POST /createTask submits a task and POST /getTaskResult polls it.
// The key travels in the JSON body.
func solveSolverQ(ctx context.Context, client *http.Client, solver Solver, pageURL, siteKey string) (string, error) {
	baseURL := baseURLOr(solver.BaseURL, SolverQBaseURL)
	key := strings.TrimSpace(solver.APIKey)
	created, err := do(ctx, client, http.MethodPost, baseURL+"/createTask", "", map[string]any{
		"clientKey": key,
		"task": map[string]string{
			"type":       "TurnstileTaskProxyless",
			"websiteURL": pageURL,
			"websiteKey": siteKey,
		},
	})
	if err != nil {
		return "", err
	}
	if created.TaskID == "" {
		return "", errors.New("platform returned no task ID")
	}
	return poll(ctx, solverQPollInterval, func() (taskReply, error) {
		return do(ctx, client, http.MethodPost, baseURL+"/getTaskResult", "", map[string]string{"clientKey": key, "taskId": created.TaskID})
	})
}

// poll waits before every query; neither platform has a result right after
// the task is created.
func poll(ctx context.Context, interval time.Duration, fetch func() (taskReply, error)) (string, error) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", errors.New("no token received in time")
		case <-timer.C:
		}
		reply, err := fetch()
		if err != nil {
			return "", err
		}
		switch reply.Status {
		case "ready":
			if token := strings.TrimSpace(reply.Solution.Token); token != "" {
				return token, nil
			}
			return "", errors.New("platform returned an empty token")
		case "processing":
		default:
			return "", errors.New("unexpected task status")
		}
		timer.Reset(interval)
	}
}

func baseURLOr(value, fallback string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return fallback
	}
	return value
}
