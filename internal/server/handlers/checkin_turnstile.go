package handlers

import (
	"github.com/bestruirui/octopus/internal/checkin"
	"github.com/gin-gonic/gin"
)

func checkinTurnstileSettings(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) { return s.Store.TurnstileSettings(c.Request.Context()) })
}

func checkinSaveTurnstile(c *gin.Context) {
	withCheckin(c, 200, func(s *checkin.Service) (any, error) {
		var input checkin.TurnstileWrite
		if err := checkinJSON(c, &input); err != nil {
			return nil, err
		}
		return s.SaveTurnstile(c.Request.Context(), input)
	})
}
