package prom_metrics

import (
	"github.com/QuantumNous/new-api/common"
)

// Config 由 LoadConfig 从环境变量解析得到,Init 时使用一次。
type Config struct {
	Enabled   bool
	Host      string
	Port      int
	Path      string
	UserLabel bool // false 时 user_id/username 标签固定为空,降级到聚合视角
}

const (
	envEnabled   = "PROMETHEUS_METRICS_ENABLED"
	envHost      = "PROMETHEUS_METRICS_HOST"
	envPort      = "PROMETHEUS_METRICS_PORT"
	envPath      = "PROMETHEUS_METRICS_PATH"
	envUserLabel = "PROMETHEUS_METRICS_USER_LABEL"
)

func LoadConfig() Config {
	return Config{
		Enabled:   common.GetEnvOrDefaultBool(envEnabled, true),
		Host:      common.GetEnvOrDefaultString(envHost, "127.0.0.1"),
		Port:      common.GetEnvOrDefault(envPort, 9100),
		Path:      common.GetEnvOrDefaultString(envPath, "/metrics"),
		UserLabel: common.GetEnvOrDefaultBool(envUserLabel, true),
	}
}
