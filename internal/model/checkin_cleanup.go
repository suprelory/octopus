package model

import "time"

// Exactly one selector is required. Before is a fixed instant so a preview and
// its subsequent confirmation use the same retention boundary.
type CheckinCleanupFilter struct {
	JobIDs []string   `json:"job_ids,omitempty"`
	Before *time.Time `json:"before,omitempty"`
}

type CheckinCleanupCounts struct {
	Matched  int64            `json:"matched"`
	Jobs     int64            `json:"jobs"`
	Items    int64            `json:"items"`
	Attempts int64            `json:"attempts"`
	Skipped  map[string]int64 `json:"skipped"`
}

type CheckinCleanupReport struct {
	Source     string               `json:"source"`
	Status     string               `json:"status"`
	StartedAt  time.Time            `json:"started_at"`
	FinishedAt *time.Time           `json:"finished_at,omitempty"`
	Counts     CheckinCleanupCounts `json:"counts"`
}
