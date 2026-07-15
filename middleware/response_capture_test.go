package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestResponseCaptureMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedCtx *gin.Context

	router := gin.New()
	router.Use(ResponseCaptureMiddleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		capturedCtx = c
		c.Writer.Header().Set("X-Test", "value")
		c.Writer.Write([]byte(`{"ok":true}`))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/chat/completions", nil)
	router.ServeHTTP(w, req)

	if capturedCtx == nil {
		t.Fatal("handler was not called")
	}

	body, exists := capturedCtx.Get(ctxKeyResponseBody)
	if !exists {
		t.Fatal("expected response body in context")
	}
	if body != `{"ok":true}` {
		t.Fatalf("unexpected body: %v", body)
	}

	headers, exists := capturedCtx.Get(ctxKeyResponseHeaders)
	if !exists {
		t.Fatal("expected response headers in context")
	}
	if !strings.Contains(headers.(string), "X-Test") {
		t.Fatalf("expected headers to contain X-Test: %v", headers)
	}
}

func TestResponseCaptureMiddlewareFlagMatrix(t *testing.T) {
	tests := []struct {
		name       string
		coslog     bool
		requestLog bool
		captured   bool
	}{
		{name: "both enabled", coslog: true, requestLog: true, captured: true},
		{name: "request log only", coslog: false, requestLog: true, captured: true},
		{name: "coslog only", coslog: true, requestLog: false, captured: true},
		{name: "both disabled", coslog: false, requestLog: false, captured: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCoslog := common.CosLogEnabled
			oldRequestLog := common.RequestLogEnabled
			common.CosLogEnabled = tt.coslog
			common.RequestLogEnabled = tt.requestLog
			t.Cleanup(func() {
				common.CosLogEnabled = oldCoslog
				common.RequestLogEnabled = oldRequestLog
			})

			var captured bool
			router := gin.New()
			router.Use(ResponseCaptureMiddleware())
			router.GET("/", func(c *gin.Context) {
				_, captured = c.Writer.(*streamCaptureWriter)
				c.Status(http.StatusNoContent)
			})

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			if captured != tt.captured {
				t.Fatalf("captured=%t, want %t", captured, tt.captured)
			}
		})
	}
}
