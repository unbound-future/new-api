# 渠道视角 Prometheus Metrics 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Prometheus 指标体系中增加渠道维度可观测性，新增 3 个渠道专属指标，并嵌入 Grafana 仪表盘。

**Architecture:** 在 `pkg/prom_metrics/` 包中扩展指标定义和标签系统，通过 `ChannelMeta` 结构体传递渠道元数据，在 relay handler 和中间件层记录指标。Grafana 仪表盘新增一行渠道视角面板。

**Tech Stack:** Go, Prometheus client_golang, Gin, Grafana JSON

---

## Task 1: 扩展 Config 和 ChannelMeta

**Files:**
- Modify: `pkg/prom_metrics/config.go`
- Modify: `relay/common/relay_info.go:62-80` (ChannelMeta struct)
- Modify: `relay/common/relay_info.go:183-234` (InitChannelMeta)

- [ ] **Step 1: 在 Config 中新增 ChannelLabel 开关**

```go
// pkg/prom_metrics/config.go — 在 UserLabel 字段后添加
type Config struct {
	Enabled      bool
	Host         string
	Port         int
	Path         string
	UserLabel    bool
	ChannelLabel bool // false 时 channel_name/channel_type 标签固定为空
}

const (
	envEnabled      = "PROMETHEUS_METRICS_ENABLED"
	envHost         = "PROMETHEUS_METRICS_HOST"
	envPort         = "PROMETHEUS_METRICS_PORT"
	envPath         = "PROMETHEUS_METRICS_PATH"
	envUserLabel    = "PROMETHEUS_METRICS_USER_LABEL"
	envChannelLabel = "PROMETHEUS_METRICS_CHANNEL_LABEL"
)

func LoadConfig() Config {
	return Config{
		Enabled:      common.GetEnvOrDefaultBool(envEnabled, true),
		Host:         common.GetEnvOrDefaultString(envHost, "127.0.0.1"),
		Port:         common.GetEnvOrDefault(envPort, 9100),
		Path:         common.GetEnvOrDefaultString(envPath, "/metrics"),
		UserLabel:    common.GetEnvOrDefaultBool(envUserLabel, true),
		ChannelLabel: common.GetEnvOrDefaultBool(envChannelLabel, true),
	}
}
```

- [ ] **Step 2: 在 ChannelMeta 中新增 ChannelName 字段**

```go
// relay/common/relay_info.go — ChannelMeta struct 中添加
type ChannelMeta struct {
	ChannelType          int
	ChannelId            int
	ChannelName          string // 新增
	ChannelIsMultiKey    bool
	ChannelMultiKeyIndex int
	ChannelBaseUrl       string
	ApiType              int
	ApiVersion           string
	ApiKey               string
	Organization         string
	ChannelCreateTime    int64
	ParamOverride        map[string]interface{}
	HeadersOverride      map[string]interface{}
	ChannelSetting       dto.ChannelSettings
	ChannelOtherSettings dto.ChannelOtherSettings
	UpstreamModelName    string
	IsModelMapped        bool
	SupportStreamOptions bool
}
```

- [ ] **Step 3: 在 InitChannelMeta 中读取 channel_name**

```go
// relay/common/relay_info.go — InitChannelMeta 函数中，在 ChannelType 赋值后添加
channelMeta := &ChannelMeta{
	ChannelType:          channelType,
	ChannelId:            common.GetContextKeyInt(c, constant.ContextKeyChannelId),
	ChannelName:          common.GetContextKeyString(c, constant.ContextKeyChannelName), // 新增
	// ... 其余字段不变
}
```

- [ ] **Step 4: 运行测试验证**

```bash
cd /Users/admin/UnBound/code/new-api
go build ./relay/common/...
```

- [ ] **Step 5: Commit**

```bash
git add pkg/prom_metrics/config.go relay/common/relay_info.go
git commit -m "feat(prom_metrics): add ChannelLabel config and ChannelName to ChannelMeta"
```

---

## Task 2: 扩展 labels.go — 添加渠道标签辅助函数

**Files:**
- Modify: `pkg/prom_metrics/labels.go`

- [ ] **Step 1: 添加 channelLabels 方法**

```go
// pkg/prom_metrics/labels.go — 在文件末尾添加

// channelLabels 根据 CHANNEL_LABEL 开关决定是否输出 channel_name/channel_type。
// 关闭时一律返回空串。
func (m *metrics) channelLabels(channelId int, channelName string, channelType int) (cName, cType string) {
	if !m.cfg.ChannelLabel {
		return "", ""
	}
	cName = sanitizeLabel(channelName)
	if name, ok := constant.ChannelTypeNames[channelType]; ok {
		cType = strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	} else {
		cType = LabelUnknown
	}
	return cName, cType
}
```

