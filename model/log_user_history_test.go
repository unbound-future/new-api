package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserLogsOrdersByCreatedAtThenID(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM logs")
	})

	require.True(t, DB.Migrator().HasIndex(&Log{}, "idx_logs_user_created_at_id"))

	logs := []Log{
		{Id: 101, UserId: 7, CreatedAt: 100, Type: LogTypeConsume, Content: "newer-high-id", Other: "{}"},
		{Id: 100, UserId: 7, CreatedAt: 100, Type: LogTypeConsume, Content: "newer-low-id", Other: "{}"},
		{Id: 102, UserId: 7, CreatedAt: 90, Type: LogTypeConsume, Content: "older", Other: "{}"},
		{Id: 103, UserId: 8, CreatedAt: 110, Type: LogTypeConsume, Content: "other-user", Other: "{}"},
	}
	require.NoError(t, DB.Create(&logs).Error)

	got, total, err := GetUserLogs(7, LogTypeUnknown, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, got, 3)
	assert.Equal(t, "newer-high-id", got[0].Content)
	assert.Equal(t, "newer-low-id", got[1].Content)
	assert.Equal(t, "older", got[2].Content)
}

func TestSumUsedQuotaByUserIDIncludesRenamedUserHistory(t *testing.T) {
	require.NoError(t, DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM logs")
	})

	logs := []Log{
		{UserId: 7, Username: "old-name", CreatedAt: 100, Type: LogTypeConsume, Quota: 11},
		{UserId: 7, Username: "new-name", CreatedAt: 110, Type: LogTypeConsume, Quota: 13},
		{UserId: 8, Username: "new-name", CreatedAt: 120, Type: LogTypeConsume, Quota: 17},
		{UserId: 7, Username: "new-name", CreatedAt: 130, Type: LogTypeError, Quota: 19},
	}
	require.NoError(t, DB.Create(&logs).Error)

	stat, err := SumUsedQuotaByUserID(LogTypeUnknown, 90, 140, "", 7, "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 24, stat.Quota)

	adminStat, err := SumUsedQuota(LogTypeUnknown, 90, 140, "", "new-name", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 30, adminStat.Quota)
}

func TestLogHasSelfStatCoveringIndex(t *testing.T) {
	require.True(t, DB.Migrator().HasIndex(&Log{}, "idx_logs_user_type_created_at_quota"))
}

func TestShouldForceUserHistoryIndexOnlyForDefaultQuery(t *testing.T) {
	assert.True(t, shouldForceUserHistoryIndex(LogTypeUnknown, "", "", "", "", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeConsume, "", "", "", "", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeUnknown, "gpt-5.6-sol", "", "", "", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeUnknown, "", "token", "", "", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeUnknown, "", "", "group", "", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeUnknown, "", "", "", "request-id", ""))
	assert.False(t, shouldForceUserHistoryIndex(LogTypeUnknown, "", "", "", "", "upstream-request-id"))
}
