package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func init() {
	r := router.NewGroupRouter("/api/v1/checkin").Use(middleware.Auth())
	add := func(path, method string, handler gin.HandlerFunc) {
		r.AddRoute(router.NewRoute(path, method).Handle(handler))
	}
	add("/sites", http.MethodGet, checkinSites)
	add("/sites", http.MethodPost, checkinSaveSite)
	add("/sites/:id", http.MethodGet, checkinSite)
	add("/sites/:id", http.MethodPatch, checkinSaveSite)
	add("/sites/:id", http.MethodDelete, checkinDeleteSite)
	add("/sites/:id/accounts", http.MethodGet, checkinAccounts)
	add("/sites/:id/accounts", http.MethodPost, checkinCreateAccount)
	add("/accounts/:id", http.MethodPatch, checkinSaveAccount)
	add("/accounts/:id/credentials", http.MethodPut, checkinSaveAccount)
	add("/accounts/:id", http.MethodDelete, checkinDeleteAccount)
	add("/accounts/:id/checkin", http.MethodPost, checkinSingle)
	add("/accounts/:id/balance", http.MethodPost, checkinBalance)
	add("/accounts/:id/detect", http.MethodPost, checkinDetectAccount)
	add("/jobs", http.MethodGet, checkinJobs)
	add("/jobs", http.MethodPost, checkinCreateJob)
	add("/jobs/:id", http.MethodGet, checkinJob)
	add("/jobs/:id/cancel", http.MethodPost, checkinCancelJob)
	add("/runs", http.MethodGet, checkinRuns)
	add("/runs/:id", http.MethodGet, checkinRun)
	add("/runs/:id/attempts", http.MethodGet, checkinAttempts)
	add("/runs/:id/confirm", http.MethodPost, checkinConfirm)
	add("/stats", http.MethodGet, checkinStats)
	add("/capabilities", http.MethodGet, func(c *gin.Context) { resp.Success(c, checkin.Capabilities()) })
	add("/settings", http.MethodGet, checkinSettings)
	add("/settings", http.MethodPatch, checkinSaveSettings)
	add("/turnstile", http.MethodGet, checkinTurnstileSettings)
	add("/turnstile", http.MethodPatch, checkinSaveTurnstile)
	add("/rules/test", http.MethodPost, checkinRules)
	add("/events", http.MethodGet, checkinEvents)
	add("/notifications", http.MethodGet, checkinNotifications)
	add("/notifications", http.MethodPost, checkinSaveNotification)
	add("/notifications/:id", http.MethodPatch, checkinSaveNotification)
	add("/notifications/:id", http.MethodDelete, checkinDeleteNotification)
	add("/notifications/:id/test", http.MethodPost, checkinTestNotification)
	add("/notification-preview", http.MethodPost, checkinPreviewNotification)
	add("/notification-deliveries", http.MethodGet, checkinNotificationDeliveries)
	add("/notification-deliveries/:id/retry", http.MethodPost, checkinRetryNotification)
}

