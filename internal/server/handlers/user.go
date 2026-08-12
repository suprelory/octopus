package handlers

import (
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/login", http.MethodPost).
				Handle(login),
		)
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/change-password", http.MethodPost).
				Handle(changePassword),
		).
		AddRoute(
			router.NewRoute("/change-username", http.MethodPost).
				Handle(changeUsername),
		).
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(status),
		)
}

func login(c *gin.Context) {
	var user model.UserLogin
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	// expire 只接受 0（默认）、-1（记住我）和正数分钟数。历史实现把任意值直接
	// 传给 GenerateJWTToken，expire < -1 会签出没有 exp 声明的 token 并 panic。
	if !auth.JWTExpireValid(user.Expire) {
		resp.InvalidParam(c)
		return
	}

	source := c.ClientIP()
	if allowed, retryAfter := op.LoginAttemptAllow(source); !allowed {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		resp.ErrorWithAppError(c, http.StatusTooManyRequests,
			apperror.New(apperror.CodeAuthLoginRateLimited, "too many login attempts").
				WithStatus(http.StatusTooManyRequests))
		return
	}

	if err := op.UserVerify(user.Username, user.Password); err != nil {
		op.LoginAttemptFailed(source)
		resp.InvalidCredentials(c)
		return
	}
	op.LoginAttemptSucceeded(source)

	token, expire, err := auth.GenerateJWTToken(user.Expire)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, model.UserLoginResponse{Token: token, ExpireAt: expire})
}

func changePassword(c *gin.Context) {
	var user model.UserChangePassword
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangePassword(user.OldPassword, user.NewPassword); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, "password changed successfully")
}

func changeUsername(c *gin.Context) {
	var user model.UserChangeUsername
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangeUsername(user.NewUsername); err != nil {
		resp.InternalErrorWithLog(c, err)
		return
	}
	resp.Success(c, "username changed successfully")
}

func status(c *gin.Context) {
	resp.Success(c, "ok")
}
