# Prometheus 用户流量监控指标 — 设计文档

- 日期：2026-05-12
- 范围：在现有 AI 网关上新增 Prometheus 指标暴露能力，第一阶段聚焦「用户流量」（请求量、token 用量、配额消耗、时延、并发）。
- 状态：已与需求方确认设计，待生成实现计划。

## 1. 背景与目标

现有项目已经维护了一套内部 `pkg/perf_metrics`，用于把模型聚合数据写入数据库，服务于管理后台的「模型广场」性能视图。但它面向业务展示，并非 Prometheus 抓取格式，无法接入 Grafana / Alertmanager 生态。

为支持运维侧基于 Grafana 的实时监控与告警，本期目标是新增一套 Prometheus 兼容的指标端点：

- 默认开启，零侵入主业务（指标记录路径任何异常都不能影响请求成功率）；
- 第一版只覆盖「用户流量」相关核心指标，便于按用户/模型查看 QPS、错误率、token 用量、时延分位；
- 部署友好：可由环境变量切换开关、监听端口与基数控制，单机默认安全。

非目标（后续可独立扩展）：

- 数据库 / Redis / Channel 内部指标（如连接池、命中率）。
- 自定义 alert rule、Grafana dashboard JSON。
- 拉取式以外的推送（Pushgateway / OTLP）。

## 2. 架构

新增独立子包 `pkg/prom_metrics`，与现有 `pkg/perf_metrics` 同层、互不依赖。包结构：

```
pkg/prom_metrics/
  metrics.go      ← 定义并注册所有 Counter/Gauge/Histogram、暴露 Record* 入口
  middleware.go   ← Gin 中间件：并发 Gauge / 耗时 Histogram / 请求计数（含失败）
  server.go       ← 独立 HTTP server(默认监听 127.0.0.1:9100)
  config.go       ← 解析 PROMETHEUS_METRICS_* 环境变量；基数控制开关
  labels.go       ← 标签清洗 / 归一化 / 错误类型派生 / username 截断
  username_cache.go ← user_id → username LRU 缓存（避免热路径阻塞）
  metrics_test.go / middleware_test.go / labels_test.go / username_cache_test.go / server_test.go
```

依赖方向（保持单向）：

- `pkg/prom_metrics` 仅依赖 `common`、`constant`、`model.GetUsernameById`、`relay/common.RelayInfo`；不反向引用 `service`、`controller`、`router`。
- `service/quota.go` 与 `service/text_quota.go` 在原有 `RecordRelaySample` 旁追加一行 `prom_metrics.RecordRelaySettled(...)`，与现有钩子并列。
- `router/relay-router.go` 在 `StatsMiddleware` 之后追加 `prom_metrics.GinMiddleware()`，只挂在 relay 路由（不污染 /api、/dashboard、/web）。
- `main.go` 在 `InitResources()` 末尾调用 `prom_metrics.Init()`，由它决定是否启动独立 HTTP server。

## 3. 指标定义

命名遵循 Prometheus 规范：`newapi_` 前缀 + 单位后缀（`_total` / `_seconds`），标签全部 lower_snake_case。

| Metric                                  | 类型       | 标签                                                                                                  | 数据来源 / 用途 |
|-----------------------------------------|----------|-----------------------------------------------------------------------------------------------------|------------|
| `newapi_relay_requests_total`           | Counter  | `user_id`, `username`, `group`, `model`, `channel_id`, `api_type`, `is_stream`, `status`, `status_code`, `error_type` | 中间件：每个 relay 请求结束计 +1（含失败请求）。QPS / 成功率 / 错误谱 |
| `newapi_relay_request_duration_seconds` | Histogram | `user_id`, `model`, `group`, `api_type`, `is_stream`, `status`                                       | 中间件：请求总耗时。延迟分位 |
| `newapi_relay_first_token_seconds`      | Histogram | `user_id`, `model`, `group`, `api_type`                                                              | 中间件：仅 stream 请求 TTFT。流式体验分析 |
| `newapi_relay_tokens_total`             | Counter  | `user_id`, `username`, `group`, `model`, `token_type`(prompt/completion/cache_read/cache_creation) | 结算钩子：token 累计。用量趋势 |
| `newapi_relay_quota_consumed_total`     | Counter  | `user_id`, `username`, `group`, `model`                                                              | 结算钩子：扣费 quota 累计。计费趋势 |
| `newapi_relay_active_requests`          | Gauge    | `api_type`, `model`                                                                                  | 中间件：进入 +1 / 退出 -1（不带 user，控制基数与原子开销） |

