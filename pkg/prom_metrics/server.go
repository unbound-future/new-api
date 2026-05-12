package prom_metrics

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var (
	globalMu sync.Mutex
	global   *metrics
	server   *http.Server
)

// Init 由 main.go 在 InitResources() 末尾调用。
// 解析配置;若未启用则零开销;若启用则注册自定义 registry + 启动独立 HTTP server。
// 重入安全:多次调用只生效一次。
func Init() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if global != nil {
		return
	}

	cfg := LoadConfig()
	if !cfg.Enabled {
		common.SysLog("prometheus metrics disabled by config")
		return
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())

	m, err := newMetrics(reg, cfg)
	if err != nil {
		common.SysError("prometheus metrics registration failed: " + err.Error())
		return
	}
	global = m

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	server = srv

	// 使用 common 包的 goroutine 池启动 server
	common.RelayCtxGo(context.Background(), func() {
		common.SysLog("prometheus metrics listening on " + addr + cfg.Path)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			common.SysError("prometheus metrics server error: " + err.Error())
		}
	})
}

// GinMiddleware 是给业务调用方的便利封装。
// 当 Init 未运行或被禁用时,返回 no-op,避免空指针。
func GinMiddleware() gin.HandlerFunc {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return m.GinMiddleware()
}

// RecordRelaySettled 由结算钩子调用。Init 未运行时安全 no-op。
func RecordRelaySettled(info *relaycommon.RelayInfo, s SettledSample) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordRelaySettled(info, s)
}

// resetGlobalForTest 与 ShutdownForTest 只在单测中使用。
func resetGlobalForTest() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	global = nil
	server = nil
}

// ShutdownForTest 关闭测试期间启动的 HTTP server,避免端口泄漏。
func ShutdownForTest() {
	resetGlobalForTest()
}
