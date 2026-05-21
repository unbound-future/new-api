package prom_metrics

import (
	"context"
	"fmt"
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
	globalMu    sync.Mutex
	initialized bool // Init 被调用过即为 true,无论 enabled/disabled
	global      *metrics
	server      *http.Server
)

// Init 由 main.go 在 InitResources() 末尾调用。
// 解析配置;若未启用则零开销;若启用则注册自定义 registry + 启动独立 HTTP server。
// 重入安全:多次调用只生效一次(包括 disabled 路径)。
func Init() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if initialized {
		return
	}
	initialized = true

	cfg := LoadConfig()
	if !cfg.Enabled {
		common.SysLog("prometheus metrics disabled by config")
		return
	}

	reg := prometheus.NewRegistry()
	if err := reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		common.SysError("prometheus process collector registration failed: " + err.Error())
		return
	}
	if err := reg.Register(collectors.NewGoCollector()); err != nil {
		common.SysError("prometheus go collector registration failed: " + err.Error())
		return
	}

	m, err := newMetrics(reg, cfg)
	if err != nil {
		common.SysError("prometheus metrics registration failed: " + err.Error())
		return
	}
	global = m
	common.SysLog("prometheus metrics initialized successfully, global set")

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
	mux.HandleFunc("/debug/gather", func(w http.ResponseWriter, r *http.Request) {
		families, err := reg.Gather()
		if err != nil {
			fmt.Fprintf(w, "Gather error: %v\n\n", err)
		}
		fmt.Fprintf(w, "Total families: %d\n\n", len(families))
		for _, f := range families {
			fmt.Fprintf(w, "%s (%s) - %d metrics\n", f.GetName(), f.GetType(), len(f.GetMetric()))
		}
	})

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
		common.SysError("prom_metrics GinMiddleware: global is nil, returning no-op")
		return func(c *gin.Context) { c.Next() }
	}
	common.SysLog("prom_metrics GinMiddleware: global is set, returning real middleware")
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

// RecordRetry 由重试路径调用。Init 未运行时安全 no-op。
func RecordRetry(info *relaycommon.RelayInfo) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordRetry(info)
}

// RecordE2ERequest 由 relay 完成时调用。Init 未运行时安全 no-op。
func RecordE2ERequest(info *relaycommon.RelayInfo, statusCode int, duration float64) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordE2ERequest(info, statusCode, duration)
}

// RecordErrorLog 由错误日志记录路径调用。Init 未运行时安全 no-op。
func RecordErrorLog(info *relaycommon.RelayInfo, errType string, statusCode int) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordErrorLog(info, errType, statusCode)
}

// RecordConsumeLogTraffic 由消费日志路径调用。Init 未运行时安全 no-op。
func RecordConsumeLogTraffic(info *relaycommon.RelayInfo, tokenType string, failed bool, errorCode string) {
	globalMu.Lock()
	m := global
	globalMu.Unlock()
	if m == nil {
		return
	}
	m.RecordConsumeLogTraffic(info, tokenType, failed, errorCode)
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

// resetGlobalForTest 与 ShutdownForTest 只在单测中使用。
func resetGlobalForTest() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if server != nil {
		_ = server.Close()
	}
	initialized = false
	global = nil
	server = nil
}

// ShutdownForTest 关闭测试期间启动的 HTTP server,避免端口泄漏。
func ShutdownForTest() {
	resetGlobalForTest()
}