- [ ] **Step 2: 添加 import**

在 `labels.go` 的 import 中添加:

```go
import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
)
```

- [ ] **Step 3: 运行测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -run TestClassify -v
```

- [ ] **Step 4: Commit**

```bash
git add pkg/prom_metrics/labels.go
git commit -m "feat(prom_metrics): add channelLabels helper for channel_name/channel_type"
```

---

## Task 3: 扩展 metrics.go — 新增 3 个渠道专属指标

**Files:**
- Modify: `pkg/prom_metrics/metrics.go`

- [ ] **Step 1: 在 metrics struct 中新增 3 个字段**

```go
// pkg/prom_metrics/metrics.go — metrics struct
type metrics struct {
	cfg       Config
	usernames *usernameCache

	requestsTotal          *prometheus.CounterVec
	requestDurationSeconds *prometheus.HistogramVec
	firstTokenSeconds      *prometheus.HistogramVec
	tokensTotal            *prometheus.CounterVec
	quotaConsumedTotal     *prometheus.CounterVec
	activeRequests         *prometheus.GaugeVec

	// 新增: 渠道专属指标
	channelUpstreamDuration *prometheus.HistogramVec
	channelErrorsTotal      *prometheus.CounterVec
	channelStatus           *prometheus.GaugeVec
}
```

- [ ] **Step 2: 在 newMetrics 中注册新指标**

```go
// pkg/prom_metrics/metrics.go — newMetrics 函数中，在 activeRequests 定义后添加

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
```

- [ ] **Step 3: 注册到 collector 列表**

```go
// pkg/prom_metrics/metrics.go — for _, c := range []prometheus.Collector{...}
for _, c := range []prometheus.Collector{
	m.requestsTotal,
	m.requestDurationSeconds,
	m.firstTokenSeconds,
	m.tokensTotal,
	m.quotaConsumedTotal,
	m.activeRequests,
	m.channelUpstreamDuration, // 新增
	m.channelErrorsTotal,      // 新增
	m.channelStatus,           // 新增
} {
```

- [ ] **Step 4: 在现有指标的 label 列表中添加 channel_name 和 channel_type**

```go
// requestsTotal — 修改 label 列表
m.requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	// ... 不变
}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "api_type", "is_stream", "status", "status_code", "error_type"})

// requestDurationSeconds — 修改 label 列表
m.requestDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	// ... 不变
}, []string{"user_id", "model", "group", "channel_id", "channel_name", "channel_type", "api_type", "is_stream", "status"})

// firstTokenSeconds — 修改 label 列表
m.firstTokenSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	// ... 不变
}, []string{"user_id", "model", "group", "channel_id", "channel_name", "channel_type", "api_type"})

// tokensTotal — 修改 label 列表
m.tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	// ... 不变
}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type", "token_type"})

// quotaConsumedTotal — 修改 label 列表
m.quotaConsumedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	// ... 不变
}, []string{"user_id", "username", "group", "model", "channel_id", "channel_name", "channel_type"})
```

- [ ] **Step 5: 添加 RecordUpstreamDuration 和 RecordChannelError 方法**

```go
// pkg/prom_metrics/metrics.go — 文件末尾添加

