# 渠道视角 Prometheus Metrics 设计文档

**日期:** 2026-05-20
**状态:** 已批准（待实现）

## 目标

在现有 Prometheus 指标体系中增加渠道维度的可观测性，使运维人员能够监控每个渠道的请求量、成功率、延迟、错误分布和健康状态。将渠道视角面板嵌入现有 Grafana Relay 仪表盘。

## 现状

`pkg/prom_metrics/` 包定义了 6 个 `newapi_relay_*` 命名空间的指标，大部分已携带 `channel_id` 标签。渠道元数据（name、type）在 `middleware/distributor.go` 的 `SetupContextForSelectedChannel` 中写入 Gin Context，relay 中间件执行时可读取。

现有 Grafana 仪表盘（`grafana/new-api-relay-dashboard.json`）有 5 行 / 19 个面板，按用户/模型视角组织。

## 设计方案

### 1. 现有指标补充标签

在已有 `channel_id` 的 5 个指标上追加 `channel_name` 和 `channel_type`：

| 指标 | 新增标签 |
|------|---------|
| `requests_total` | +channel_name, +channel_type |
| `request_duration_seconds` | +channel_name, +channel_type |
| `first_token_seconds` | +channel_name, +channel_type |
| `tokens_total` | +channel_name, +channel_type |
| `quota_consumed_total` | +channel_name, +channel_type |

**标签取值：**
- `channel_name`：从 Context 读取并 sanitize（最长 64 字符，空值 → "unknown"）
- `channel_type`：使用 `constant.ChannelTypeNames` 映射的提供商名称（如 `openai`、`anthropic`、`gemini`、`deepseek`）

**开关控制：** 通过环境变量 `PROMETHEUS_METRICS_CHANNEL_LABEL`（默认 `true`）控制，设为 `false` 可省略这些标签以降低基数。

### 2. 新增 3 个渠道专属指标

| 指标名称 | 类型 | 标签 | 用途 |
|----------|------|------|------|
| `newapi_relay_channel_upstream_duration_seconds` | Histogram | channel_id, channel_name, channel_type, model, status | 上游提供商往返耗时 |
| `newapi_relay_channel_errors_total` | Counter | channel_id, channel_name, channel_type, error_type, status_code | 按错误分类的渠道错误计数 |
| `newapi_relay_channel_status` | Gauge | channel_id, channel_name, channel_type | 渠道健康状态（1=启用, 0=禁用） |

**上游耗时 bucket：** `0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120` 秒 — 覆盖快速提供商（DeepSeek）到慢速提供商（图像生成）。

**上游耗时计时点：** `relay/channel/api_request.go` 第 518 行，`doRequest()` 函数内的 `client.Do(req)` 调用处。这是所有标准渠道适配器的唯一出站瓶颈 — 无重试循环，捕获真正的上游往返耗时。通过 `prom_metrics.RecordUpstreamDuration()` 在 `client.Do()` 返回后记录。

**错误分类：** 复用已有 `error_type` 枚举：`upstream_4xx`、`upstream_5xx`、`timeout`、`rate_limit`、`quota_not_enough`、`forbidden`、`internal`、`canceled`、`none`。

**健康状态：** 在 `processChannelError` → `DisableChannel` 路径中响应式更新，以及渠道被手动启用/禁用时更新。

### 3. 所有速率使用 RPM（非 QPS）

所有速率面板使用 RPM（每分钟请求量）而非 QPS：
- `increase(newapi_relay_requests_total{...}[1m])` 用于即时 RPM
- `sum(increase(newapi_relay_requests_total{...}[5m])) by (...) / 5` 用于 5 分钟平滑 RPM

### 4. Grafana 仪表盘变更

在现有"并发与基础设施"行之后新增 **第 6 行 — "渠道视角"**。

| 面板 | 类型 | PromQL |
|------|------|--------|
| RPM 按渠道 | timeseries | `sum(increase(newapi_relay_requests_total{channel_id!="", $channel_filter}[1m])) by (channel_name)` |
| 成功率 按渠道 | timeseries | `sum(increase(newapi_relay_requests_total{status="success", channel_id!="", $channel_filter}[5m])) by (channel_name) / sum(increase(newapi_relay_requests_total{channel_id!="", $channel_filter}[5m])) by (channel_name)` |
| P95 请求时延 按渠道 | timeseries | `histogram_quantile(0.95, sum(rate(newapi_relay_request_duration_seconds_bucket{channel_id!="", $channel_filter}[5m])) by (le, channel_name))` |
| 渠道错误分布 | timeseries | `sum(increase(newapi_relay_channel_errors_total{$channel_filter}[5m])) by (channel_name, error_type)` |
| 渠道健康状态 | stat | `newapi_relay_channel_status{$channel_filter}`（值映射：1=绿色"健康"，0=红色"已禁用"） |

**新增模板变量：**
- `channel_name`：`label_values(newapi_relay_requests_total, channel_name)`
- `channel_type`：`label_values(newapi_relay_requests_total, channel_type)`

### 5. 需修改的文件

| 文件 | 改动 |
|------|------|
| `pkg/prom_metrics/metrics.go` | 新增 3 个指标定义，添加渠道标签辅助函数 |
| `pkg/prom_metrics/labels.go` | 新增 `channelName()`、`channelType()` 标签提取器 |
| `pkg/prom_metrics/middleware.go` | 从 Context 读取 channel_name/channel_type，附加到现有 + 新增指标 |
| `service/quota.go` | 向 `RecordRelaySettled` 传递渠道元数据 |
| `service/text_quota.go` | 同上 |
| `relay/channel/api_request.go` | 在第 518 行 `client.Do(req)` 处添加上游耗时计时 |
| `controller/relay.go` | 在 `processChannelError` 中记录渠道错误 |
| `model/channel.go` / `service/channel.go` | 状态变更时更新 `ChannelStatusGauge` |
| `grafana/new-api-relay-dashboard.json` | 新增第 6 行面板、模板变量，现有面板速率改用 RPM |

### 6. 不在范围内

- 不做主动健康检查轮询（现有被动自动禁用机制已足够）
- 不做多 key 渠道的单 key 指标（超出范围）
- 不新建独立仪表盘（嵌入现有仪表盘）

### 7. 风险

- **标签基数：** 100 个渠道 × 50 个模型 × 10 种 api_type 可能导致高基数。缓解措施：环境变量开关、64 字符截断、"unknown" 兜底。
- **Scrape 体积：** 5 个指标各加 2 个标签 + 3 个新指标会增加 Prometheus 存储。预期：典型部署增加约 20-30%。
