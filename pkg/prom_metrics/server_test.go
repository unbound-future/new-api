package prom_metrics

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

// pickFreePort 找一个未占用端口,以便测试启动独立 server。
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestInit_DisabledNoServer(t *testing.T) {
	port := pickFreePort(t)
	t.Setenv(envEnabled, "false")
	t.Setenv(envHost, "127.0.0.1")
	t.Setenv(envPort, strconv.Itoa(port))
	resetGlobalForTest()

	Init()
	time.Sleep(100 * time.Millisecond)

	// 端口应当空闲
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatalf("expected no server on port %d when disabled", port)
	}
}

func TestInit_EnabledServesMetrics(t *testing.T) {
	port := pickFreePort(t)
	t.Setenv(envEnabled, "true")
	t.Setenv(envHost, "127.0.0.1")
	t.Setenv(envPort, strconv.Itoa(port))
	t.Setenv(envPath, "/metrics")
	t.Setenv(envUserLabel, "false") // 禁用用户标签,避免依赖 DB
	resetGlobalForTest()

	Init()
	t.Cleanup(ShutdownForTest)

	// 使用 middleware 记录一次请求,使 requests/duration/active 指标出现
	mw := GinMiddleware()
	r := gin.New()
	r.Use(mw)
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set(string(constant.ContextKeyUserId), 1)
		c.Set(string(constant.ContextKeyUserName), "test")
		c.Set(string(constant.ContextKeyUsingGroup), "default")
		c.Set(string(constant.ContextKeyOriginalModel), "gpt-4o")
		c.Set(string(constant.ContextKeyChannelId), 1)
		c.Set(string(constant.ContextKeyIsStream), false)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	// 使用 RecordRelaySettled 记录 token/quota 数据
	RecordRelaySettled(&relaycommon.RelayInfo{
		UserId:          1,
		UsingGroup:      "default",
		OriginModelName: "gpt-4o",
		RelayFormat:     types.RelayFormatOpenAI,
		IsStream:        false,
		StartTime:       time.Now(),
	}, SettledSample{
		PromptTokens:     10,
		CompletionTokens: 20,
		Quota:            100,
	})

	// 等待 server 就绪
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("metrics endpoint unreachable: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		"newapi_relay_requests_total",
		"newapi_relay_request_duration_seconds",
		"newapi_relay_tokens_total",
		"newapi_relay_quota_consumed_total",
		"newapi_relay_active_requests",
		"go_goroutines",
		"process_resident_memory_bytes",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in /metrics output", want)
		}
	}
}
