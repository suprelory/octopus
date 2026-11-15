package checkin

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// The caller holds Access, just as for the other management operations.
func (s *Service) CleanupJobs(ctx context.Context, filter model.CheckinCleanupFilter) (*model.CheckinCleanupReport, error) {
	if !s.cleanupMu.TryLock() {
		return nil, op.ErrCheckinConflict
	}
	defer s.cleanupMu.Unlock()
	return s.cleanupJobs(ctx, filter, "manual")
}

func (s *Service) cleanupJobs(ctx context.Context, filter model.CheckinCleanupFilter, source string) (*model.CheckinCleanupReport, error) {
	now := s.Now().UTC()
	filter, err := op.NormalizeCheckinCleanup(filter, now)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report := &model.CheckinCleanupReport{Source: source, Status: "running", StartedAt: now, Counts: model.CheckinCleanupCounts{Skipped: map[string]int64{}}}
	if err := s.Store.SaveCleanupReport(ctx, *report); err != nil {
		return nil, err
	}
	report.Counts, err = s.Store.CleanupJobs(ctx, filter, now)
	report.Status = "completed"
	if err != nil {
		report.Status = "failed"
		if report.Counts.Jobs > 0 {
			report.Status = "partial"
		}
	}
	finished := s.Now().UTC()
	report.FinishedAt = &finished
	// Preserve the actual committed counts even when the HTTP request is canceled.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	if err := s.Store.SaveCleanupReport(saveCtx, *report); err != nil {
		return nil, err
	}
	s.Notify("refresh")
	return report, nil
}

func (s *Service) CleanupStatus(ctx context.Context) (*model.CheckinCleanupReport, error) {
	// Holding the lock while reading prevents a newly started cleanup from being
	// mistaken for an interrupted cleanup left by a previous process.
	idle := s.cleanupMu.TryLock()
	if idle {
		defer s.cleanupMu.Unlock()
	}
	report, err := s.Store.CleanupReport(ctx)
	if err == nil && report != nil && report.Status == "running" && idle {
		report.Status = "interrupted"
	}
	return report, err
}

func (s *Service) AutoCleanup(ctx context.Context) error {
	if !s.cleanupMu.TryLock() {
		return nil
	}
	defer s.cleanupMu.Unlock()
	return s.Access(func() error {
		settings, err := s.Store.Settings(ctx)
		if err != nil || !settings.AutoCleanup {
			return err
		}
		if err := settings.Validate(); err != nil {
			return err
		}
		last, err := s.Store.CleanupReport(ctx)
		if err != nil {
			return err
		}
		now := s.Now().UTC()
		if last != nil && now.Sub(last.StartedAt) < 24*time.Hour {
			return nil
		}
		before := now.AddDate(0, 0, -settings.RetentionDays)
		report, err := s.cleanupJobs(ctx, model.CheckinCleanupFilter{Before: &before}, "automatic")
		if err == nil && report.Status != "completed" {
			return op.ErrCheckinBusy
		}
		return err
	})
}
