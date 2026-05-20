# Channel-Perspective Prometheus Metrics Design

**Date:** 2026-05-20
**Status:** Approved (pending implementation)

## Goal

Add channel-level observability to the existing Prometheus metrics system, enabling operators to monitor per-channel request volume, success rate, latency, error distribution, and health status. Embed these views into the existing Grafana Relay Dashboard.

## Current State

The `pkg/prom_metrics/` package defines 6 metrics under the `newapi_relay_*` namespace. Most already carry a `channel_id` label. Channel metadata (name, type) is available in Gin Context via `middleware/distributor.go` → `SetupContextForSelectedChannel`.

Existing Grafana dashboard (`grafana/new-api-relay-dashboard.json`) has 5 rows / 19 panels, organized by user/model perspective.

## Design

### 1. Add Labels to Existing Metrics

Add `channel_name` and `channel_type` to the 5 metrics that already have `channel_id`:

| Metric | New Labels |
|--------|-----------|
| `requests_total` | +channel_name, +channel_type |
| `request_duration_seconds` | +channel_name, +channel_type |
| `first_token_seconds` | +channel_name, +channel_type |
| `tokens_total` | +channel_name, +channel_type |
| `quota_consumed_total` | +channel_name, +channel_type |

**Label values:**
- `channel_name`: sanitized string from Context (max 64 chars, empty → "unknown")
- `channel_type`: provider name from `constant.ChannelTypeNames` (e.g., `openai`, `anthropic`, `gemini`, `deepseek`)

**Opt-out:** Controlled by env var `PROMETHEUS_METRICS_CHANNEL_LABEL` (default `true`). Set to `false` to omit these labels and reduce cardinality.

### 2. New Channel-Specific Metrics

| Metric Name | Type | Labels | Purpose |
|-------------|------|--------|---------|
| `newapi_relay_channel_upstream_duration_seconds` | Histogram | channel_id, channel_name, channel_type, model, status | Upstream provider round-trip time |
| `newapi_relay_channel_errors_total` | Counter | channel_id, channel_name, channel_type, error_type, status_code | Error count by classification |
| `newapi_relay_channel_status` | Gauge | channel_id, channel_name, channel_type | Channel health (1=enabled, 0=disabled) |

**Upstream duration buckets:** `0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120` seconds — covers fast providers (DeepSeek) to slow ones (image generation).

**Upstream duration measurement point:** `relay/channel/api_request.go` line 518, inside `doRequest()` around `client.Do(req)`. This is the single bottleneck for all standard channel adaptors — no retry loop, captures true upstream round-trip. Duration is recorded via `prom_metrics.RecordUpstreamDuration()` called after `client.Do()` returns.

**Error classification:** Reuses the existing `error_type` enum: `upstream_4xx`, `upstream_5xx`, `timeout`, `rate_limit`, `quota_not_enough`, `forbidden`, `internal`, `canceled`, `none`.

**Health status:** Updated reactively in the `processChannelError` → `DisableChannel` path, and when channels are manually enabled/disabled.

### 3. All Rates in RPM (not QPS)

All rate panels use RPM (requests per minute) instead of QPS:
- `increase(newapi_relay_requests_total{...}[1m])` for instant RPM
- `sum(increase(newapi_relay_requests_total{...}[5m])) by (...) / 5` for smoothed RPM over 5m

### 4. Grafana Dashboard Changes

Add **Row 6 — "渠道视角 (Channel Overview)"** after the existing "并发与基础设施" row.

| Panel | Type | PromQL |
|-------|------|--------|
| RPM 按渠道 | timeseries | `sum(increase(newapi_relay_requests_total{channel_id!="", $channel_filter}[1m])) by (channel_name)` |
| 成功率 按渠道 | timeseries | `sum(increase(newapi_relay_requests_total{status="success", channel_id!="", $channel_filter}[5m])) by (channel_name) / sum(increase(newapi_relay_requests_total{channel_id!="", $channel_filter}[5m])) by (channel_name)` |
| P95 请求时延 按渠道 | timeseries | `histogram_quantile(0.95, sum(rate(newapi_relay_request_duration_seconds_bucket{channel_id!="", $channel_filter}[5m])) by (le, channel_name))` |
| 渠道错误分布 | timeseries | `sum(increase(newapi_relay_channel_errors_total{$channel_filter}[5m])) by (channel_name, error_type)` |
| 渠道健康状态 | stat | `newapi_relay_channel_status{$channel_filter}` (value mappings: 1=green "Healthy", 0=red "Disabled") |

**New template variables:**
- `channel_name`: `label_values(newapi_relay_requests_total, channel_name)`
- `channel_type`: `label_values(newapi_relay_requests_total, channel_type)`

### 5. Files to Modify

| File | Change |
|------|--------|
| `pkg/prom_metrics/metrics.go` | Add 3 new metric definitions, add channel label helpers |
| `pkg/prom_metrics/labels.go` | Add `channelName()`, `channelType()` label extractors |
| `pkg/prom_metrics/middleware.go` | Read channel_name/channel_type from Context, attach to existing + new metrics |
| `service/quota.go` | Pass channel metadata to `RecordRelaySettled` |
| `service/text_quota.go` | Same as above |
| `relay/channel/api_request.go` | Add upstream duration timing around `client.Do(req)` at L518 |
| `controller/relay.go` | Record channel errors in `processChannelError` |
| `model/channel.go` / `service/channel.go` | Update `ChannelStatusGauge` on status change |
| `grafana/new-api-relay-dashboard.json` | Add Row 6 panels, new template variables, update existing panel RPM |

### 6. Non-Goals

- No proactive health check polling (existing reactive auto-disable is sufficient)
- No per-key metrics for multi-key channels (out of scope)
- No new standalone dashboard (embed into existing one)

### 7. Risks

- **Label cardinality:** 100 channels × 50 models × 10 api_types could create high cardinality. Mitigation: env var opt-out, 64-char truncation, "unknown" fallback.
- **Scrape size:** Adding 2 labels to 5 metrics + 3 new metrics increases Prometheus storage. Expected: ~20-30% increase for typical deployments.
