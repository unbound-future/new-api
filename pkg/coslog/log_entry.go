package coslog

import (
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type COSLOG struct {
	UserName        string `json:"user_name"`
	ModelName       string `json:"model_name"`
	Url             string `json:"url"`
	RequestID       string `json:"request_id"`
	RequestBody     string `json:"request_body"`
	RequestHeaders  string `json:"request_headers"`
	ResponseHeaders string `json:"response_headers"`
	ResponseBody    string `json:"response_body"`
}

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"

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
	if bs, err := common.GetRequestBody(ctx); err == nil {
		if b, err := io.ReadAll(bs.(io.Reader)); err == nil {
			reqBody = string(b)
		}
	}

	var reqHeaders string
	if h, err := common.Marshal(headersToMap(ctx.Request.Header)); err == nil {
		reqHeaders = string(h)
	}

	var respBody, respHeaders string
	if v, exists := ctx.Get(ctxKeyResponseBody); exists {
		respBody, _ = v.(string)
	}
	if v, exists := ctx.Get(ctxKeyResponseHeaders); exists {
		respHeaders, _ = v.(string)
	}

	requestId := ctx.GetString(common.RequestIdKey)

	entry := COSLOG{
		UserName:        ctx.GetString("username"),
		ModelName:       relayInfo.OriginModelName,
		Url:             ctx.Request.URL.String(),
		RequestID:       requestId,
		RequestBody:     reqBody,
		RequestHeaders:  reqHeaders,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBody,
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
