package prom_metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/QuantumNous/new-api/constant"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestEngine(t *testing.T, m *metrics, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	r := gin.New()
	r.Use(m.GinMiddleware())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set(string(constant.ContextKeyUserId), 7)
		c.Set(string(constant.ContextKeyUserName), "alice")
		c.Set(string(constant.ContextKeyUsingGroup), "default")
		c.Set(string(constant.ContextKeyOriginalModel), "gpt-4o")
		c.Set(string(constant.ContextKeyChannelId), 11)
		c.Set(string(constant.ContextKeyIsStream), false)
		handler(c)
	})
	return r
}

func TestMiddleware_SuccessPath(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, _ := newMetrics(reg, Config{Enabled: true, UserLabel: true})

	r := newTestEngine(t, m, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}

	got := testutil.ToFloat64(m.requestsTotal.WithLabelValues(
		"7", "alice", "default", "gpt-4o", "11", "", "", "chat", "false",
		"success", "200", "none",
	))
	if got != 1 {
		t.Fatalf("expected requests_total=1, got %v", got)
	}
	if c := testutil.CollectAndCount(m.requestDurationSeconds); c != 1 {
		t.Fatalf("expected 1 duration sample, got %d", c)
	}
}

func TestMiddleware_RateLimit(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, _ := newMetrics(reg, Config{Enabled: true, UserLabel: true})
	r := newTestEngine(t, m, func(c *gin.Context) {
		c.JSON(http.StatusTooManyRequests, gin.H{"err": "rate"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	got := testutil.ToFloat64(m.requestsTotal.WithLabelValues(
		"7", "alice", "default", "gpt-4o", "11", "", "", "chat", "false",
		"error", "429", "rate_limit",
	))
	if got != 1 {
		t.Fatalf("expected rate_limit=1, got %v", got)
	}
}

func TestMiddleware_Upstream5xx(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, _ := newMetrics(reg, Config{Enabled: true, UserLabel: true})
	r := newTestEngine(t, m, func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"err": "boom"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	got := testutil.ToFloat64(m.requestsTotal.WithLabelValues(
		"7", "alice", "default", "gpt-4o", "11", "", "", "chat", "false",
		"error", "500", "upstream_5xx",
	))
	if got != 1 {
		t.Fatalf("expected upstream_5xx=1, got %v", got)
	}
}

func TestMiddleware_GaugeSymmetricOnPanic(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, _ := newMetrics(reg, Config{Enabled: true, UserLabel: true})

	r := gin.New()
	r.Use(gin.Recovery()) // gin 自身的 panic 恢复
	r.Use(m.GinMiddleware())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	// 即使 handler panic,Gauge 必须回到 0
	got := testutil.ToFloat64(m.activeRequests.WithLabelValues("chat", ""))
	if got != 0 {
		t.Fatalf("expected active_requests back to 0 after panic, got %v", got)
	}
}
