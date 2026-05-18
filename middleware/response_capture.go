package middleware

import (
	"bytes"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"

type captureWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	headers http.Header
	wrote   bool
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	for k, v := range w.ResponseWriter.Header() {
		w.headers[k] = v
	}
	w.ResponseWriter.WriteHeader(code)
}

func ResponseCaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cw := &captureWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
			headers:        make(http.Header),
		}
		c.Writer = cw
		c.Next()

		hBytes, _ := common.Marshal(headersToMap(cw.headers))
		bodyStr := cw.body.String()
		headersStr := string(hBytes)
		c.Set(ctxKeyResponseBody, bodyStr)
		c.Set(ctxKeyResponseHeaders, headersStr)

		// 把响应内容写回到本次请求生成的 request_logs 行（response_body/headers）。
		// RecordConsumeLog 在 c.Next() 内部就已经创建了 request_logs 行，
		// 但当时响应还未生成，因此响应内容只能在这里补齐。
		model.FlushRequestLogResponses(c, headersStr, bodyStr)
	}
}

func headersToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