func checkinJSON(c *gin.Context, value any) error {
	if c.ContentType() != "application/json" {
		return model.InvalidCheckin("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return model.InvalidCheckin("invalid JSON or unknown request fields")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return model.InvalidCheckin("expected one JSON object")
	}
	return nil
}

func withCheckin(c *gin.Context, status int, fn func(*checkin.Service) (any, error)) {
	if checkin.Default == nil {
		resp.Error(c, http.StatusServiceUnavailable, "checkin service is unavailable")
		return
	}
	if id := c.Param("id"); id != "" {
		if _, err := uuid.Parse(id); err != nil {
			resp.Error(c, 400, "invalid ID")
			return
		}
	}
	var data any
	err := checkin.Default.Access(func() error { var err error; data, err = fn(checkin.Default); return err })
	if err != nil {
		code, message := http.StatusInternalServerError, "checkin operation failed"
		var inputError *model.CheckinInputError
		switch {
		case errors.As(err, &inputError):
			code, message = http.StatusBadRequest, inputError.Error()
		case errors.Is(err, gorm.ErrRecordNotFound):
			code, message = http.StatusNotFound, "checkin record not found"
		case errors.Is(err, op.ErrCheckinConflict):
			code, message = http.StatusConflict, op.ErrCheckinConflict.Error()
		case errors.Is(err, op.ErrCheckinBusy):
			code, message = http.StatusServiceUnavailable, op.ErrCheckinBusy.Error()
		}
		resp.Error(c, code, message)
		return
	}
	if status == http.StatusNoContent {
		c.Status(status)
	} else {
		c.JSON(status, resp.ResponseStruct{Code: status, Message: "success", Data: data})
	}
}

func checkinSites(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Sites(c.Request.Context()) })
}
func checkinSite(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Site(c.Request.Context(), c.Param("id")) })
}
func checkinSaveSite(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var input struct {
			Config   model.CheckinSiteConfig `json:"config"`
			Revision int64                   `json:"revision"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		if c.Param("id") != "" && input.Revision <= 0 {
			return nil, op.ErrCheckinConflict
		}
		row, err := s.Store.SaveSite(c.Request.Context(), c.Param("id"), input.Revision, input.Config)
		if err == nil {
			s.Notify("site")
		}
		return row, err
	})
}
func checkinDeleteSite(c *gin.Context) {
	withCheckin(c, 204, func(s *checkin.Service) (any, error) {
		err := s.Store.Delete(c.Request.Context(), c.Param("id"), "")
		if err == nil {
			s.Notify("site")
		}
		return nil, err
	})
}
func checkinAccounts(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		if _, err := s.Store.Site(c.Request.Context(), c.Param("id")); err != nil {
			return nil, err
		}
		return s.Store.Accounts(c.Request.Context(), c.Param("id"))
	})
}
func checkinCreateAccount(c *gin.Context) { checkinWriteAccount(c, true) }
func checkinSaveAccount(c *gin.Context)   { checkinWriteAccount(c, false) }
func checkinWriteAccount(c *gin.Context, create bool) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var input checkin.AccountWrite
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		siteID, id := "", c.Param("id")
		if create {
			siteID, id = id, ""
		}
		row, err := s.SaveAccount(c.Request.Context(), siteID, id, input)
		if err == nil {
			s.Notify("account")
		}
		return row, err
	})
}
func checkinDeleteAccount(c *gin.Context) {
	withCheckin(c, 204, func(s *checkin.Service) (any, error) {
		err := s.Store.Delete(c.Request.Context(), "", c.Param("id"))
		if err == nil {
			s.Notify("account")
		}
		return nil, err
	})
}
func checkinSingle(c *gin.Context) {
	withCheckin(c, 202, func(s *checkin.Service) (any, error) {
		return s.Enqueue(c.Request.Context(), model.CheckinScope{Kind: "accounts", IDs: []string{c.Param("id")}}, "manual", c.GetHeader("Idempotency-Key"))
	})
}
func checkinBalance(c *gin.Context)       { checkinInspectAccount(c, "balance") }
func checkinDetectAccount(c *gin.Context) { checkinInspectAccount(c, "detect") }
func checkinInspectAccount(c *gin.Context, kind string) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		// Reject configurable paths, rules or credentials on this read-only action.
		if c.Request.ContentLength != 0 {
			var input struct{}
			if err := checkinJSON(c, &input); err != nil {
				return nil, err
			}
		}
		return s.InspectAccount(c.Request.Context(), c.Param("id"), kind)
	})
}
func checkinCreateJob(c *gin.Context) {
	withCheckin(c, 202, func(s *checkin.Service) (any, error) {
		var scope model.CheckinScope
		if err := checkinJSON(c, &scope); err != nil {
			return nil, err
		}
		return s.Enqueue(c.Request.Context(), scope, "manual", c.GetHeader("Idempotency-Key"))
	})
}
func checkinJobs(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Jobs(c.Request.Context()) })
}
func checkinJob(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		job, items, err := s.Store.Job(c.Request.Context(), c.Param("id"))
		return gin.H{"job": job, "items": items}, err
	})
}
func checkinCancelJob(c *gin.Context) {
	withCheckin(c, 204, func(s *checkin.Service) (any, error) {
		err := s.Store.CancelJob(c.Request.Context(), c.Param("id"))
		if err == nil {
			s.Notify("job")
		}
		return nil, err
	})
}
func checkinFilter(c *gin.Context) (op.CheckinRunFilter, error) {
	f := op.CheckinRunFilter{SiteID: c.Query("site_id"), AccountID: c.Query("account_id"), Status: c.Query("status"), Before: c.Query("before"), From: c.Query("from"), Until: c.Query("until"), Limit: 50}
	for _, id := range []string{f.SiteID, f.AccountID, f.Before} {
		if id != "" {
			if _, err := uuid.Parse(id); err != nil {
				return f, model.InvalidCheckin("invalid filter ID")
			}
		}
	}
	for _, date := range []string{f.From, f.Until} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return f, model.InvalidCheckin("invalid date filter")
			}
		}
	}
	if f.From != "" && f.Until != "" && f.From > f.Until {
		return f, model.InvalidCheckin("invalid date range")
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return f, model.InvalidCheckin("limit must be between 1 and 100")
		}
		f.Limit = n
	}
	return f, nil
}
func checkinRuns(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		f, err := checkinFilter(c)
		if err != nil {
			return nil, err
		}
		items, err := s.Store.Runs(c.Request.Context(), f)
		next := ""
		if len(items) == f.Limit {
			next = items[len(items)-1].ID
		}
		return gin.H{"items": items, "next_before": next}, err
	})
}
func checkinRun(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Run(c.Request.Context(), c.Param("id")) })
}
func checkinAttempts(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Attempts(c.Request.Context(), c.Param("id")) })
}
func checkinConfirm(c *gin.Context) {
	withCheckin(c, 202, func(s *checkin.Service) (any, error) {
		return s.Confirm(c.Request.Context(), c.Param("id"), c.GetHeader("Idempotency-Key"), false)
	})
}
func checkinStats(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		f, err := checkinFilter(c)
		if err != nil {
			return nil, err
		}
		return s.Stats(c.Request.Context(), f.SiteID, f.AccountID)
	})
}
func checkinSettings(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.Settings(c.Request.Context()) })
}
func checkinSaveSettings(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var settings model.CheckinSettings
		if err := checkinJSON(c, &settings); err != nil {
			return nil, err
		}
		err := s.Store.SaveSettings(c.Request.Context(), settings)
		if err == nil {
			s.Notify("settings")
		}
		return settings, err
	})
}
func checkinRules(c *gin.Context) {
	withCheckin(c, 200, func(_ *checkin.Service) (any, error) {
		var input struct {
			Config     model.CheckinSiteConfig `json:"config"`
			HTTPStatus int                     `json:"http_status"`
			Response   json.RawMessage         `json:"response"`
			Date       string                  `json:"date"`
		}
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		if _, err := time.Parse("2006-01-02", input.Date); err != nil {
			return nil, model.InvalidCheckin("provide a target date")
		}
		if input.HTTPStatus < 100 || input.HTTPStatus > 599 {
			return nil, model.InvalidCheckin("invalid HTTP status")
		}
		return checkin.TestRules(input.Config, input.HTTPStatus, input.Response, input.Date)
	})
}
func checkinEvents(c *gin.Context) {
	service := checkin.Default
	if service == nil {
		resp.Error(c, 503, "checkin service is unavailable")
		return
	}
	updates, closeStream := service.Subscribe()
	defer closeStream()
	prepareSSE(c)
	if err := sse.Encode(c.Writer, sse.Event{Event: "checkin", Data: checkin.Event{Kind: "refresh"}}); err != nil {
		return
	}
	c.Writer.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			c.Writer.Flush()
		case update, ok := <-updates:
			if !ok {
				return
			}
			if err := sse.Encode(c.Writer, sse.Event{Event: "checkin", Data: update}); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
