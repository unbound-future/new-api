package coslog

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func TestPrepareContextSkipsPayloadWhenDisabled(t *testing.T) {
	old := common.CosLogEnabled
	common.CosLogEnabled = false
	t.Cleanup(func() { common.CosLogEnabled = old })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"secret":"payload"}`))
	PrepareContext(c)

	if _, exists := c.Get(CtxKeyRequestBody); exists {
		t.Fatal("disabled COSLOG must not retain request body")
	}
	if _, exists := c.Get(CtxKeyRequestHeaders); exists {
		t.Fatal("disabled COSLOG must not retain request headers")
	}
}

func TestPrepareContextSkipsPayloadWhenNotSampled(t *testing.T) {
	oldEnabled := common.CosLogEnabled
	oldSampleBps := common.GetCosLogSampleBasisPoints()
	common.CosLogEnabled = true
	common.SetCosLogSampleBasisPoints(0)
	t.Cleanup(func() {
		common.CosLogEnabled = oldEnabled
		common.SetCosLogSampleBasisPoints(oldSampleBps)
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"secret":"payload"}`))
	DecideForContext(c)
	PrepareContext(c)

	if _, exists := c.Get(CtxKeyRequestBody); exists {
		t.Fatal("unsampled COSLOG must not retain request body")
	}
	if _, exists := c.Get(CtxKeyRequestHeaders); exists {
		t.Fatal("unsampled COSLOG must not retain request headers")
	}
}
