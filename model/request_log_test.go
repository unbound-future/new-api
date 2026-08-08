package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRequestLogTextUsesPortableDatabaseTypes(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
		want      string
	}{
		{name: "postgres", dialector: postgres.New(postgres.Config{}), want: "TEXT"},
		{name: "mysql", dialector: mysql.New(mysql.Config{}), want: "LONGTEXT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &gorm.DB{Config: &gorm.Config{Dialector: tt.dialector}}
			if got := (requestLogText("")).GormDBDataType(db, nil); got != tt.want {
				t.Fatalf("GormDBDataType() = %q, want %q", got, tt.want)
			}
		})
	}
}

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