// RecordUpstreamDuration 记录上游提供商往返耗时。
func (m *metrics) RecordUpstreamDuration(channelId int, channelName string, channelType int, modelName string, duration float64, statusCode int) {
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
```

- [ ] **Step 6: 运行测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 7: Commit**

```bash
git add pkg/prom_metrics/metrics.go
git commit -m "feat(prom_metrics): add channel_upstream_duration, channel_errors, channel_status metrics"
```

---

## Task 4: 更新 middleware.go — 附加渠道标签到现有指标

**Files:**
- Modify: `pkg/prom_metrics/middleware.go`

- [ ] **Step 1: 在 GinMiddleware 中读取渠道元数据并附加到指标**

```go
// pkg/prom_metrics/middleware.go — GinMiddleware 函数中，c.Next() 之后的出口阶段

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
channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
channelLabel := strconv.Itoa(channelId)
isStreamLabel := strconv.FormatBool(common.GetContextKeyBool(c, constant.ContextKeyIsStream))

// 新增: 读取渠道名称和类型
channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
cNameLabel, cTypeLabel := m.channelLabels(channelId, channelName, channelType)

// 出口 apiType
apiTypeFinal := coerceAPIType(NormalizeAPIType(types.RelayFormat(""), path))

statusLabel, errorTypeLabel := ClassifyOutcome(statusCode)
errorTypeLabel = coerceErrorType(errorTypeLabel)

m.requestsTotal.WithLabelValues(
	uidLabel, unameLabel, group, modelName, channelLabel,
	cNameLabel, cTypeLabel, // 新增
	apiTypeFinal, isStreamLabel,
	statusLabel, strconv.Itoa(statusCode), errorTypeLabel,
).Inc()

m.requestDurationSeconds.WithLabelValues(
	uidLabel, modelName, group, channelLabel,
	cNameLabel, cTypeLabel, // 新增
	apiTypeFinal, isStreamLabel, statusLabel,
).Observe(time.Since(start).Seconds())
```

- [ ] **Step 2: 运行测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 3: Commit**

```bash
git add pkg/prom_metrics/middleware.go
git commit -m "feat(prom_metrics): attach channel_name/channel_type labels in middleware"
```

---

## Task 5: 更新 RecordRelaySettled — 传递渠道标签

**Files:**
- Modify: `pkg/prom_metrics/metrics.go` (RecordRelaySettled method)

- [ ] **Step 1: 在 RecordRelaySettled 中读取渠道元数据**

```go
// pkg/prom_metrics/metrics.go — RecordRelaySettled 方法

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

	if s.PromptTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "prompt").Add(float64(s.PromptTokens))
	}
	if s.CompletionTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "completion").Add(float64(s.CompletionTokens))
	}
	if s.CacheReadTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "cache_read").Add(float64(s.CacheReadTokens))
	}
	if s.CacheCreationTokens > 0 {
		m.tokensTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel, "cache_creation").Add(float64(s.CacheCreationTokens))
	}
	if s.Quota > 0 {
		m.quotaConsumedTotal.WithLabelValues(uid, uname, group, modelName, channelLabel, cNameLabel, cTypeLabel).Add(float64(s.Quota))
	}

	// TTFT
	if info.IsStream && info.HasSendResponse() {
		apiType := coerceAPIType(NormalizeAPIType(info.RelayFormat, ""))
		ttft := info.FirstResponseTime.Sub(info.StartTime).Seconds()
		if ttft > 0 {
			m.firstTokenSeconds.WithLabelValues(uid, modelName, group, channelLabel, cNameLabel, cTypeLabel, apiType).Observe(ttft)
		}
	}
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 3: Commit**

```bash
git add pkg/prom_metrics/metrics.go
git commit -m "feat(prom_metrics): pass channel labels in RecordRelaySettled"
```

---

## Task 6: 更新 server.go — 添加公共函数

**Files:**
- Modify: `pkg/prom_metrics/server.go`

- [ ] **Step 1: 添加公共函数**

```go
// pkg/prom_metrics/server.go — 文件末尾添加

// RecordUpstreamDuration 由上游请求完成时调用。Init 未运行时安全 no-op。
func RecordUpstreamDuration(channelId int, channelName string, channelType int, modelName string, duration float64, statusCode int) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordUpstreamDuration(channelId, channelName, channelType, modelName, duration, statusCode)
}

// RecordChannelError 由错误处理路径调用。Init 未运行时安全 no-op。
func RecordChannelError(channelId int, channelName string, channelType int, errType string, statusCode int) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordChannelError(channelId, channelName, channelType, errType, statusCode)
}

// UpdateChannelStatus 由渠道状态变更路径调用。Init 未运行时安全 no-op。
func UpdateChannelStatus(channelId int, channelName string, channelType int, enabled bool) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.UpdateChannelStatus(channelId, channelName, channelType, enabled)
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 3: Commit**

```bash
git add pkg/prom_metrics/server.go
git commit -m "feat(prom_metrics): add public RecordUpstreamDuration/RecordChannelError/UpdateChannelStatus"
```

---

## Task 7: 在 api_request.go 中添加上游耗时计时

**Files:**
- Modify: `relay/channel/api_request.go:486-534` (doRequest function)

- [ ] **Step 1: 在 client.Do 前后添加计时**

```go
// relay/channel/api_request.go — doRequest 函数中，替换 resp, err := client.Do(req) 那一行

import (
	// ... 现有 import
	"github.com/QuantumNous/new-api/pkg/prom_metrics"
)

