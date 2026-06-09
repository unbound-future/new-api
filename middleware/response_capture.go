package middleware

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"
const ctxKeyStreamChunkCount = "coslog_stream_chunk_count"
const ctxKeyStreamTotalBytes = "coslog_stream_total_bytes"
const ctxKeyStreamCompleted = "coslog_stream_completed"
const ctxKeyLastStreamChunk = "coslog_last_stream_chunk"

type captureWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	headers http.Header
	wrote   bool
}

// streamCaptureWriter 扩展 captureWriter，支持 stream 数据统计
type streamCaptureWriter struct {
	*captureWriter
	chunkCount int   // stream chunk 计数
	totalBytes int64 // stream 总字节数
	completed  bool  // 是否收到 [DONE] 标记
	lastChunk  []byte // 最后一个 chunk
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// Write 重写 Write 方法，统计 stream 数据
func (w *streamCaptureWriter) Write(b []byte) (int, error) {
	w.chunkCount++
	w.totalBytes += int64(len(b))
	w.lastChunk = make([]byte, len(b))
	copy(w.lastChunk, b)

	// 检测 stream 是否完成（[DONE] 标记）
	if strings.Contains(string(b), "[DONE]") {
		w.completed = true
	}

	return w.captureWriter.Write(b)
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

		// 使用 streamCaptureWriter 包装 captureWriter，支持 stream 统计
		scw := &streamCaptureWriter{
			captureWriter: cw,
		}
		c.Writer = scw
		c.Next()

		hBytes, _ := common.Marshal(headersToMap(cw.headers))
		bodyStr := cw.body.String()
		headersStr := string(hBytes)
		c.Set(ctxKeyResponseBody, bodyStr)
		c.Set(ctxKeyResponseHeaders, headersStr)

		// 设置 stream 元数据
		c.Set(ctxKeyStreamChunkCount, scw.chunkCount)
		c.Set(ctxKeyStreamTotalBytes, scw.totalBytes)
		c.Set(ctxKeyStreamCompleted, scw.completed)
		if len(scw.lastChunk) > 0 {
			c.Set(ctxKeyLastStreamChunk, string(scw.lastChunk))
		}

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
