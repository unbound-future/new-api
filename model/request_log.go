package model

import (
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

// RequestLog 存储每次请求/响应的 header 与 body 原文。
// 其主键 Id 与对应消费日志（Log.Id）保持 1:1 一致，
// 因此 autoIncrement 关闭，由调用方在写入消费日志后显式赋值。
type RequestLog struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement:false"`
	UserId          int    `json:"user_id" gorm:"index"`
	Username        string `json:"username" gorm:"index;default:''"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint;index"`
	RequestId       string `json:"request_id,omitempty" gorm:"type:varchar(64);index;default:''"`
	ModelName       string `json:"model_name" gorm:"index;default:''"`
	Url             string `json:"url" gorm:"type:text"`
	RequestHeaders  string `json:"request_headers" gorm:"type:mediumtext"`
	RequestBody     string `json:"request_body" gorm:"type:longtext"`
	ResponseHeaders string `json:"response_headers" gorm:"type:mediumtext"`
	ResponseBody    string `json:"response_body" gorm:"type:longtext"`
}

// 与 middleware/response_capture.go 及 pkg/coslog/log_entry.go 中的 key 保持一致。
const (
	ctxKeyResponseBody    = "coslog_response_body"
	ctxKeyResponseHeaders = "coslog_response_headers"
)

// ctxKeyRequestLogIds 临时记录本次请求中已写入的 request_logs 行 Id。
// 由 RecordConsumeLog 在写入后追加；ResponseCaptureMiddleware 在 c.Next()
// 返回后读取并补写 response_body / response_headers，从而绕开
// "RecordConsumeLog 调用时响应尚未生成" 的时序问题。
const ctxKeyRequestLogIds = "__request_log_ids"

func extractRequestBody(c *gin.Context) string {
	bs, err := common.GetRequestBody(c)
	if err != nil || bs == nil {
		return ""
	}
	if storage, ok := bs.(common.BodyStorage); ok {
		if b, berr := storage.Bytes(); berr == nil {
			return string(b)
		}
		return ""
	}
	if reader, ok := bs.(io.Reader); ok {
		if b, rerr := io.ReadAll(reader); rerr == nil {
			return string(b)
		}
	}
	return ""
}

func extractRequestHeaders(c *gin.Context) string {
	if c == nil || c.Request == nil || len(c.Request.Header) == 0 {
		return ""
	}
	m := make(map[string]string, len(c.Request.Header))
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

func recordRequestLog(c *gin.Context, logId int, userId int, username string, modelName string, createdAt int64, requestId string) {
	if c == nil || logId <= 0 {
		return
	}

	url := ""
	if c.Request != nil && c.Request.URL != nil {
		url = c.Request.URL.String()
	}

	respBody, _ := c.Get(ctxKeyResponseBody)
	respHeaders, _ := c.Get(ctxKeyResponseHeaders)
	respBodyStr, _ := respBody.(string)
	respHeadersStr, _ := respHeaders.(string)

	rl := &RequestLog{
		Id:              logId,
		UserId:          userId,
		Username:        username,
		CreatedAt:       createdAt,
		RequestId:       requestId,
		ModelName:       modelName,
		Url:             url,
		RequestHeaders:  extractRequestHeaders(c),
		RequestBody:     extractRequestBody(c),
		ResponseHeaders: respHeadersStr,
		ResponseBody:    respBodyStr,
	}
	if err := LOG_DB.Create(rl).Error; err != nil {
		logger.LogError(c, "failed to record request log: "+err.Error())
		return
	}
	// 记录本次请求生成的 request_log id，供后续中间件写回响应内容。
	if existing, ok := c.Get(ctxKeyRequestLogIds); ok {
		if ids, ok := existing.([]int); ok {
			c.Set(ctxKeyRequestLogIds, append(ids, logId))
			return
		}
	}
	c.Set(ctxKeyRequestLogIds, []int{logId})
}

// FlushRequestLogResponses 在请求完成后被调用（来自 ResponseCaptureMiddleware），
// 把本次请求采集到的响应 body / headers 写回到对应的 request_logs 行。
func FlushRequestLogResponses(c *gin.Context, responseHeaders string, responseBody string) {
	if c == nil {
		return
	}
	v, exists := c.Get(ctxKeyRequestLogIds)
	if !exists {
		return
	}
	ids, ok := v.([]int)
	if !ok || len(ids) == 0 {
		return
	}
	if responseHeaders == "" && responseBody == "" {
		return
	}
	if err := LOG_DB.Model(&RequestLog{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"response_headers": responseHeaders,
			"response_body":    responseBody,
		}).Error; err != nil {
		logger.LogError(c, "failed to flush request log responses: "+err.Error())
	}
}

// GetRequestLogById 通过消费日志 Id 获取对应的请求日志。
func GetRequestLogById(id int) (*RequestLog, error) {
	var rl RequestLog
	if err := LOG_DB.Where("id = ?", id).First(&rl).Error; err != nil {
		return nil, err
	}
	return &rl, nil
}
