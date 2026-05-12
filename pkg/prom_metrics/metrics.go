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
	}, []string{"user_id", "username", "group", "model", "channel_id", "api_type", "is_stream", "status", "status_code", "error_type"})

	m.requestDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "request_duration_seconds",
		Help:      "Relay request total duration in seconds.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"user_id", "model", "group", "api_type", "is_stream", "status"})

	m.firstTokenSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "first_token_seconds",
		Help:      "Time-to-first-token for streaming responses (seconds).",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20},
	}, []string{"user_id", "model", "group", "api_type"})

	m.tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "tokens_total",
		Help:      "Tokens consumed by token_type (prompt/completion/cache_read/cache_creation).",
	}, []string{"user_id", "username", "group", "model", "token_type"})

	m.quotaConsumedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "quota_consumed_total",
		Help:      "Quota consumed by relay requests (gateway internal units).",
	}, []string{"user_id", "username", "group", "model"})

	m.activeRequests = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "relay",
		Name:      "active_requests",
		Help:      "Number of in-flight relay requests.",
	}, []string{"api_type", "model"})

	for _, c := range []prometheus.Collector{
		m.requestsTotal,
		m.requestDurationSeconds,
		m.firstTokenSeconds,
		m.tokensTotal,
		m.quotaConsumedTotal,
		m.activeRequests,
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
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

	if s.PromptTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, "prompt").Add(float64(s.PromptTokens))
	}
	if s.CompletionTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, "completion").Add(float64(s.CompletionTokens))
	}
	if s.CacheReadTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, "cache_read").Add(float64(s.CacheReadTokens))
	}
	if s.CacheCreationTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, "cache_creation").Add(float64(s.CacheCreationTokens))
	}
	if s.Quota > 0 {
		m.quotaConsumedTotal.WithLabelValues(uid, uname, group, modelName).Add(float64(s.Quota))
	}

	// TTFT 仅在流式且确实有过响应时记录。RelayInfo 不存于 gin.Context,
	// 因此 TTFT 放在结算钩子而非 middleware。
	if info.IsStream && info.HasSendResponse() {
		apiType := coerceAPIType(NormalizeAPIType(info.RelayFormat, ""))
		ttft := info.FirstResponseTime.Sub(info.StartTime).Seconds()
		if ttft > 0 {
			m.firstTokenSeconds.WithLabelValues(uid, modelName, group, apiType).Observe(ttft)
		}
	}
}
