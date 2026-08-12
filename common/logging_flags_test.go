package common

import "testing"

func TestInitLoggingFlags(t *testing.T) {
	oldCosLogEnabled := CosLogEnabled
	oldRequestLogEnabled := RequestLogEnabled
	oldSampleBasisPoints := GetCosLogSampleBasisPoints()
	t.Cleanup(func() {
		CosLogEnabled = oldCosLogEnabled
		RequestLogEnabled = oldRequestLogEnabled
		SetCosLogSampleBasisPoints(oldSampleBasisPoints)
	})

	t.Run("defaults preserve existing behavior", func(t *testing.T) {
		t.Setenv("COSLOG_ENABLED", "")
		t.Setenv("REQUEST_LOG_ENABLED", "")
		initLoggingFlags()
		if CosLogEnabled {
			t.Fatal("COSLOG must default to disabled")
		}
		if !RequestLogEnabled {
			t.Fatal("request_logs must default to enabled")
		}
	})

	t.Run("explicit values are parsed independently", func(t *testing.T) {
		t.Setenv("COSLOG_ENABLED", "true")
		t.Setenv("REQUEST_LOG_ENABLED", "false")
		t.Setenv("COSLOG_SAMPLE_PERCENT", "10")
		initLoggingFlags()
		if !CosLogEnabled || RequestLogEnabled {
			t.Fatalf("unexpected flags: coslog=%t request_log=%t", CosLogEnabled, RequestLogEnabled)
		}
		if GetCosLogSampleBasisPoints() != 1000 {
			t.Fatalf("unexpected sample basis points: %d", GetCosLogSampleBasisPoints())
		}
	})
}
