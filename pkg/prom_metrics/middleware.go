package prom_metrics

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
)

// GinMiddleware 是挂在 relay 路由上的核心中间件:
//   - 进入时:active_requests +1(只按 api_type × model)
//   - 退出时:active_requests -1,requests_total +1,request_duration_seconds.Observe
//
// 主路径任何 panic 不会被本中间件吞掉(交给 gin.Recovery),
// 但 active_requests 的 Inc/Dec 严格对称,Dec 放在 defer 中。
func (m *metrics) GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// 入口 apiType 仅用于 Gauge Inc/Dec 对称,固化到独立变量,
		// 与出口阶段可能不同的 apiTypeFinal 互不干扰。
		entryAPIType := coerceAPIType(deriveAPITypeFromPath(path))

		// Gauge 仅按 api_type × model 维度;入口/出口都用空串占位 model。
		m.activeRequests.WithLabelValues(entryAPIType, "").Inc()
		start := time.Now()
		defer func() {
			m.activeRequests.WithLabelValues(entryAPIType, "").Dec()
		}()

		c.Next()

		statusCode := c.Writer.Status()
		// 跳过纯模型查询请求，避免产生 unknown 标签
		if c.Request.Method == "GET" && isModelMetaRequest(path) {
			return
		}

		// 跳过未到达渠道的请求（鉴权失败、模型不存在等提前终止的场景）
		channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		if channelId == 0 {
			return
		}

		// 出口阶段:从 context 读出最终标签值
		uid := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		uidLabel, unameLabel := m.userLabels(uid)
		if m.cfg.UserLabel {
			if uname := common.GetContextKeyString(c, constant.ContextKeyUserName); uname != "" {
				unameLabel = sanitizeLabel(uname)
			}
		}
		group := sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		if group == LabelUnknown {
			group = sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyUserGroup))
		}
		modelName := sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
		channelLabel := strconv.Itoa(channelId)
		channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
		channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
		cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)
		isStreamLabel := strconv.FormatBool(common.GetContextKeyBool(c, constant.ContextKeyIsStream))

		// 出口 apiType 从 path 重新派生(RelayFormat 暂未通过 context 传播到中间件层)。
		apiTypeFinal := coerceAPIType(NormalizeAPIType(types.RelayFormat(""), path))

		statusLabel, errorTypeLabel := ClassifyOutcome(statusCode)
		errorTypeLabel = coerceErrorType(errorTypeLabel)

		m.requestsTotal.WithLabelValues(
			uidLabel, unameLabel, group, modelName, channelLabel,
			cNameLabel, cTypeLabel,
			apiTypeFinal, isStreamLabel,
			statusLabel, strconv.Itoa(statusCode), errorTypeLabel,
		).Inc()

		m.requestDurationSeconds.WithLabelValues(
			uidLabel, modelName, group, channelLabel,
			cNameLabel, cTypeLabel,
			apiTypeFinal, isStreamLabel, statusLabel,
		).Observe(time.Since(start).Seconds())
	}
}

// isModelMetaRequest 判断是否为模型列表/查询请求（不产生实际调用，会导致 model 标签为 unknown）
func isModelMetaRequest(path string) bool {
	return path == "/v1/models" ||
		strings.HasPrefix(path, "/v1/models/") ||
		path == "/v1beta/models" ||
		path == "/v1beta/openai/models"
}