Histogram 桶：

- `request_duration_seconds`: `[0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300]`
- `first_token_seconds`: `[0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20]`

标签取值约定：

- `api_type` ∈ `{chat, embedding, image, audio, rerank, claude, gemini, mj, suno, realtime, other}`（由 `RelayFormat` + URL path 派生）。
- `is_stream` ∈ `{true, false}`。
- `status` ∈ `{success, error}`；`status_code` 为字符串化的 HTTP code（如 `200`、`429`、`500`），无可用 code 时填 `0`。
- `error_type` 枚举：`none`、`upstream_4xx`、`upstream_5xx`、`quota_not_enough`、`rate_limit`、`timeout`、`forbidden`、`internal`、`canceled`；越界一律归 `internal`。
- `user_id` 字符串化数字；`username` 异步从 `model.GetUsernameById` 获取，失败兜底 `unknown`。
- 空标签值统一替换为 `unknown`，便于 PromQL 操作。
- `username` 与 `model` 截断 64 字符（多字节安全）。

## 4. 数据流

### 4.1 RPC 层（中间件）

```
请求进入 relay 路由
  ↓
PrometheusRelayMiddleware:
  apiType = deriveApiType(c.Request.URL.Path, RelayFormat)
  active_requests.WithLabelValues(apiType, "").Inc()
  startTime = time.Now()
  c.Next()                                                   // ← 业务处理 + 上游调用 + 结算
  ─────────────────────────────────────────────
  从 Context 读取（结算阶段已写入）：
    userId   = ContextKeyUserId
    group    = ContextKeyUsingGroup（回退 UserGroup）
    model    = ContextKeyOriginalModel
    isStream = ContextKeyIsStream
    channelId= ContextKeyChannelId
  statusCode = c.Writer.Status()
  status, errorType = classifyOutcome(statusCode, c.Errors)
  
  active_requests.Dec(apiType, "")                           // 与 Inc 严格对称
  requests_total.Inc(全标签)
  request_duration_seconds.Observe(elapsed)
  if isStream && hasFirstResponseTime(c) {
      first_token_seconds.Observe(ttft)
  }
  username 异步通过 model.GetUsernameById 写入 metrics（见 4.3）
```

`classifyOutcome` 派生表：

| status_code | status  | error_type      |
|-------------|---------|-----------------|
| 2xx         | success | none            |
| 401/403     | error   | forbidden       |
| 429         | error   | rate_limit      |
| 504         | error   | timeout         |
| 4xx（其它）  | error   | upstream_4xx    |
| 5xx（非 504）| error   | upstream_5xx    |
| 其它 / 0    | error   | internal        |

第一版只基于 `status_code` 派生 `error_type`，不在错误文案上做字符串匹配；后续若需细化 `quota_not_enough` 等业务错误，由中间件读取专门的 Context Key（如 `ContextKeyMetricsErrorType`）来覆盖，避免脆弱的关键字匹配。

### 4.2 业务层（结算钩子）

在 `service/text_quota.go::PostTextConsumeQuota` 与 `service/quota.go::PostAudioConsumeQuota`、`PostWssConsumeQuota` 内已有 `RecordRelaySample` 的位置旁追加一行：

```go
gopool.Go(func() {
    prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
        PromptTokens:         summary.PromptTokens,
        CompletionTokens:     summary.CompletionTokens,
        CacheReadTokens:      summary.CacheTokens,
        CacheCreationTokens:  summary.CacheCreationTokens,
        Quota:                quota,
    })
})
```

- 该路径仅在结算成功时执行，token / quota 计数自然不包含失败请求。
- 字段口径与 `model.RecordConsumeLog` 对齐，与控制台日志一致。

### 4.3 Username 异步解析

中间件与结算钩子都只持有 `user_id`，username 解析流程：

