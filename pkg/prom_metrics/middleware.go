package prom_metrics

import (
	"strconv"
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
		// 中间件挂在 router 顶层,RelayFormat 还未确定;此处用 path 启发式,
		// 待 c.Next() 之后若 context 提供了精确类型则覆盖。
		apiType := coerceAPIType(deriveAPITypeFromPath(path))

		// 仅 api_type × model 维度,model 在 c.Next() 之前可能为空,先用 unknown 占位。
		// 关键不变量:Gauge 入口/出口必须使用同一组标签,所以 model 在 entry/exit 都用 "" 占位,
		// model 信息只作用于 requests_total/duration 等出口标签。
		m.activeRequests.WithLabelValues(apiType, "").Inc()
		start := time.Now()
		defer func() {
			m.activeRequests.WithLabelValues(apiType, "").Dec()
		}()

		c.Next()

		// 出口阶段:从 context 读出最终标签值
		uid := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		uidLabel, unameLabel := m.userLabels(uid)
		if uname := common.GetContextKeyString(c, constant.ContextKeyUserName); uname != "" && m.cfg.UserLabel {
			// 中间件路径下 username 可直接从 context 拿,优先于 LRU
			unameLabel = sanitizeLabel(uname)
		}
		group := sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		if group == LabelUnknown {
			group = sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyUserGroup))
		}
		modelName := sanitizeLabel(common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
		channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		channelLabel := strconv.Itoa(channelID)
		if channelID <= 0 {
			channelLabel = "0"
		}
		isStream := common.GetContextKeyBool(c, constant.ContextKeyIsStream)
		isStreamLabel := "false"
		if isStream {
			isStreamLabel = "true"
		}

		// RelayFormat 在中间件层无法直接拿到;但部分 handler 会写 ContextKey,可选优化未来添加。
		// 这里继续用 path 启发式;handler 内部最终再校准。
		apiType = coerceAPIType(NormalizeAPIType(types.RelayFormat(""), path))

		statusCode := c.Writer.Status()
		statusLabel, errorTypeLabel := ClassifyOutcome(statusCode)
		errorTypeLabel = coerceErrorType(errorTypeLabel)

		m.requestsTotal.WithLabelValues(
			uidLabel, unameLabel, group, modelName, channelLabel,
			apiType, isStreamLabel,
			statusLabel, strconv.Itoa(statusCode), errorTypeLabel,
		).Inc()

		m.requestDurationSeconds.WithLabelValues(
			uidLabel, modelName, group, apiType, isStreamLabel, statusLabel,
		).Observe(time.Since(start).Seconds())
	}
}
