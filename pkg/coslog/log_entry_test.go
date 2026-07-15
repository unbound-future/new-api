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
