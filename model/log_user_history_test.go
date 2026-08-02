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
