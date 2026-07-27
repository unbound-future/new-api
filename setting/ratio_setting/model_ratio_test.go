package ratio_setting

import "testing"

var benchmarkCompletionRatio float64

func restoreCompletionRatios(t *testing.T) {
	t.Helper()
	original := CompletionRatio2JSONString()
	t.Cleanup(func() {
		if err := UpdateCompletionRatioByJSONString(original); err != nil {
			t.Fatalf("restore completion ratios: %v", err)
		}
	})
}

func TestExplicitCompletionRatioOverridesHardcodedRatio(t *testing.T) {
	restoreCompletionRatios(t)

	if err := UpdateCompletionRatioByJSONString(`{
		"gpt-5.6-sol": 6,
		"gpt-5.5": 7,
		"o3-custom": 2.5
	}`); err != nil {
		t.Fatalf("update completion ratios: %v", err)
	}

	tests := []struct {
		model string
		want  float64
	}{
		{model: "gpt-5.6-sol", want: 6},
		{model: "gpt-5.5", want: 7},
		{model: "o3-custom", want: 2.5},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			if got := GetCompletionRatio(test.model); got != test.want {
				t.Fatalf("GetCompletionRatio(%q) = %v, want %v", test.model, got, test.want)
			}
			info := GetCompletionRatioInfo(test.model)
			if info.Ratio != test.want || info.Locked {
				t.Fatalf("GetCompletionRatioInfo(%q) = %+v, want ratio %v and unlocked", test.model, info, test.want)
			}
		})
	}
}

func TestHardcodedCompletionRatioRemainsFallback(t *testing.T) {
	restoreCompletionRatios(t)

	if err := UpdateCompletionRatioByJSONString(`{}`); err != nil {
		t.Fatalf("clear completion ratios: %v", err)
	}

	tests := []struct {
		model string
		want  float64
	}{
		{model: "gpt-5.6-sol", want: 8},
		{model: "gpt-5.5", want: 6},
		{model: "o3-custom", want: 4},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			if got := GetCompletionRatio(test.model); got != test.want {
				t.Fatalf("GetCompletionRatio(%q) = %v, want %v", test.model, got, test.want)
			}
			info := GetCompletionRatioInfo(test.model)
			if info.Ratio != test.want || !info.Locked {
				t.Fatalf("GetCompletionRatioInfo(%q) = %+v, want ratio %v and locked", test.model, info, test.want)
			}
		})
	}
}

func BenchmarkGetCompletionRatioConfigured(b *testing.B) {
	original := CompletionRatio2JSONString()
	defer func() {
		if err := UpdateCompletionRatioByJSONString(original); err != nil {
			b.Fatalf("restore completion ratios: %v", err)
		}
	}()
	if err := UpdateCompletionRatioByJSONString(`{"gpt-5.6-sol":6}`); err != nil {
		b.Fatalf("update completion ratios: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkCompletionRatio = GetCompletionRatio("gpt-5.6-sol")
	}
}

func BenchmarkGetCompletionRatioFallback(b *testing.B) {
	original := CompletionRatio2JSONString()
	defer func() {
		if err := UpdateCompletionRatioByJSONString(original); err != nil {
			b.Fatalf("restore completion ratios: %v", err)
		}
	}()
	if err := UpdateCompletionRatioByJSONString(`{}`); err != nil {
		b.Fatalf("clear completion ratios: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkCompletionRatio = GetCompletionRatio("gpt-5.6-sol")
	}
}
