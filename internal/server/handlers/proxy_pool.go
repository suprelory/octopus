package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/rhttp"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	r := router.NewGroupRouter("/api/v1/proxy-pool").Use(middleware.Auth())
	for _, route := range []struct {
		path, method string
		handler      gin.HandlerFunc
	}{
		{"", http.MethodGet, proxyList}, {"", http.MethodPost, proxySave},
		{"/:id", http.MethodPatch, proxySave}, {"/:id", http.MethodDelete, proxyDelete},
		{"/:id/references", http.MethodGet, proxyReferences}, {"/test", http.MethodPost, proxyTest},
	} {
		r.AddRoute(router.NewRoute(route.path, route.method).Handle(route.handler))
	}
}

func proxyError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, op.ErrProxyConflict) {
		status = http.StatusConflict
	}
	resp.Error(c, status, err.Error())
}

func proxyID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.Error(c, http.StatusBadRequest, "invalid proxy ID")
		return 0, false
	}
	return id, true
}

func proxyList(c *gin.Context) {
	rows, err := op.ProxyList(c.Request.Context(), db.GetDB())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, "proxy list could not be loaded")
		return
	}
	resp.Success(c, rows)
}

func proxySave(c *gin.Context) {
	id := 0
	if c.Request.Method == http.MethodPatch {
		var ok bool
		id, ok = proxyID(c)
		if !ok {
			return
		}
	}
	var input model.ProxyWrite
	if err := checkinJSON(c, &input); err != nil {
		proxyError(c, err)
		return
	}
	row, err := op.ProxySave(c.Request.Context(), db.GetDB(), id, input)
	if err != nil {
		proxyError(c, err)
		return
	}
	resp.Success(c, row)
}

func proxyDelete(c *gin.Context) {
	id, ok := proxyID(c)
	if !ok {
		return
	}
	if err := op.ProxyDelete(c.Request.Context(), db.GetDB(), id); err != nil {
		proxyError(c, err)
		return
	}
	resp.Success(c, nil)
}

func proxyReferences(c *gin.Context) {
	id, ok := proxyID(c)
	if !ok {
		return
	}
	rows, err := op.ProxyReferences(c.Request.Context(), db.GetDB(), id)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, "proxy references could not be loaded")
		return
	}
	resp.Success(c, rows)
}

func proxyTest(c *gin.Context) {
	var input struct {
		ID        int    `json:"id"`
		URL       string `json:"url"`
		TargetURL string `json:"target_url"`
	}
	if err := checkinJSON(c, &input); err != nil {
		proxyError(c, err)
		return
	}
	if (input.ID > 0) == (input.URL != "") || input.ID < 0 {
		resp.Error(c, http.StatusBadRequest, "provide a proxy ID or URL")
		return
	}
	proxyURL := input.URL
	if input.ID > 0 {
		var err error
		proxyURL, err = op.ProxyURLForTest(c.Request.Context(), db.GetDB(), input.ID)
		if err != nil {
			proxyError(c, err)
			return
		}
	}
	result, err := rhttp.TestProxy(c.Request.Context(), proxyURL, input.TargetURL)
	if err != nil {
		proxyError(c, err)
		return
	}
	resp.Success(c, result)
}