func doRequest(c *gin.Context, req *http.Request, info *common.RelayInfo) (*http.Response, error) {
	// ... 现有代码不变，直到 client.Do 调用处

	// 计时上游请求
	upstreamStart := time.Now()
	resp, err := client.Do(req)
	upstreamDuration := time.Since(upstreamStart).Seconds()

	if err != nil {
		logger.LogError(c, "do request failed: "+err.Error())
		// 记录上游耗时（失败场景）
		if info.ChannelMeta != nil {
			prom_metrics.RecordUpstreamDuration(info.ChannelId, info.ChannelName, info.ChannelType, info.UpstreamModelName, upstreamDuration, 0)
		}
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithHideErrMsg("upstream error: do request failed"))
	}
	if resp == nil {
		return nil, errors.New("resp is nil")
	}

	// 记录上游耗时（成功场景）
	if info.ChannelMeta != nil {
		prom_metrics.RecordUpstreamDuration(info.ChannelId, info.ChannelName, info.ChannelType, info.UpstreamModelName, upstreamDuration, resp.StatusCode)
	}

	// ... 其余代码不变
}
```

- [ ] **Step 2: 确认编译通过**

```bash
cd /Users/admin/UnBound/code/new-api
go build ./relay/channel/...
```

- [ ] **Step 3: Commit**

```bash
git add relay/channel/api_request.go
git commit -m "feat(prom_metrics): add upstream duration timing in doRequest"
```

---

## Task 8: 在 controller/relay.go 中记录渠道错误

**Files:**
- Modify: `controller/relay.go:356-401` (processChannelError)

- [ ] **Step 1: 在 processChannelError 中添加 metrics 记录**

```go
// controller/relay.go — processChannelError 函数开头添加

import (
	// ... 现有 import
	"github.com/QuantumNous/new-api/pkg/prom_metrics"
)

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, err.Error()))

	// 记录渠道错误指标
	channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
	channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	prom_metrics.RecordChannelError(channelError.ChannelId, channelName, channelType, err.GetErrorType(), err.StatusCode)

	// ... 其余代码不变
}
```

- [ ] **Step 2: 确认编译通过**

```bash
cd /Users/admin/UnBound/code/new-api
go build ./controller/...
```

- [ ] **Step 3: Commit**

```bash
git add controller/relay.go
git commit -m "feat(prom_metrics): record channel errors in processChannelError"
```

---

## Task 9: 在渠道状态变更时更新健康状态 Gauge

**Files:**
- Modify: `service/channel.go:19` (DisableChannel)
- Modify: `model/channel.go:660` (UpdateChannelStatus)

- [ ] **Step 1: 在 DisableChannel 中记录指标**

```go
// service/channel.go — DisableChannel 函数

import (
	// ... 现有 import
	"github.com/QuantumNous/new-api/pkg/prom_metrics"
)

func DisableChannel(channelError types.ChannelError, reason string) {
	// ... 现有逻辑不变

	// 记录渠道状态变更
	prom_metrics.UpdateChannelStatus(channelError.ChannelId, channelError.ChannelName, channelError.ChannelType, false)

	// ... 其余代码不变
}
```

注意: 需要检查 `types.ChannelError` 是否包含 `ChannelName` 和 `ChannelType` 字段。如果不包含，需要从其他来源获取。

- [ ] **Step 2: 在 UpdateChannelStatus 中记录指标（可选）**

如果 `model.UpdateChannelStatus` 是所有状态变更的唯一入口，可以在此处也记录。但考虑到 `DisableChannel` 已经覆盖了自动禁用场景，手动启用/禁用可能需要在 controller 层处理。

- [ ] **Step 3: 确认编译通过**

```bash
cd /Users/admin/UnBound/code/new-api
go build ./service/... ./model/...
```

- [ ] **Step 4: Commit**

```bash
git add service/channel.go
git commit -m "feat(prom_metrics): update channel health status gauge on disable"
```

---

## Task 10: 更新现有测试

**Files:**
- Modify: `pkg/prom_metrics/metrics_test.go`
- Modify: `pkg/prom_metrics/middleware_test.go`

- [ ] **Step 1: 更新 metrics_test.go 中的 WithLabelValues 调用**

所有 `WithLabelValues` 调用需要添加新的 channel_name 和 channel_type 参数。例如:

```go
// TestRecordRelaySettled_TokenCounters 中
check := func(tokenType string, want float64) {
	got := testutil.ToFloat64(m.tokensTotal.WithLabelValues("7", "alice", "default", "gpt-4o", "42", "", "", tokenType))
	if got != want {
		t.Errorf("tokens[%s] = %v, want %v", tokenType, got, want)
	}
}

