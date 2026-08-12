package service

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestBillingReportAggregationAndUpsert(t *testing.T) {
	previousEnabled := billingReportFeatureEnabled
	billingReportFeatureEnabled = true
	t.Cleanup(func() {
		billingReportFeatureEnabled = previousEnabled
	})

	require.NoError(t, model.DB.AutoMigrate(
		&model.BillingReportDaily{},
		&model.BillingReportState{},
		&model.BillingReportJob{},
	))
	require.NoError(t, model.DB.Exec("DELETE FROM billing_report_daily").Error)

	baseURL := "https://upstream.example"
	tag := "primary"
	require.NoError(t, model.DB.Create(&model.User{
		Id:       701,
		Username: "billing-user",
		Password: "not-used-in-test",
		Group:    "current-group",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      702,
		Name:    "billing-channel",
		Key:     "not-used-in-test",
		BaseURL: &baseURL,
		Tag:     &tag,
	}).Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM users WHERE id = 701")
		model.DB.Exec("DELETE FROM channels WHERE id = 702")
	})

	createdAt := time.Date(2026, 7, 27, 10, 0, 0, 0, billingReportLocation).Unix()
	log := model.Log{
		Id:               9001,
		UserId:           701,
		CreatedAt:        createdAt,
		Type:             model.LogTypeConsume,
		Username:         "billing-user",
		TokenName:        "billing-token",
		ModelName:        "billing-model",
		Quota:            2645,
		PromptTokens:     1000,
		CompletionTokens: 200,
		ChannelId:        702,
		TokenId:          703,
		Group:            "using-group",
		Other: common.MapToJsonStr(map[string]interface{}{
			"model_ratio":          1,
			"group_ratio":          2,
			"completion_ratio":     2,
			"cache_ratio":          0.1,
			"cache_creation_ratio": 1.25,
			"cache_tokens":         100,
			"cache_write_tokens":   50,
			"usage_semantic":       "openai",
			"billing_report": map[string]interface{}{
				"user_group":         "snapshot-user-group",
				"third_party_group":  "snapshot-third-party-group",
				"channel_name":       "snapshot-channel",
				"channel_tag":        "snapshot-tag",
				"upstream_url":       "https://snapshot.example",
				"quota_after_group":  2645,
				"quota_before_group": 1322.5,
			},
		}),
	}

	aggregates, latestAt := buildBillingAggregates([]model.Log{log})
	require.Equal(t, createdAt, latestAt)
	require.Len(t, aggregates, 1)
	var aggregate *model.BillingReportDaily
	for _, row := range aggregates {
		aggregate = row
	}
	require.NotNil(t, aggregate)
	require.Equal(t, "snapshot-user-group", aggregate.UserGroup)
	require.Equal(t, "snapshot-third-party-group", aggregate.ThirdPartyGroup)
	require.Equal(t, "snapshot-channel", aggregate.ChannelName)
	require.Equal(t, int64(850), aggregate.InputTokens)
	require.Equal(t, int64(100), aggregate.CacheReadTokens)
	require.Equal(t, int64(50), aggregate.CacheWriteTokens)
	require.True(t, aggregate.OriginalTotal.Equal(decimal.RequireFromString("0.002645")))
	require.True(t, aggregate.AdjustedTotal.Equal(decimal.RequireFromString("0.00529")))

	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return applyBillingAggregates(tx, aggregates, false, 0)
	}))
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return applyBillingAggregates(tx, aggregates, false, 0)
	}))

	var stored model.BillingReportDaily
	require.NoError(t, model.DB.First(&stored).Error)
	require.Equal(t, int64(2), stored.CallCount)
	require.Equal(t, int64(1700), stored.InputTokens)
	require.True(t, stored.OriginalTotal.Equal(decimal.RequireFromString("0.00529")))
	require.True(t, stored.AdjustedTotal.Equal(decimal.RequireFromString("0.01058")))
}

func TestBillingReportPostgresUpsertQualifiesExistingColumns(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{Dialector: postgres.New(postgres.Config{})}}
	onConflict := billingReportUpsertClause(db)

	var adjustedTotal clause.Expr
	for _, assignment := range onConflict.DoUpdates {
		if assignment.Column.Name == "adjusted_total" {
			var ok bool
			adjustedTotal, ok = assignment.Value.(clause.Expr)
			require.True(t, ok)
			break
		}
	}
	require.Equal(t,
		`"billing_report_daily"."adjusted_total" + excluded."adjusted_total"`,
		adjustedTotal.SQL,
	)
}

func TestFetchBillingLogsUsesCreatedAtAndRequestIDCursor(t *testing.T) {
	base := int64(1_999_999_000)
	requestIDs := []string{"billing-cursor-a", "billing-cursor-b", "billing-cursor-c"}
	logs := []model.Log{
		{CreatedAt: base, Type: model.LogTypeConsume, RequestId: requestIDs[0], ModelName: "cursor-test"},
		{CreatedAt: base, Type: model.LogTypeConsume, RequestId: requestIDs[1], ModelName: "cursor-test"},
		{CreatedAt: base + 1, Type: model.LogTypeConsume, RequestId: requestIDs[2], ModelName: "cursor-test"},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)
	t.Cleanup(func() {
		model.LOG_DB.Where("request_id IN ?", requestIDs).Delete(&model.Log{})
	})

	rows, err := fetchBillingLogs(
		billingCursor{CreatedAt: base, RequestID: requestIDs[0]},
		billingCursor{CreatedAt: base + 1, RequestID: requestIDs[2]},
		base,
		base+2,
	)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, requestIDs[1], rows[0].RequestId)
	require.Equal(t, requestIDs[2], rows[1].RequestId)
}

