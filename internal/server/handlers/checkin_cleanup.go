package handlers

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/gin-gonic/gin"
)

func checkinPreviewCleanup(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var filter model.CheckinCleanupFilter
		if err := checkinJSON(c, &filter); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
		defer cancel()
		return s.Store.PreviewCleanup(ctx, filter, s.Now().UTC())
	})
}

func checkinCleanupJobs(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var filter model.CheckinCleanupFilter
		if err := checkinJSON(c, &filter); err != nil {
			return nil, err
		}
		return s.CleanupJobs(c.Request.Context(), filter)
	})
}

func checkinCleanupStatus(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		return s.CleanupStatus(c.Request.Context())
	})
}
