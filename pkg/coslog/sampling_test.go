package coslog

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestShouldSampleRequestIDBoundaries(t *testing.T) {
	old := common.GetCosLogSampleBasisPoints()
	t.Cleanup(func() { common.SetCosLogSampleBasisPoints(old) })

	common.SetCosLogSampleBasisPoints(0)
	if ShouldSampleRequestID("request-1") {
		t.Fatal("0 percent must not sample")
	}

	common.SetCosLogSampleBasisPoints(10000)
	if !ShouldSampleRequestID("request-1") {
		t.Fatal("100 percent must sample")
	}
}

func TestShouldSampleRequestIDIsStableAndDistributed(t *testing.T) {
	old := common.GetCosLogSampleBasisPoints()
	t.Cleanup(func() { common.SetCosLogSampleBasisPoints(old) })
	common.SetCosLogSampleBasisPoints(1000)

	const total = 100000
	selected := 0
	for i := 0; i < total; i++ {
		requestID := fmt.Sprintf("request-%d", i)
		first := ShouldSampleRequestID(requestID)
		if first != ShouldSampleRequestID(requestID) {
			t.Fatalf("sampling decision changed for %s", requestID)
		}
		if first {
			selected++
		}
	}
	if selected < 9500 || selected > 10500 {
		t.Fatalf("10 percent sample selected %d of %d requests", selected, total)
	}
}