// quotaConsumedTotal 检查
if v := testutil.ToFloat64(m.quotaConsumedTotal.WithLabelValues("7", "alice", "default", "gpt-4o", "42", "", "")); v != 1000 {
```

- [ ] **Step 2: 更新 middleware_test.go 中的 WithLabelValues 调用**

```go
// TestMiddleware_SuccessPath 中
got := testutil.ToFloat64(m.requestsTotal.WithLabelValues(
	"7", "alice", "default", "gpt-4o", "11", "", "", "chat", "false",
	"success", "200", "none",
))
```

- [ ] **Step 3: 运行全部测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 4: Commit**

```bash
git add pkg/prom_metrics/metrics_test.go pkg/prom_metrics/middleware_test.go
git commit -m "test(prom_metrics): update tests for channel labels"
```

---

## Task 11: 新增渠道指标测试

**Files:**
- Modify: `pkg/prom_metrics/metrics_test.go`

- [ ] **Step 1: 添加上游耗时测试**

```go
func TestRecordUpstreamDuration(t *testing.T) {
	m := newTestMetrics(t)
	m.RecordUpstreamDuration(42, "test-channel", 14, "gpt-4o", 0.5, 200)

	// 验证 histogram 有样本
	if c := testutil.CollectAndCount(m.channelUpstreamDuration); c != 1 {
		t.Fatalf("expected 1 upstream duration sample, got %d", c)
	}
}
```

- [ ] **Step 2: 添加渠道错误测试**

```go
func TestRecordChannelError(t *testing.T) {
	m := newTestMetrics(t)
	m.RecordChannelError(42, "test-channel", 14, "upstream_5xx", 500)

	got := testutil.ToFloat64(m.channelErrorsTotal.WithLabelValues("42", "test-channel", "anthropic", "upstream_5xx", "500"))
	if got != 1 {
		t.Fatalf("expected channel_errors_total=1, got %v", got)
	}
}
```

- [ ] **Step 3: 添加渠道健康状态测试**

```go
func TestUpdateChannelStatus(t *testing.T) {
	m := newTestMetrics(t)

	// 启用状态
	m.UpdateChannelStatus(42, "test-channel", 14, true)
	got := testutil.ToFloat64(m.channelStatus.WithLabelValues("42", "test-channel", "anthropic"))
	if got != 1 {
		t.Fatalf("expected channel_status=1 (enabled), got %v", got)
	}

	// 禁用状态
	m.UpdateChannelStatus(42, "test-channel", 14, false)
	got = testutil.ToFloat64(m.channelStatus.WithLabelValues("42", "test-channel", "anthropic"))
	if got != 0 {
		t.Fatalf("expected channel_status=0 (disabled), got %v", got)
	}
}
```

- [ ] **Step 4: 添加 ChannelLabel 开关测试**

```go
func TestChannelLabelDisabled(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg, Config{Enabled: true, ChannelLabel: false})
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}

	m.RecordChannelError(42, "test-channel", 14, "upstream_5xx", 500)

	// ChannelLabel=false 时，channel_name/channel_type 应为空
	got := testutil.ToFloat64(m.channelErrorsTotal.WithLabelValues("42", "", "", "upstream_5xx", "500"))
	if got != 1 {
		t.Fatalf("expected 1 error with empty channel labels, got %v", got)
	}
}
```

- [ ] **Step 5: 运行全部测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
```

- [ ] **Step 6: Commit**

```bash
git add pkg/prom_metrics/metrics_test.go
git commit -m "test(prom_metrics): add tests for channel upstream duration, errors, and status"
```

---

## Task 12: 更新 Grafana 仪表盘

**Files:**
- Modify: `grafana/new-api-relay-dashboard.json`

- [ ] **Step 1: 添加 channel_name 和 channel_type 模板变量**

在 dashboard 的 `templating.list` 数组中添加两个新变量:

```json
{
  "current": {},
  "datasource": "${datasource}",
  "definition": "label_values(newapi_relay_requests_total, channel_name)",
  "hide": 0,
  "includeAll": true,
  "multi": true,
  "name": "channel_name",
  "query": {
    "query": "label_values(newapi_relay_requests_total, channel_name)",
    "refId": "StandardVariableQuery"
  },
  "refresh": 2,
  "sort": 1,
  "type": "query"
},
{
  "current": {},
  "datasource": "${datasource}",
  "definition": "label_values(newapi_relay_requests_total, channel_type)",
  "hide": 0,
  "includeAll": true,
  "multi": true,
  "name": "channel_type",
  "query": {
    "query": "label_values(newapi_relay_requests_total, channel_type)",
    "refId": "StandardVariableQuery"
  },
  "refresh": 2,
  "sort": 1,
  "type": "query"
}
```

