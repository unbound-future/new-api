package prom_metrics

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	clearEnv(t)
	cfg := LoadConfig()
	if !cfg.Enabled {
		t.Fatalf("expected Enabled default true, got false")
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %q", cfg.Host)
	}
	if cfg.Port != 9100 {
		t.Fatalf("expected default port 9100, got %d", cfg.Port)
	}
	if cfg.Path != "/metrics" {
		t.Fatalf("expected default path /metrics, got %q", cfg.Path)
	}
	if !cfg.UserLabel {
		t.Fatalf("expected UserLabel default true, got false")
	}
	if !cfg.ChannelLabel {
		t.Fatalf("expected ChannelLabel default true, got false")
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	clearEnv(t)
	t.Setenv(envEnabled, "false")
	t.Setenv(envHost, "0.0.0.0")
	t.Setenv(envPort, "9200")
	t.Setenv(envPath, "/m")
	t.Setenv(envUserLabel, "false")
	t.Setenv(envChannelLabel, "false")

	cfg := LoadConfig()
	if cfg.Enabled || cfg.UserLabel || cfg.ChannelLabel {
		t.Fatalf("expected Enabled/UserLabel/ChannelLabel to be false: %+v", cfg)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != 9200 || cfg.Path != "/m" {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestLoadConfig_InvalidPortFallsBack(t *testing.T) {
	clearEnv(t)
	t.Setenv(envPort, "not-a-number")
	cfg := LoadConfig()
	if cfg.Port != 9100 {
		t.Fatalf("expected fallback port 9100 on invalid input, got %d", cfg.Port)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{envEnabled, envHost, envPort, envPath, envUserLabel, envChannelLabel} {
		_ = os.Unsetenv(k)
	}
}
