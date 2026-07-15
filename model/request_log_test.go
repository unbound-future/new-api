package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func withRequestLogDisabled(t *testing.T) {
	oldEnabled := common.RequestLogEnabled
	oldDB := LOG_DB
	common.RequestLogEnabled = false
	LOG_DB = nil
	t.Cleanup(func() {
		common.RequestLogEnabled = oldEnabled
		LOG_DB = oldDB
	})
}

func TestRecordRequestLogDisabledDoesNotTouchDatabase(t *testing.T) {
	withRequestLogDisabled(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	recordRequestLog(c, 1, 1, "user", "model", 1, "request-id")
}

func TestFlushRequestLogResponsesDisabledDoesNotTouchDatabase(t *testing.T) {
	withRequestLogDisabled(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ctxKeyRequestLogIds, []int{1})
	FlushRequestLogResponses(c, `{"X-Test":"value"}`, `{"ok":true}`)
}
