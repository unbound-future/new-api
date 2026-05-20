package prom_metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func newTestMetrics(t *testing.T) *metrics {
	t.Helper()
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg, Config{Enabled: true, UserLabel: true})
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}
	return m
}

func TestRegister_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registration panicked: %v", r)
		}
	}()
	_ = newTestMetrics(t)
}

func TestRecordRelaySettled_NilInfo(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil info should not panic, got %v", r)
		}
	}()
	m := newTestMetrics(t)
	m.RecordRelaySettled(nil, SettledSample{PromptTokens: 1})
}

func TestRecordRelaySettled_ZeroValues(t *testing.T) {
	m := newTestMetrics(t)
	info := &relaycommon.RelayInfo{
		UserId:          1,
		UsingGroup:      "default",
		OriginModelName: "gpt-4o",
		RelayFormat:     types.RelayFormatOpenAI,
		IsStream:        false,
		StartTime:       time.Now(),
	}
	m.RecordRelaySettled(info, SettledSample{}) // 全零

	// 全零样本不应产生任何 token / quota series
	if c := testutil.CollectAndCount(m.tokensTotal); c != 0 {
		t.Fatalf("expected 0 token series for zero sample, got %d", c)
	}
	if c := testutil.CollectAndCount(m.quotaConsumedTotal); c != 0 {
		t.Fatalf("expected 0 quota series for zero sample, got %d", c)
	}
}

func TestRecordRelaySettled_TokenCounters(t *testing.T) {
	m := newTestMetrics(t)
	m.usernames.Set(7, "alice")

	info := &relaycommon.RelayInfo{
		UserId:          7,
		UsingGroup:      "default",
		OriginModelName: "gpt-4o",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 42},
		RelayFormat:     types.RelayFormatOpenAI,
		IsStream:        false,
		StartTime:       time.Now(),
	}
	m.RecordRelaySettled(info, SettledSample{
		PromptTokens:        10,
		CompletionTokens:    20,
		CacheReadTokens:     5,
		CacheCreationTokens: 3,
		Quota:               1000,
	})

	check := func(tokenType string, want float64) {
		got := testutil.ToFloat64(m.tokensTotal.WithLabelValues("7", "alice", "default", "gpt-4o", "42", "", "", tokenType))
		if got != want {
			t.Errorf("tokens[%s] = %v, want %v", tokenType, got, want)
		}
	}
	check("prompt", 10)
	check("completion", 20)
	check("cache_read", 5)
	check("cache_creation", 3)

	if v := testutil.ToFloat64(m.quotaConsumedTotal.WithLabelValues("7", "alice", "default", "gpt-4o", "42", "", "")); v != 1000 {
		t.Errorf("quota = %v, want 1000", v)
	}
}

func TestRecordRelaySettled_TTFTOnlyForStream(t *testing.T) {
	m := newTestMetrics(t)
	now := time.Now()

	// 非流式:不应写入 TTFT
	infoNonStream := &relaycommon.RelayInfo{
		UserId: 1, UsingGroup: "g", OriginModelName: "m",
		RelayFormat: types.RelayFormatOpenAI,
		IsStream:    false,
		StartTime:   now,
	}
	m.RecordRelaySettled(infoNonStream, SettledSample{})
	if got := testutil.CollectAndCount(m.firstTokenSeconds); got != 0 {
		t.Errorf("expected 0 TTFT samples for non-stream, got %d", got)
	}

	// 流式且 FirstResponseTime > StartTime(HasSendResponse=true):应写入 TTFT。
	infoStream := &relaycommon.RelayInfo{
		UserId: 1, UsingGroup: "g", OriginModelName: "m",
		RelayFormat:       types.RelayFormatOpenAI,
		IsStream:          true,
		StartTime:         now,
		FirstResponseTime: now.Add(120 * time.Millisecond),
	}
	m.RecordRelaySettled(infoStream, SettledSample{})
	if got := testutil.CollectAndCount(m.firstTokenSeconds); got != 1 {
		t.Errorf("expected 1 TTFT sample for stream with HasSendResponse, got %d", got)
	}
}

func TestRecordRelaySettled_UserLabelDisabled(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg, Config{Enabled: true, UserLabel: false})
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}
	m.usernames.Set(7, "alice")

	info := &relaycommon.RelayInfo{
		UserId:          7,
		UsingGroup:      "default",
		OriginModelName: "gpt-4o",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 42},
		IsStream:        false,
		StartTime:       time.Now(),
	}
	m.RecordRelaySettled(info, SettledSample{PromptTokens: 5})

	// USER_LABEL=false 时,user_id/username 标签应为空
	got := testutil.ToFloat64(m.tokensTotal.WithLabelValues("", "", "default", "gpt-4o", "42", "", "", "prompt"))
	if got != 5 {
		t.Errorf("expected 5 prompt tokens under empty user labels, got %v", got)
	}
}

func TestRecordUpstreamDuration(t *testing.T) {
	m := newTestMetrics(t)
	m.RecordUpstreamDuration(42, "test-channel", 14, "gpt-4o", 0.5, 200)

	if c := testutil.CollectAndCount(m.channelUpstreamDuration); c != 1 {
		t.Fatalf("expected 1 upstream duration sample, got %d", c)
	}
}

func TestRecordChannelError(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg, Config{Enabled: true, ChannelLabel: true})
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}
	m.RecordChannelError(42, "test-channel", 14, "upstream_5xx", 500)

	got := testutil.ToFloat64(m.channelErrorsTotal.WithLabelValues("42", "test-channel", "anthropic", "upstream_5xx", "500"))
	if got != 1 {
		t.Fatalf("expected channel_errors_total=1, got %v", got)
	}
}

func TestUpdateChannelStatus(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := newMetrics(reg, Config{Enabled: true, ChannelLabel: true})
	if err != nil {
		t.Fatalf("newMetrics: %v", err)
	}

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