- 进程内 LRU（1024 条 + 5 min TTL）`map[int]string`，命中即用。
- 未命中时调用 `model.GetUsernameById(id, false)`（内部已经 Redis → DB 兜底），结果写回 LRU。
- 解析放在 `gopool.Go` 协程，主路径不阻塞。
- 解析失败兜底 `unknown`，不报错。

## 5. 配置与暴露端点

### 5.1 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PROMETHEUS_METRICS_ENABLED` | `true` | 总开关。`false` 时跳过初始化与中间件挂载，零额外开销 |
| `PROMETHEUS_METRICS_HOST` | `127.0.0.1` | 监听地址。默认仅本机，容器场景可改 `0.0.0.0` 并配合外层 ACL |
| `PROMETHEUS_METRICS_PORT` | `9100` | 独立端口，不复用主端口 |
| `PROMETHEUS_METRICS_PATH` | `/metrics` | 暴露路径 |
| `PROMETHEUS_METRICS_USER_LABEL` | `true` | 关闭后 `user_id` / `username` 标签固定为空字符串，规避高用户量下的基数爆炸 |

不引入额外的 token 鉴权或 IP 白名单：默认监听 `127.0.0.1` 足以覆盖单机部署；多机/容器场景由用户用网关或防火墙保护，避免在网关层再发明半成品鉴权。

### 5.2 独立 HTTP server

```go
func Init() {
    cfg := loadConfig()
    if !cfg.Enabled { return }
    
    registry := prometheus.NewRegistry()         // 自定义 registry，不污染 DefaultRegisterer
    registry.MustRegister(collectors.NewProcessCollector(...))
    registry.MustRegister(collectors.NewGoCollector())
    registerRelayMetrics(registry)               // 注册第 3 节定义的所有指标
    
    mux := http.NewServeMux()
    mux.Handle(cfg.Path, promhttp.HandlerFor(registry, promhttp.HandlerOpts{
        EnableOpenMetrics: true,
    }))
    
    addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
    srv := &http.Server{
        Addr:              addr,
        Handler:           mux,
        ReadHeaderTimeout: 5 * time.Second,
    }
    
    gopool.Go(func() {
        common.SysLog("prometheus metrics listening on " + addr + cfg.Path)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            common.SysError("prometheus metrics server error: " + err.Error())
        }
    })
}
```

- 使用**自定义 registry**，避免与第三方库注入到全局 registry 的指标命名冲突，单测亦可重置。
- 顺便注册 `process_*`、`go_*` 内置 collector，Grafana 上可直接查看进程/内存/GC 指标。

### 5.3 中间件挂载点

`router/relay-router.go::SetRelayRouter` 顶部，紧跟 `StatsMiddleware`：

```go
router.Use(middleware.CORS())
router.Use(middleware.DecompressRequestMiddleware())
router.Use(middleware.BodyStorageCleanup())
router.Use(middleware.StatsMiddleware())
router.Use(prom_metrics.GinMiddleware())   // ← 新增
```

只挂在 relay-router，确保「用户流量」语义清晰。

## 6. 错误处理与基数控制

### 6.1 主路径零影响原则

- 所有 `Record*` 公开函数顶层 `defer recover`，吞掉 panic 并写 `common.SysError`。
- `nil RelayInfo` / 空 model 直接 return，不计指标也不报错。
- 中间件在 `c.Next()` 之前只做轻量计数；标签解析、username 异步全部放 `c.Next()` 之后或 `gopool.Go` 内。
- 注册阶段失败（`registry.MustRegister` panic）只可能发生在进程启动；单测覆盖以免上线翻车。

### 6.2 标签清洗与归一化

`labels.go` 统一处理：

| 处理 | 规则 |
|---|---|
| 空字符串 | 一律替换为 `"unknown"` |
| 长字符串 | username / model 截断 64 字符（多字节安全） |
| `user_id` | `strconv.Itoa`；`<= 0` 时输出 `"0"`，配合 `USER_LABEL=false` 整体降级 |
| `channel_id` | 同上，未注入时填 `"0"` |
| `error_type` | 仅允许 §4.1 表中枚举值，越界归 `internal` |
| `api_type` | 仅允许 §3 中预定义集合，越界归 `other` |

### 6.3 基数硬约束

