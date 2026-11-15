package handlers

import (
	"strconv"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func checkinNotifications(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.NotificationTargets(c.Request.Context()) })
}

func checkinSaveNotification(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var input checkin.NotificationWrite
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		return s.SaveNotification(c.Request.Context(), c.Param("id"), input)
	})
}

func checkinDeleteNotification(c *gin.Context) {
	withCheckin(c, 204, func(s *checkin.Service) (any, error) {
		var input struct {
			Revision int64 `json:"revision"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		err := s.Store.DeleteNotificationTarget(c.Request.Context(), c.Param("id"), input.Revision, s.Now())
		if err == nil {
			s.Notify("notification")
		}
		return nil, err
	})
}

func checkinTestNotification(c *gin.Context) {
	withCheckin(c, 202, func(s *checkin.Service) (any, error) {
		var input struct {
			Revision int64 `json:"revision"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		row, err := s.Store.TestNotification(c.Request.Context(), c.Param("id"), input.Revision, s.Now())
		if err == nil {
			s.Notify("notification")
		}
		return row, err
	})
}

func checkinPreviewNotification(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var input struct {
			Language string `json:"language"`
			Title    string `json:"title_template"`
			Body     string `json:"body_template"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		if input.Language != "en" && input.Language != "zh_hans" && input.Language != "zh_hant" {
			return nil, model.InvalidCheckin("invalid notification language")
		}
		title, message, err := notify.Render(model.CheckinNotificationConfig{Language: input.Language, TitleTemplate: input.Title, BodyTemplate: input.Body}, op.NotificationTestEvent(s.Now()))
		return gin.H{"title": title, "message": message}, err
	})
}

func checkinNotificationDeliveries(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		filter := op.NotificationDeliveryFilter{TargetID: c.Query("target_id"), Before: c.Query("before"), Limit: 50}
		for _, id := range []string{filter.TargetID, filter.Before} {
			if id != "" {
				if _, err := uuid.Parse(id); err != nil {
					return nil, model.InvalidCheckin("invalid notification filter ID")
				}
			}
		}
		if raw := c.Query("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 100 {
				return nil, model.InvalidCheckin("limit must be between 1 and 100")
			}
			filter.Limit = value
		}
		return s.Store.NotificationDeliveries(c.Request.Context(), filter)
	})
}

func checkinRetryNotification(c *gin.Context) {
	withCheckin(c, 202, func(s *checkin.Service) (any, error) {
		var input struct {
			Attempts int   `json:"attempts"`
			Revision int64 `json:"target_revision"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		row, err := s.Store.RetryNotification(c.Request.Context(), c.Param("id"), input.Attempts, input.Revision, s.Now())
		if err == nil {
			s.Notify("notification")
		}
		return row, err
	})
}
