package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
