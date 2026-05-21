package prom_metrics

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const namespace = "newapi"

// SettledSample 是 service 层结算钩子传入 prom_metrics 的样本。
// 字段口径与 model.RecordConsumeLog 对齐。
type SettledSample struct {
	PromptTokens        int
	CompletionTokens    int
	CacheReadTokens     int
	CacheCreationTokens int
	ThinkingTokens      int // reasoning tokens (深度思考)
	Quota               int
}

type metrics struct {
	cfg       Config
	usernames *usernameCache

	requestsTotal          *prometheus.CounterVec
	requestDurationSeconds *prometheus.HistogramVec
	firstTokenSeconds      *prometheus.HistogramVec
	tokensTotal            *prometheus.CounterVec
	quotaConsumedTotal     *prometheus.CounterVec
	activeRequests         *prometheus.GaugeVec

	// 重试计数
	retryTotal *prometheus.CounterVec
	// E2E 端到端指标
	e2eRequestsTotal    *prometheus.CounterVec
	e2eRequestDuration  *prometheus.HistogramVec
	// Token 异常计数
	tokenAnomalyTotal *prometheus.CounterVec
	// Token 分类计数（input/output/cache_hit/inference/total）
	inputTokensTotal     *prometheus.CounterVec
	outputTokensTotal    *prometheus.CounterVec
	cacheHitTokensTotal  *prometheus.CounterVec
	inferenceTokensTotal *prometheus.CounterVec
	totalTokensTotal     *prometheus.CounterVec
	// 错误日志计数
	errorLogTotal *prometheus.CounterVec
	// 消费日志流量
	consumeLogTrafficTotal   *prometheus.CounterVec
	consumeLogTrafficFailed  *prometheus.CounterVec
	consumeLogTrafficSuccess *prometheus.CounterVec

	// 渠道专属指标
	channelUpstreamDuration *prometheus.HistogramVec
	channelErrorsTotal      *prometheus.CounterVec
	channelStatus           *prometheus.GaugeVec
}

