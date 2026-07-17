package middleware

import (
	"bytes"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/coslog"
	"github.com/gin-gonic/gin"
)

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"
const ctxKeyStreamChunkCount = "coslog_stream_chunk_count"
const ctxKeyStreamTotalBytes = "coslog_stream_total_bytes"
const ctxKeyStreamCompleted = "coslog_stream_completed"
const ctxKeyLastStreamChunk = "coslog_last_stream_chunk"

// CtxKeyBodyReader 存储一个 func() string，供 coslog/request_log 懒读 response body。
// 读取时才做一次 buffer→string 转换，避免每个 chunk 都全量复制（O(n²) → O(n)）。
const CtxKeyBodyReader = "coslog_body_reader"

type captureWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	headers http.Header
	wrote   bool
}

// streamCaptureWriter 扩展 captureWriter，支持 stream 数据统计和完整记录
type streamCaptureWriter struct {
	*captureWriter
	chunkCount int    // stream chunk 计数
	totalBytes int64  // stream 总字节数
	completed  bool   // 是否收到 [DONE] 标记
	lastChunk  []byte // 最后一个 chunk
	streamBody *bytes.Buffer
	mu         sync.Mutex
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// Write 重写 Write 方法，统计 stream 数据并完整记录。
// 不在每个 chunk 时转换 buffer→string（O(n²) 的内存复制），
// 而是通过 CtxKeyBodyReader 存储懒读函数，由调用方按需转换一次（O(n)）。
func (w *streamCaptureWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	w.chunkCount++
	w.totalBytes += int64(len(b))
	w.lastChunk = make([]byte, len(b))
	copy(w.lastChunk, b)

	if strings.Contains(string(b), "[DONE]") {
		w.completed = true
	}

	if w.streamBody != nil {
		w.streamBody.Write(b)
	}
	w.mu.Unlock()

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
		cosLogSampled := coslog.DecideForContext(c)
		if !cosLogSampled && !common.RequestLogEnabled {
			c.Next()
			return
		}

		cw := &captureWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
			headers:        make(http.Header),
		}

		scw := &streamCaptureWriter{
			captureWriter: cw,
			streamBody:    &bytes.Buffer{},
		}
		c.Writer = scw

		// 存储懒读函数：读取时才做一次 buffer→string，避免每个 chunk 全量复制。
		// bytes.Buffer.String() 是只读快照，多次调用安全，不消耗数据。
		c.Set(CtxKeyBodyReader, func() string {
			scw.mu.Lock()
			defer scw.mu.Unlock()
			return scw.streamBody.String()
		})

		c.Next()

		hBytes, _ := common.Marshal(headersToMap(cw.headers))
		headersStr := string(hBytes)

		// 获取完整的响应体（包括 c.Next() 之后继续写入的数据）
		// 优先使用 streamBody（累积了所有数据），否则使用 captureWriter.body
		scw.mu.Lock()
		var bodyStr string
		if scw.streamBody.Len() > 0 {
			bodyStr = scw.streamBody.String()
		} else {
			bodyStr = cw.body.String()
		}
		scw.mu.Unlock()

		// 最终更新 context，确保包含完整的响应体
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
