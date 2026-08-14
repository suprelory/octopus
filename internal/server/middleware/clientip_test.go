package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConfigureTrustedProxiesAppliesImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.24.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if err := ConfigureTrustedProxies(""); err != nil {
		t.Fatal(err)
	}
	if got := ClientIP(c); got != "172.24.0.1" {
		t.Fatalf("untrusted client IP = %q, want TCP peer", got)
	}

	if err := ConfigureTrustedProxies("172.24.0.1"); err != nil {
		t.Fatal(err)
	}
	if got := ClientIP(c); got != "203.0.113.9" {
		t.Fatalf("trusted client IP = %q, want forwarded client", got)
	}

	t.Cleanup(func() {
		_ = ConfigureTrustedProxies("")
	})
}