func newMetrics(reg prometheus.Registerer, cfg Config) (*metrics, error) {
	m := &metrics{
		cfg:       cfg,
		usernames: newUsernameCache(1024, 5*time.Minute),
	}

	m.requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "requests_total",
		Help:      "Total number of relay requests, including failures.",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "api_type", "is_stream", "status", "status_code", "error_type"})

	m.requestDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "request_duration_seconds",
		Help:      "Relay request total duration in seconds.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"user_id", "model", "group", "channel_id", "channel_name", "channel_type", "api_type", "is_stream", "status"})

	m.firstTokenSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "first_token_seconds",
		Help:      "Time-to-first-token for streaming responses (seconds).",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20},
	}, []string{"user_id", "model", "group", "channel_id", "channel_name", "channel_type", "api_type"})

	m.tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "tokens_total",
		Help:      "Tokens consumed by token_type (prompt/completion/cache_read/cache_creation).",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "token_type"})

	m.quotaConsumedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "quota_consumed_total",
		Help:      "Quota consumed by relay requests (gateway internal units).",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type"})

	m.activeRequests = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "active_requests",
		Help:      "Number of in-flight relay requests.",
	}, []string{"api_type", "model"})

	// 重试计数
	m.retryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "retry_total",
		Help:      "Total number of relay request retries.",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "api_type"})

	// E2E 端到端指标
	m.e2eRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "e2e_requests_total",
		Help:      "Total number of E2E relay requests.",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "api_type", "status", "status_code"})

	m.e2eRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "e2e_request_duration_seconds",
		Help:      "E2E relay request duration in seconds.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"user_id", "model", "group", "channel_id", "channel_name", "channel_type", "api_type", "status"})

	// Token 异常计数（prompt/completion/thinking/total 为零或负数）
	m.tokenAnomalyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "token_anomaly_total",
		Help:      "Total number of token count anomalies (zero or negative).",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "token_type"})

	// Token 分类计数
	tokenLabels := []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "token_type"}
	m.inputTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "input_tokens_total",
		Help: "Total input tokens.",
	}, tokenLabels)
	m.outputTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "output_tokens_total",
		Help: "Total output tokens.",
	}, tokenLabels)
	m.cacheHitTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "cache_hit_tokens_total",
		Help: "Total cache hit tokens.",
	}, tokenLabels)
	m.inferenceTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "inference_tokens_total",
		Help: "Total inference tokens.",
	}, tokenLabels)
	m.totalTokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "total_tokens_total",
		Help: "Total tokens (input + output).",
	}, tokenLabels)

	// 错误日志计数
	m.errorLogTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "error_log_total",
		Help: "Total error log count.",
	}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "error_type", "status_code"})

	// 消费日志流量
	consumeLabels := []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "token_type"}
	m.consumeLogTrafficTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "consume_log_traffic_total",
		Help: "Total consume log traffic count.",
	}, consumeLabels)
	m.consumeLogTrafficFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "consume_log_traffic_failed_total",
		Help: "Total failed consume log traffic count.",
	}, append(consumeLabels, "error_code"))
	m.consumeLogTrafficSuccess = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace, Subsystem: "relay", Name: "consume_log_traffic_success_total",
		Help: "Total successful consume log traffic count.",
	}, consumeLabels)

	m.channelUpstreamDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "channel_upstream_duration_seconds",
		Help:      "Upstream provider round-trip time in seconds.",
		Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	}, []string{"channel_id", "channel_name", "channel_type", "model", "status"})

	m.channelErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "channel_errors_total",
		Help:      "Channel error count by error classification.",
	}, []string{"channel_id", "channel_name", "channel_type", "error_type", "status_code"})

	m.channelStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "channel_status",
		Help:      "Channel health status (1=enabled, 0=disabled).",
	}, []string{"channel_id", "channel_name", "channel_type"})

	collectors := []prometheus.Collector{
		m.requestsTotal,
		m.requestDurationSeconds,
		m.firstTokenSeconds,
		m.tokensTotal,
		m.quotaConsumedTotal,
		m.activeRequests,
		m.retryTotal,
		m.e2eRequestsTotal,
		m.e2eRequestDuration,
		m.tokenAnomalyTotal,
		m.inputTokensTotal,
		m.outputTokensTotal,
		m.cacheHitTokensTotal,
		m.inferenceTokensTotal,
		m.totalTokensTotal,
		m.errorLogTotal,
		m.consumeLogTrafficTotal,
		m.consumeLogTrafficFailed,
		m.consumeLogTrafficSuccess,
		m.channelUpstreamDuration,
		m.channelErrorsTotal,
		m.channelStatus,
	}
	for i, c := range collectors {
		if err := reg.Register(c); err != nil {
			common.SysError(fmt.Sprintf("[prom_metrics-debug] register[%d] failed: %v", i, err))
			return nil, err
		}
	}
	common.SysLog(fmt.Sprintf("[prom_metrics-debug] registered %d collectors", len(collectors)))
	return m, nil
}

// userLabels 根据 USER_LABEL 开关决定是否输出 user_id/username。
// 关闭时一律返回空串,聚合视图也仍能工作。
func (m *metrics) userLabels(userID int) (uid, uname string) {
	if !m.cfg.UserLabel {
		return "", ""
	}
	if userID <= 0 {
		return "0", LabelUnknown
	}
	uid = strconv.Itoa(userID)
	uname = m.usernames.ResolveWith(userID, safeUsernameFetcher)
	return uid, sanitizeLabel(uname)
}

// safeUsernameFetcher 隔离 model.GetUsernameById 的潜在 panic(例如测试环境下
// DB 句柄为 nil),将其降级为返回错误,避免污染 RecordRelaySettled 主流程。
func safeUsernameFetcher(id int) (name string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errFetcherPanic
		}
	}()
	return model.GetUsernameById(id, false)
}

var errFetcherPanic = errors.New("prom_metrics: username fetcher panic recovered")