- **用户标签开关**：`PROMETHEUS_METRICS_USER_LABEL=false` 时 `user_id`/`username` 固定空字符串，整体降级到聚合视角。
- **Username 缓存**：1024 条 LRU + 5 min TTL；超容量淘汰，避免无界增长。
- **Histogram 标签收缩**：直方图不带 `status_code` / `error_type` / `channel_id`，只保留 6 个稳定低基数标签，避免 bucket 膨胀。
- **Gauge 不带 user**：`active_requests` 只按 `api_type` × `model` 维度，避免高频 Inc/Dec 在长尾用户上的开销。

### 6.4 防护与可观测自查

- 注册阶段 panic 必须导致单测失败（CI 提前发现）。
- 启动日志打印 `prometheus metrics listening on <addr><path>`，便于运维确认。
- 进程退出时不强制 graceful shutdown 该 server（Prometheus 拉取无状态，丢失少量 scrape 无影响）。

## 7. 测试策略

### 7.1 单元测试（位于 `pkg/prom_metrics/`）

| 测试文件 | 覆盖点 |
|---|---|
| `metrics_test.go` | 注册阶段不 panic；`RecordRelaySettled` 在 token/quota=0、`nil RelayInfo` 时安全 no-op；正常输入下 Counter 数值与传入参数一致（`testutil.ToFloat64`） |
| `middleware_test.go` | 构造 `gin.TestContext`：跑通成功路径（status=200/success/none）；4xx/5xx 派生为 `upstream_4xx/5xx`；`429`→`rate_limit`；stream 请求记录 TTFT；`c.Next()` panic 时 Gauge 仍能 Dec、不外泄 panic |
| `labels_test.go` | 空字符串→`unknown`；超长 username 截断；`error_type` 越界归 `internal`；`api_type` 越界归 `other`；`USER_LABEL=false` 下 `user_id`/`username` 输出空 |
| `username_cache_test.go` | LRU 命中、过期、容量淘汰；`GetUsernameById` 返回错误时兜底 `unknown` |
| `server_test.go` | `Init()` 在 `ENABLED=false` 时确实不监听端口；`ENABLED=true` 时 `/metrics` 返回 `text/plain`（OpenMetrics）并包含已注册指标名 |

所有 Counter/Histogram 断言通过 `github.com/prometheus/client_golang/prometheus/testutil` 完成，避免依赖文本解析。

### 7.2 集成验证（手工）

- 启动服务，`curl http://127.0.0.1:9100/metrics`：
  - 包含 `newapi_relay_requests_total` 系列；
  - 包含 `process_resident_memory_bytes`、`go_goroutines`。
- 用 `wrk` / `hey` 向 `/v1/chat/completions` 发若干请求，再抓 `/metrics`，确认计数按预期增长。
- Grafana 数据源添加 Prometheus，参考查询：
  - 用户 QPS：`sum by (username) (rate(newapi_relay_requests_total[1m]))`
  - 用户 token 用量：`sum by (username, model) (rate(newapi_relay_tokens_total[5m]))`
  - 用户错误率：`sum by (username) (rate(newapi_relay_requests_total{status="error"}[5m])) / sum by (username) (rate(newapi_relay_requests_total[5m]))`
  - 全局 P95 时延：`histogram_quantile(0.95, sum by (le) (rate(newapi_relay_request_duration_seconds_bucket[5m])))`

### 7.3 性能影响评估

- 中间件每请求开销目标：≤ 2 µs（4 次原子操作 + 1 次 LRU 读 + 1 次 histogram observe）。
- 不在请求主路径分配新切片/map。
- LRU 与 username 解析在 `gopool.Go` 中完成，零阻塞。

## 8. 依赖

- `github.com/prometheus/client_golang v1.22.0`（go.mod 已作为 indirect 存在，需要在 `go.mod` 中改为 direct require）。
- `github.com/prometheus/client_golang/prometheus/collectors`（同上）。
- 标准库 `net/http`、`sync`、`container/list`（LRU 用内部小型实现,不引入新外部依赖）。

## 9. 不在本期范围

- 数据库 / Redis / Channel 池内部指标。
- Pushgateway / OTLP exporter。
- Grafana dashboard JSON、alert rule。
- 多租户隔离的 metrics namespace（如多组织部署）。
- Histogram exemplar、native histogram（v3 protocol）。
