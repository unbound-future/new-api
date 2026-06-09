package coslog

import (
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type COSLOG struct {
	UserName        string `json:"user_name"`
	ModelName       string `json:"model_name"`
	Url             string `json:"url"`
	RequestID       string `json:"request_id"`
	IsStream        bool   `json:"is_stream"`
	StatusCode      int    `json:"status_code"`
	ChannelId       int    `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
	ChannelType     int    `json:"channel_type"`
	RequestBody     string `json:"request_body"`
	RequestHeaders  string `json:"request_headers"`
	ResponseHeaders string `json:"response_headers"`
	ResponseBody    string `json:"response_body"`
	// Stream 相关字段
	StreamChunkCount int    `json:"stream_chunk_count,omitempty"` // stream chunk 数量
	StreamTotalBytes int64  `json:"stream_total_bytes,omitempty"` // stream 总字节数
	StreamCompleted  bool   `json:"stream_completed,omitempty"`   // stream 是否完成（收到 [DONE]）
	LastStreamChunk  string `json:"last_stream_chunk,omitempty"` // 最后一个 chunk 的内容
}

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"

const CtxKeyRequestBody = "coslog_request_body"
const CtxKeyRequestHeaders = "coslog_request_headers"

func PrepareContext(ctx *gin.Context) {
	if bs, err := common.GetRequestBody(ctx); err == nil {
		if b, err := io.ReadAll(bs.(io.Reader)); err == nil {
			ctx.Set(CtxKeyRequestBody, string(b))
		}
	}
	if h, err := common.Marshal(headersToMap(ctx.Request.Header)); err == nil {
		ctx.Set(CtxKeyRequestHeaders, string(h))
	}
}

func Record(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if defaultWriter == nil || ctx == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError("coslog.Record panic: " + fmt.Sprint(r))
		}
	}()

	var reqBody string
	if v, exists := ctx.Get(CtxKeyRequestBody); exists {
		reqBody, _ = v.(string)
	} else {
		if bs, err := common.GetRequestBody(ctx); err == nil {
			if b, err := io.ReadAll(bs.(io.Reader)); err == nil {
				reqBody = string(b)
			}
		}
	}

	var reqHeaders string
	if v, exists := ctx.Get(CtxKeyRequestHeaders); exists {
		reqHeaders, _ = v.(string)
	} else {
		if h, err := common.Marshal(headersToMap(ctx.Request.Header)); err == nil {
			reqHeaders = string(h)
		}
	}

	var respBody, respHeaders string
	if v, exists := ctx.Get(ctxKeyResponseBody); exists {
		respBody, _ = v.(string)
	}
	if v, exists := ctx.Get(ctxKeyResponseHeaders); exists {
		respHeaders, _ = v.(string)
	}

	// 读取 stream 元数据
	var streamChunkCount int
	var streamTotalBytes int64
	var streamCompleted bool
	var lastStreamChunk string

	if v, exists := ctx.Get("coslog_stream_chunk_count"); exists {
		streamChunkCount, _ = v.(int)
	}
	if v, exists := ctx.Get("coslog_stream_total_bytes"); exists {
		streamTotalBytes, _ = v.(int64)
	}
	if v, exists := ctx.Get("coslog_stream_completed"); exists {
		streamCompleted, _ = v.(bool)
	}
	if v, exists := ctx.Get("coslog_last_stream_chunk"); exists {
		lastStreamChunk, _ = v.(string)
	}

	requestId := ctx.GetString(common.RequestIdKey)

	entry := COSLOG{
		UserName:        ctx.GetString("username"),
		ModelName:       relayInfo.OriginModelName,
		Url:             ctx.Request.URL.String(),
		RequestID:       requestId,
		IsStream:        relayInfo.IsStream,
		StatusCode:      ctx.Writer.Status(),
		ChannelId:       common.GetContextKeyInt(ctx, constant.ContextKeyChannelId),
		ChannelName:     common.GetContextKeyString(ctx, constant.ContextKeyChannelName),
		ChannelType:     common.GetContextKeyInt(ctx, constant.ContextKeyChannelType),
		RequestBody:     reqBody,
		RequestHeaders:  reqHeaders,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBody,
		// Stream 元数据
		StreamChunkCount: streamChunkCount,
		StreamTotalBytes: streamTotalBytes,
		StreamCompleted:  streamCompleted,
		LastStreamChunk:  lastStreamChunk,
	}

	defaultWriter.Write(entry)
}

func headersToMap(h map[string][]string) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