// RecordRelaySettled 在结算钩子里被调用。负责 token/quota 计数与 TTFT 直方图。
// 任何 panic 不会向外冒泡;nil RelayInfo 视为 no-op。
func (m *metrics) RecordRelaySettled(info *relaycommon.RelayInfo, s SettledSample) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordRelaySettled panic: %v", r))
		}
	}()
	if info == nil {
		return
	}

	uid, uname := m.userLabels(info.UserId)
	group := sanitizeLabel(info.UsingGroup)
	if group == LabelUnknown {
		group = sanitizeLabel(info.UserGroup)
	}
	modelName := sanitizeLabel(info.OriginModelName)

	// 渠道标签
	channelLabel := "0"
	channelId := 0
	channelName := ""
	channelType := 0
	if info.ChannelMeta != nil {
		channelId = info.ChannelId
		channelLabel = strconv.Itoa(channelId)
		channelName = info.ChannelName
		channelType = info.ChannelType
	}
	cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)

	tokenBase := []string{uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel}
	if s.PromptTokens > 0 {
		m.tokensTotal.WithLabelValues(append(tokenBase, "prompt")...).Add(float64(s.PromptTokens))
		m.inputTokensTotal.WithLabelValues(append(tokenBase, "prompt")...).Add(float64(s.PromptTokens))
	}
	if s.CompletionTokens > 0 {
		m.tokensTotal.WithLabelValues(append(tokenBase, "completion")...).Add(float64(s.CompletionTokens))
		m.outputTokensTotal.WithLabelValues(append(tokenBase, "completion")...).Add(float64(s.CompletionTokens))
	}
	if s.CacheReadTokens > 0 {
		m.tokensTotal.WithLabelValues(append(tokenBase, "cache_read")...).Add(float64(s.CacheReadTokens))
		m.cacheHitTokensTotal.WithLabelValues(append(tokenBase, "cache_read")...).Add(float64(s.CacheReadTokens))
	}
	if s.CacheCreationTokens > 0 {
		m.tokensTotal.WithLabelValues(append(tokenBase, "cache_creation")...).Add(float64(s.CacheCreationTokens))
	}
	if s.ThinkingTokens > 0 {
		m.tokensTotal.WithLabelValues(append(tokenBase, "thinking")...).Add(float64(s.ThinkingTokens))
		m.inferenceTokensTotal.WithLabelValues(append(tokenBase, "thinking")...).Add(float64(s.ThinkingTokens))
	}
	totalTokens := s.PromptTokens + s.CompletionTokens
	if totalTokens > 0 {
		m.totalTokensTotal.WithLabelValues(append(tokenBase, "total")...).Add(float64(totalTokens))
	}
	if s.Quota > 0 {
		m.quotaConsumedTotal.WithLabelValues(tokenBase...).Add(float64(s.Quota))
	}

	// TTFT 仅在流式且确实有过响应时记录
	if info.IsStream && info.HasSendResponse() {
		apiType := coerceAPIType(NormalizeAPIType(info.RelayFormat, ""))
		ttft := info.FirstResponseTime.Sub(info.StartTime).Seconds()
		if ttft > 0 {
			m.firstTokenSeconds.WithLabelValues(uid, modelName, group, channelLabel, cNameLabel, cTypeLabel, apiType).Observe(ttft)
		}
	}

	// Token 异常计数
	if s.PromptTokens <= 0 {
		m.tokenAnomalyTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "prompt").Inc()
	}
	if s.CompletionTokens <= 0 {
		m.tokenAnomalyTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "completion").Inc()
	}
}

// RecordRetry 记录重试次数。
func (m *metrics) RecordRetry(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordRetry panic: %v", r))
		}
	}()

	uid, uname := m.userLabels(info.UserId)
	group := sanitizeLabel(info.UsingGroup)
	if group == LabelUnknown {
		group = sanitizeLabel(info.UserGroup)
	}
	modelName := sanitizeLabel(info.OriginModelName)

	channelLabel := "0"
	channelId := 0
	channelName := ""
	channelType := 0
	if info.ChannelMeta != nil {
		channelId = info.ChannelId
		channelLabel = strconv.Itoa(channelId)
		channelName = info.ChannelName
		channelType = info.ChannelType
	}
	cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)
	apiType := coerceAPIType(NormalizeAPIType(info.RelayFormat, ""))

	m.retryTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, apiType).Inc()
}

// RecordE2ERequest 记录 E2E 端到端请求。
func (m *metrics) RecordE2ERequest(info *relaycommon.RelayInfo, statusCode int, duration float64) {
	if info == nil {
		common.SysLog("[prom_metrics-debug] RecordE2ERequest: info is nil")
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordE2ERequest panic: %v", r))
		}
	}()
	common.SysLog(fmt.Sprintf("[prom_metrics-debug] RecordE2ERequest enter: model=%s, ch=%d, status=%d", info.OriginModelName, info.ChannelId, statusCode))

	uid, uname := m.userLabels(info.UserId)
	group := sanitizeLabel(info.UsingGroup)
	if group == LabelUnknown {
		group = sanitizeLabel(info.UserGroup)
	}
	modelName := sanitizeLabel(info.OriginModelName)

	channelLabel := "0"
	channelId := 0
	channelName := ""
	channelType := 0
	if info.ChannelMeta != nil {
		channelId = info.ChannelId
		channelLabel = strconv.Itoa(channelId)
		channelName = info.ChannelName
		channelType = info.ChannelType
	}
	cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)
	apiType := coerceAPIType(NormalizeAPIType(info.RelayFormat, ""))
	statusLabel, _ := ClassifyOutcome(statusCode)
	statusCodeLabel := strconv.Itoa(statusCode)

	m.e2eRequestsTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, apiType, statusLabel, statusCodeLabel).Inc()
	common.SysLog(fmt.Sprintf("[prom_metrics-debug] e2eRequestsTotal.WithLabelValues done: labels=%d", 10))
	m.e2eRequestDuration.WithLabelValues(uid, modelName, group, channelLabel, cNameLabel, cTypeLabel, apiType, statusLabel).Observe(duration)
	common.SysLog("[prom_metrics-debug] e2eRequestDuration.Observe done")
}