- [ ] **Step 2: 将现有面板中的 rate 改为 RPM**

将所有 `rate(...[1m])` 改为 `increase(...[1m])`，`rate(...[5m])` 改为 `increase(...[5m])` 或 `rate(...[5m]) * 60`。

- [ ] **Step 3: 添加 Row 6 — "渠道视角"**

在现有最后一行之后添加新行，包含 5 个面板:

```json
{
  "collapsed": false,
  "gridPos": { "h": 1, "w": 24, "x": 0, "y": <next_y> },
  "id": <next_id>,
  "panels": [],
  "title": "渠道视角 (Channel Overview)",
  "type": "row"
}
```

**面板 1: RPM 按渠道 (timeseries, w=12, h=8)**
```json
{
  "title": "RPM 按渠道",
  "type": "timeseries",
  "datasource": "${datasource}",
  "targets": [{
    "expr": "sum(increase(newapi_relay_requests_total{channel_id!=\"\", channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}[1m])) by (channel_name)",
    "legendFormat": "{{channel_name}}"
  }]
}
```

**面板 2: 成功率 按渠道 (timeseries, w=12, h=8)**
```json
{
  "title": "成功率 按渠道",
  "type": "timeseries",
  "datasource": "${datasource}",
  "targets": [{
    "expr": "sum(increase(newapi_relay_requests_total{status=\"success\", channel_id!=\"\", channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}[5m])) by (channel_name) / sum(increase(newapi_relay_requests_total{channel_id!=\"\", channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}[5m])) by (channel_name)",
    "legendFormat": "{{channel_name}}"
  }]
}
```

**面板 3: P95 请求时延 按渠道 (timeseries, w=12, h=8)**
```json
{
  "title": "P95 请求时延 按渠道",
  "type": "timeseries",
  "datasource": "${datasource}",
  "targets": [{
    "expr": "histogram_quantile(0.95, sum(rate(newapi_relay_request_duration_seconds_bucket{channel_id!=\"\", channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}[5m])) by (le, channel_name))",
    "legendFormat": "{{channel_name}}"
  }]
}
```

**面板 4: 渠道错误分布 (timeseries, w=12, h=8)**
```json
{
  "title": "渠道错误分布",
  "type": "timeseries",
  "datasource": "${datasource}",
  "targets": [{
    "expr": "sum(increase(newapi_relay_channel_errors_total{channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}[5m])) by (channel_name, error_type)",
    "legendFormat": "{{channel_name}} - {{error_type}}"
  }]
}
```

**面板 5: 渠道健康状态 (stat, w=12, h=8)**
```json
{
  "title": "渠道健康状态",
  "type": "stat",
  "datasource": "${datasource}",
  "targets": [{
    "expr": "newapi_relay_channel_status{channel_name=~\"$channel_name\", channel_type=~\"$channel_type\"}",
    "legendFormat": "{{channel_name}}"
  }],
  "fieldConfig": {
    "defaults": {
      "mappings": [
        { "type": "value", "options": { "0": { "text": "Disabled", "color": "red" } } },
        { "type": "value", "options": { "1": { "text": "Healthy", "color": "green" } } }
      ]
    }
  }
}
```

- [ ] **Step 4: 验证 JSON 格式**

```bash
python3 -c "import json; json.load(open('grafana/new-api-relay-dashboard.json'))" && echo "JSON valid"
```

- [ ] **Step 5: Commit**

```bash
git add grafana/new-api-relay-dashboard.json
git commit -m "feat(grafana): add channel overview row and channel_name/channel_type variables"
```

---

## Task 13: 最终验证

- [ ] **Step 1: 运行全部测试**

```bash
cd /Users/admin/UnBound/code/new-api
go test ./pkg/prom_metrics/ -v -count=1
go test ./relay/channel/ -v -count=1
go test ./controller/ -v -count=1
```

- [ ] **Step 2: 编译整个项目**

```bash
cd /Users/admin/UnBound/code/new-api
go build ./...
```

- [ ] **Step 3: 验证 Grafana JSON**

```bash
python3 -c "import json; json.load(open('grafana/new-api-relay-dashboard.json'))" && echo "JSON valid"
```
