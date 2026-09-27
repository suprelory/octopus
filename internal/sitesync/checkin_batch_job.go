package sitesync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
)

var (
	checkinBatchTriggerMu sync.Mutex
)

func StartCheckinBatch(ctx context.Context) (*model.SiteCheckinBatchJob, error) {
	checkinBatchTriggerMu.Lock()
	defer checkinBatchTriggerMu.Unlock()

	active, err := op.SiteCheckinBatchJobActive(ctx)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return active, nil
	}

	now := time.Now().UTC()
	job := &model.SiteCheckinBatchJob{
		ID: snowflake.GenerateID(), Status: model.SiteCheckinBatchJobStatusQueued,
		Trigger: string(SiteBatchTriggerManual), StartedAt: now, UpdatedAt: now,
	}
	if err := op.SiteCheckinBatchJobCreate(ctx, job); err != nil {
		return nil, err
	}

	safe.Go(fmt.Sprintf("site-checkin-batch:%d", job.ID), func() {
		runCheckinBatchJob(job.ID)
	})
	return job, nil
}

func runCheckinBatchJob(taskID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	if err := updateCheckinBatchJob(taskID, map[string]any{
		"status": model.SiteCheckinBatchJobStatusRunning,
	}); err != nil {
		log.Warnf("site checkin batch %d failed to enter running state: %v", taskID, err)
		_ = updateCheckinBatchJob(taskID, map[string]any{
			"status":        model.SiteCheckinBatchJobStatusFailed,
			"error_message": "batch could not be started",
			"finished_at":   time.Now().UTC(),
		})
		return
	}

	completed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Errorf("site checkin batch %d panicked: %v", taskID, recovered)
			_ = updateCheckinBatchJob(taskID, map[string]any{
				"status":        model.SiteCheckinBatchJobStatusFailed,
				"error_message": "batch failed unexpectedly",
				"finished_at":   time.Now().UTC(),
			})
			return
		}
		if !completed {
			_ = updateCheckinBatchJob(taskID, map[string]any{
				"status":        model.SiteCheckinBatchJobStatusFailed,
				"error_message": "batch stopped before it could report a result",
				"finished_at":   time.Now().UTC(),
			})
		}
	}()

	summary := CheckinAllWithOptions(ctx, SiteBatchOptions{
		Trigger: SiteBatchTriggerManual,
		TaskID:  taskID,
		OnProgress: func(progress SiteBatchProgress) {
			persistCheckinBatchProgress(progress)
		},
	})

	status := model.SiteCheckinBatchJobStatusCompleted
	if summary.Canceled {
		status = model.SiteCheckinBatchJobStatusCanceled
	} else if summary.ErrorMessage != "" {
		status = model.SiteCheckinBatchJobStatusFailed
	} else if summary.Failed > 0 || summary.Partial > 0 {
		status = model.SiteCheckinBatchJobStatusCompletedWithErrors
	}
	updates := map[string]any{
		"status":        status,
		"total":         summary.Total,
		"attempted":     summary.Attempted,
		"success":       summary.Success,
		"partial":       summary.Partial,
		"failed":        summary.Failed,
		"skipped":       summary.Skipped,
		"warnings":      summary.Warnings,
		"canceled":      summary.Canceled,
		"cancel_reason": string(summary.CancelReason),
		"duration_ms":   summary.Duration.Milliseconds(),
		"error_message": summary.ErrorMessage,
		"finished_at":   time.Now().UTC(),
	}
	if err := updateCheckinBatchJob(taskID, updates); err != nil {
		log.Warnf("site checkin batch %d final update failed: %v", taskID, err)
		return
	}
	completed = true
}

func persistCheckinBatchProgress(progress SiteBatchProgress) {
	if progress.TaskID <= 0 {
		return
	}
	updates := map[string]any{
		"total":                progress.Total,
		"attempted":            progress.Attempted,
		"success":              progress.Success,
		"partial":              progress.Partial,
		"failed":               progress.Failed,
		"skipped":              progress.Skipped,
		"warnings":             progress.Warnings,
		"canceled":             progress.Canceled,
		"cancel_reason":        string(progress.CancelReason),
		"current_site_id":      progress.CurrentSiteID,
		"current_site_name":    progress.CurrentSiteName,
		"current_account_id":   progress.CurrentAccountID,
		"current_account_name": progress.CurrentAccountName,
	}
	if err := updateCheckinBatchJob(progress.TaskID, updates); err != nil {
		log.Warnf("site checkin batch %d progress update failed: %v", progress.TaskID, err)
	}
}

func updateCheckinBatchJob(taskID int64, updates map[string]any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return op.SiteCheckinBatchJobUpdate(ctx, taskID, updates)
}