// RecordUpstreamDuration 记录上游提供商往返耗时。
func (m *metrics) RecordUpstreamDuration(channelId int, channelName string, channelType int, modelName string, duration float64, statusCode int) {
	if channelId == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordUpstreamDuration panic: %v", r))
		}
	}()
	statusLabel, _ := ClassifyOutcome(statusCode)
	cName, cType := m.channelLabels(channelId, channelName, channelType)
	m.channelUpstreamDuration.WithLabelValues(
		strconv.Itoa(channelId), cName, cType, sanitizeLabel(modelName), statusLabel,
	).Observe(duration)
}

// RecordChannelError 记录渠道错误。
func (m *metrics) RecordChannelError(channelId int, channelName string, channelType int, errType string, statusCode int) {
	if channelId == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordChannelError panic: %v", r))
		}
	}()
	cName, cType := m.channelLabels(channelId, channelName, channelType)
	m.channelErrorsTotal.WithLabelValues(
		strconv.Itoa(channelId), cName, cType, coerceErrorType(errType), strconv.Itoa(statusCode),
	).Inc()
}

// UpdateChannelStatus 更新渠道健康状态 gauge。
func (m *metrics) UpdateChannelStatus(channelId int, channelName string, channelType int, enabled bool) {
	if channelId == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics UpdateChannelStatus panic: %v", r))
		}
	}()
	cName, cType := m.channelLabels(channelId, channelName, channelType)
	val := float64(0)
	if enabled {
		val = 1
	}
	m.channelStatus.WithLabelValues(strconv.Itoa(channelId), cName, cType).Set(val)
}

// RecordErrorLog 记录错误日志。
func (m *metrics) RecordErrorLog(info *relaycommon.RelayInfo, errType string, statusCode int) {
	if info == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordErrorLog panic: %v", r))
		}
	}()

	uid, uname := m.userLabels(info.UserId)
	group := sanitizeLabel(info.UsingGroup)
	if group == LabelUnknown {
		group = sanitizeLabel(info.UserGroup)
	}
	modelName := sanitizeLabel(info.OriginModelName)
	channelLabel := "0"
	channelId := 0
	channelName := ""
	channelType := 0
	if info.ChannelMeta != nil {
		channelId = info.ChannelId
		channelLabel = strconv.Itoa(channelId)
		channelName = info.ChannelName
		channelType = info.ChannelType
	}
	cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)

	m.errorLogTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, coerceErrorType(errType), strconv.Itoa(statusCode)).Inc()
}

// RecordConsumeLogTraffic 记录消费日志流量。
func (m *metrics) RecordConsumeLogTraffic(info *relaycommon.RelayInfo, tokenType string, failed bool, errorCode string) {
	if info == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("prom_metrics RecordConsumeLogTraffic panic: %v", r))
		}
	}()

	uid, uname := m.userLabels(info.UserId)
	group := sanitizeLabel(info.UsingGroup)
	if group == LabelUnknown {
		group = sanitizeLabel(info.UserGroup)
	}
	modelName := sanitizeLabel(info.OriginModelName)
	channelLabel := "0"
	channelId := 0
	channelName := ""
	channelType := 0
	if info.ChannelMeta != nil {
		channelId = info.ChannelId
		channelLabel = strconv.Itoa(channelId)
		channelName = info.ChannelName
		channelType = info.ChannelType
	}
	cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)
	base := []string{uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, sanitizeLabel(tokenType)}

	m.consumeLogTrafficTotal.WithLabelValues(base...).Inc()
	if failed {
		m.consumeLogTrafficFailed.WithLabelValues(append(base, errorCode)...).Inc()
	} else {
		m.consumeLogTrafficSuccess.WithLabelValues(base...).Inc()
	}
}
