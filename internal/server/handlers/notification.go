package handlers

import (
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

var notificationTestClient = notify.NewHTTPClient()

func init() {
	router.NewGroupRouter("/api/v1/setting").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/notification/test", http.MethodPost).
			Use(middleware.RequireJSON()).Handle(testNotificationChannel))
}

// Tests use the supplied draft and do not save settings or consume the
// check-in cooldown. The administrator explicitly selects one destination.
func testNotificationChannel(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var request struct {
		Channel notify.Kind   `json:"channel"`
		Config  notify.Config `json:"config"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid notification test request")
		return
	}
	if err := request.Config.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	for _, target := range request.Config.Targets() {
		if target.Kind != request.Channel {
			continue
		}
		err := notify.Deliver(c.Request.Context(), notificationTestClient, target, notify.Message{
			Level: "info", Title: "Octopus 通知测试", Text: "通知渠道连接成功。签到结果将通过此渠道发送。",
			Timestamp: time.Now().UTC(),
		})
		if err != nil {
			resp.Error(c, http.StatusBadGateway, err.Error())
			return
		}
		resp.Success(c, gin.H{"channel": target.Kind, "success": true})
		return
	}
	resp.Error(c, http.StatusBadRequest, "notification channel is not configured")
}
