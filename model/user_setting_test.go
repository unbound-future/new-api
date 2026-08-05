package model

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

func TestRecordIpLogDefaultsToTrue(t *testing.T) {
	tests := []struct {
		name    string
		setting string
	}{
		{name: "empty setting"},
		{name: "missing field", setting: `{"notify_type":"email"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !(&User{Setting: test.setting}).GetSetting().RecordIpLog {
				t.Fatal("User.GetSetting() should enable IP logging by default")
			}
			if !(&UserBase{Setting: test.setting}).GetSetting().RecordIpLog {
				t.Fatal("UserBase.GetSetting() should enable IP logging by default")
			}
		})
	}
}

func TestRecordIpLogExplicitFalseIsPreserved(t *testing.T) {
	const setting = `{"record_ip_log":false}`
	if (&User{Setting: setting}).GetSetting().RecordIpLog {
		t.Fatal("User.GetSetting() should preserve an explicit false value")
	}
	if (&UserBase{Setting: setting}).GetSetting().RecordIpLog {
		t.Fatal("UserBase.GetSetting() should preserve an explicit false value")
	}

	user := &User{}
	user.SetSetting(dto.UserSetting{RecordIpLog: false})
	if !strings.Contains(user.Setting, `"record_ip_log":false`) {
		t.Fatalf("SetSetting() omitted explicit false value: %s", user.Setting)
	}
}

func TestRecordErrorLogHonorsDefaultAndExplicitRecordIpSetting(t *testing.T) {
	truncateTables(t)
	user := &User{
		Id:       901,
		Username: "record_ip_user",
		Password: "record_ip_password",
		Status:   common.UserStatusEnabled,
		Setting:  `{}`,
	}
	if err := DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.RemoteAddr = "203.0.113.8:1234"
	c.Set("username", user.Username)
	RecordErrorLog(c, user.Id, 0, "test-model", "test-token", "test", 0, 0, false, "default", nil)

	var log Log
	if err := LOG_DB.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to load default IP log: %v", err)
	}
	if log.Ip != "203.0.113.8" {
		t.Fatalf("default setting recorded IP %q, want %q", log.Ip, "203.0.113.8")
	}

	user.SetSetting(dto.UserSetting{RecordIpLog: false})
	if err := DB.Model(user).Update("setting", user.Setting).Error; err != nil {
		t.Fatalf("failed to disable IP logging: %v", err)
	}
	RecordErrorLog(c, user.Id, 0, "test-model", "test-token", "test", 0, 0, false, "default", nil)

	log = Log{}
	if err := LOG_DB.Order("id desc").First(&log).Error; err != nil {
		t.Fatalf("failed to load disabled IP log: %v", err)
	}
	if log.Ip != "" {
		t.Fatalf("explicit false recorded IP %q, want empty", log.Ip)
	}
}
