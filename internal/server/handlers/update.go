package handlers

import (
	"errors"
	"net/http"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/update"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/update").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("", http.MethodGet).
				Handle(latest),
		).
		AddRoute(
			router.NewRoute("/now-version", http.MethodGet).
				Handle(getNowVersion),
		).
		AddRoute(
			router.NewRoute("/metadata", http.MethodGet).
				Handle(getBuildInfo),
		).
		AddRoute(
			router.NewRoute("", http.MethodPost).
				Handle(updateFunc),
		)
}

func latest(c *gin.Context) {
	latestInfo, err := update.GetLatestInfo()
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, *latestInfo)
}

func getNowVersion(c *gin.Context) {
	resp.Success(c, conf.Version)
}

func getBuildInfo(c *gin.Context) {
	resp.Success(c, gin.H{
		"version":        conf.Version,
		"commit":         conf.Commit,
		"build_time":     conf.BuildTime,
		"author":         conf.Author,
		"repository":     conf.Repository,
		"repository_url": conf.RepositoryURL(),
	})
}

func updateFunc(c *gin.Context) {
	var request struct {
		TagName string `json:"tag_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, "select a release version before updating")
		return
	}
	err := update.UpdateCore(request.TagName)
	if err != nil {
		if errors.Is(err, update.ErrUpdateUnavailable) {
			resp.Error(c, http.StatusConflict, err.Error())
			return
		}
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, "update success")
}