func TestBillingReportTieredPricingBreakdown(t *testing.T) {
	expression := `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6) : tier("long", p * 6 + c * 22.5)`
	log := model.Log{
		CreatedAt:        time.Date(2026, 7, 28, 12, 0, 0, 0, billingReportLocation).Unix(),
		Type:             model.LogTypeConsume,
		Username:         "tiered-user",
		TokenName:        "tiered-token",
		ModelName:        "tiered-model",
		Quota:            6368,
		PromptTokens:     1175,
		CompletionTokens: 200,
		Group:            "tiered-group",
		Other: common.MapToJsonStr(map[string]interface{}{
			"group_ratio":              2,
			"billing_mode":             "tiered_expr",
			"matched_tier":             "standard",
			"expr_b64":                 base64.StdEncoding.EncodeToString([]byte(expression)),
			"cache_tokens":             100,
			"cache_creation_tokens_5m": 50,
			"cache_creation_tokens_1h": 25,
			"cache_write_tokens":       75,
			"usage_semantic":           "openai",
			"billing_report": map[string]interface{}{
				"quota_after_group":  6368,
				"quota_before_group": 3183.75,
			},
		}),
	}

	row := billingRowFromLog(&log, billingChannelSnapshot{}, "")
	require.True(t, row.PricingBreakdownKnown)
	require.Equal(t, "standard", row.MatchedTier)
	require.Equal(t, int64(1000), row.InputTokens)
	require.Equal(t, int64(75), row.CacheWriteTokens)
	require.True(t, row.OriginalInputUnit.Equal(decimal.RequireFromString("3")))
	require.True(t, row.OriginalOutputUnit.Equal(decimal.RequireFromString("15")))
	require.True(t, row.OriginalCacheReadUnit.Equal(decimal.RequireFromString("0.3")))
	require.True(t, row.OriginalCacheWriteUnit.Equal(decimal.RequireFromString("4.5")))
	require.True(t, row.OriginalInput.Equal(decimal.RequireFromString("0.003")))
	require.True(t, row.OriginalOutput.Equal(decimal.RequireFromString("0.003")))
	require.True(t, row.OriginalCacheRead.Equal(decimal.RequireFromString("0.00003")))
	require.True(t, row.OriginalCacheWrite.Equal(decimal.RequireFromString("0.0003375")))
	require.True(t, row.OriginalTotal.Equal(decimal.RequireFromString("0.0063675")))
	require.True(t, row.OriginalOther.IsZero())
	require.True(t, row.AdjustedInput.Add(row.AdjustedOutput).
		Add(row.AdjustedCacheRead).Add(row.AdjustedCacheWrite).
		Add(row.AdjustedOther).Equal(row.AdjustedTotal))
}

func TestBillingReportCustomTieredPricingFallsBackToDifference(t *testing.T) {
	expression := `tier("custom", p * 2 + img * 5)`
	log := model.Log{
		CreatedAt:        time.Date(2026, 7, 28, 12, 0, 0, 0, billingReportLocation).Unix(),
		Type:             model.LogTypeConsume,
		Quota:            500,
		PromptTokens:     100,
		CompletionTokens: 20,
		Other: common.MapToJsonStr(map[string]interface{}{
			"group_ratio":  1,
			"billing_mode": "tiered_expr",
			"matched_tier": "custom",
			"expr_b64":     base64.StdEncoding.EncodeToString([]byte(expression)),
			"billing_report": map[string]interface{}{
				"quota_after_group":  500,
				"quota_before_group": 500,
			},
		}),
	}

	row := billingRowFromLog(&log, billingChannelSnapshot{}, "")
	require.False(t, row.PricingBreakdownKnown)
	require.True(t, row.OriginalInput.IsZero())
	require.True(t, row.OriginalOutput.IsZero())
	require.True(t, row.OriginalOther.Equal(row.OriginalTotal))
	require.True(t, row.AdjustedOther.Equal(row.AdjustedTotal))
}

func TestBillingReportFixedPriceFallsBackToDifference(t *testing.T) {
	log := model.Log{
		CreatedAt:        time.Date(2026, 7, 28, 12, 0, 0, 0, billingReportLocation).Unix(),
		Type:             model.LogTypeConsume,
		Quota:            1000,
		PromptTokens:     100,
		CompletionTokens: 20,
		Other: common.MapToJsonStr(map[string]interface{}{
			"model_ratio":  99,
			"group_ratio":  2,
			"billing_mode": "fixed",
			"model_price":  0.005,
			"billing_report": map[string]interface{}{
				"quota_after_group":  1000,
				"quota_before_group": 500,
			},
		}),
	}

	row := billingRowFromLog(&log, billingChannelSnapshot{}, "")
	require.False(t, row.PricingBreakdownKnown)
	require.True(t, row.OriginalInputUnit.IsZero())
	require.True(t, row.OriginalOutputUnit.IsZero())
	require.True(t, row.OriginalInput.IsZero())
	require.True(t, row.OriginalOutput.IsZero())
	require.True(t, row.OriginalOther.Equal(row.OriginalTotal))
	require.True(t, row.AdjustedOther.Equal(row.AdjustedTotal))
}
