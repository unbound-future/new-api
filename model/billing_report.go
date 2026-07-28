package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	BillingReportStateID = 1

	BillingReportJobPending   = "pending"
	BillingReportJobRunning   = "running"
	BillingReportJobCompleted = "completed"
	BillingReportJobFailed    = "failed"
)

// BillingReportDaily stores materialized daily billing buckets. BucketKey is a
// stable hash of dimensions and pricing snapshots, so a price change within a
// day creates a separate row instead of averaging incompatible prices.
type BillingReportDaily struct {
	Id                     uint64          `json:"id" gorm:"primaryKey;index:idx_billing_date_id,priority:3"`
	JobId                  uint64          `json:"-" gorm:"uniqueIndex:idx_billing_daily_bucket,priority:1;index:idx_billing_date_username,priority:1;index:idx_billing_date_model,priority:1;index:idx_billing_date_id,priority:1"`
	BillDate               string          `json:"bill_date" gorm:"size:10;uniqueIndex:idx_billing_daily_bucket,priority:2;index:idx_billing_date_username,priority:2;index:idx_billing_date_model,priority:2;index:idx_billing_date_id,priority:2"`
	BucketKey              string          `json:"-" gorm:"size:64;uniqueIndex:idx_billing_daily_bucket,priority:3"`
	UserId                 int             `json:"user_id" gorm:"index"`
	Username               string          `json:"username" gorm:"size:128;index:idx_billing_date_username,priority:3"`
	UserGroup              string          `json:"user_group" gorm:"size:128"`
	ThirdPartyGroup        string          `json:"third_party_group" gorm:"size:128"`
	ChannelId              int             `json:"channel_id" gorm:"index"`
	ChannelName            string          `json:"channel_name" gorm:"size:191"`
	ChannelTag             string          `json:"channel_tag" gorm:"size:191;index"`
	UpstreamUrl            string          `json:"upstream_url" gorm:"type:text"`
	ModelName              string          `json:"model_name" gorm:"size:191;index:idx_billing_date_model,priority:3"`
	TokenId                int             `json:"token_id" gorm:"index"`
	TokenName              string          `json:"token_name" gorm:"size:191"`
	BillingMode            string          `json:"billing_mode" gorm:"size:32"`
	MatchedTier            string          `json:"matched_tier" gorm:"size:191"`
	PricingBreakdownKnown  bool            `json:"pricing_breakdown_known"`
	CacheWriteUnitKnown    bool            `json:"cache_write_unit_known"`
	GroupRatio             decimal.Decimal `json:"group_ratio" gorm:"type:decimal(24,12)"`
	GroupRatioKnown        bool            `json:"group_ratio_known"`
	InputTokens            int64           `json:"input_tokens"`
	OutputTokens           int64           `json:"output_tokens"`
	CacheReadTokens        int64           `json:"cache_read_tokens"`
	CacheWriteTokens       int64           `json:"cache_write_tokens"`
	CallCount              int64           `json:"call_count"`
	OriginalInput          decimal.Decimal `json:"original_input" gorm:"type:decimal(36,12)"`
	OriginalOutput         decimal.Decimal `json:"original_output" gorm:"type:decimal(36,12)"`
	OriginalCacheRead      decimal.Decimal `json:"original_cache_read" gorm:"type:decimal(36,12)"`
	OriginalCacheWrite     decimal.Decimal `json:"original_cache_write" gorm:"type:decimal(36,12)"`
	OriginalOther          decimal.Decimal `json:"original_other" gorm:"type:decimal(36,12)"`
	OriginalTotal          decimal.Decimal `json:"original_total" gorm:"type:decimal(36,12)"`
	AdjustedInput          decimal.Decimal `json:"adjusted_input" gorm:"type:decimal(36,12)"`
	AdjustedOutput         decimal.Decimal `json:"adjusted_output" gorm:"type:decimal(36,12)"`
	AdjustedCacheRead      decimal.Decimal `json:"adjusted_cache_read" gorm:"type:decimal(36,12)"`
	AdjustedCacheWrite     decimal.Decimal `json:"adjusted_cache_write" gorm:"type:decimal(36,12)"`
	AdjustedOther          decimal.Decimal `json:"adjusted_other" gorm:"type:decimal(36,12)"`
	AdjustedTotal          decimal.Decimal `json:"adjusted_total" gorm:"type:decimal(36,12)"`
	OriginalInputUnit      decimal.Decimal `json:"original_input_unit" gorm:"type:decimal(24,12)"`
	OriginalOutputUnit     decimal.Decimal `json:"original_output_unit" gorm:"type:decimal(24,12)"`
	OriginalCacheReadUnit  decimal.Decimal `json:"original_cache_read_unit" gorm:"type:decimal(24,12)"`
	OriginalCacheWriteUnit decimal.Decimal `json:"original_cache_write_unit" gorm:"type:decimal(24,12)"`
	AdjustedInputUnit      decimal.Decimal `json:"adjusted_input_unit" gorm:"type:decimal(24,12)"`
	AdjustedOutputUnit     decimal.Decimal `json:"adjusted_output_unit" gorm:"type:decimal(24,12)"`
	AdjustedCacheReadUnit  decimal.Decimal `json:"adjusted_cache_read_unit" gorm:"type:decimal(24,12)"`
	AdjustedCacheWriteUnit decimal.Decimal `json:"adjusted_cache_write_unit" gorm:"type:decimal(24,12)"`
	CreatedAt              int64           `json:"created_at"`
	UpdatedAt              int64           `json:"updated_at"`
}

func (BillingReportDaily) TableName() string {
	return "billing_report_daily"
}

// BillingReportState is a singleton and also provides a portable database
// lease for multi-instance deployments.
type BillingReportState struct {
	Id              int    `json:"-" gorm:"primaryKey"`
	AutoEnabled     bool   `json:"auto_enabled"`
	Initialized     bool   `json:"initialized"`
	LiveCursorId    int    `json:"live_cursor_id"`
	HistoryDate     string `json:"history_date" gorm:"size:10"`
	HistoryCursorId int    `json:"history_cursor_id"`
	HistoryCutoffId int    `json:"history_cutoff_id"`
	LastAutoRunAt   int64  `json:"last_auto_run_at"`
	LastSyncedAt    int64  `json:"last_synced_at"`
	LastSourceLogAt int64  `json:"last_source_log_at"`
	ProcessedLogs   int64  `json:"processed_logs"`
	Status          string `json:"status" gorm:"size:32"`
	LastError       string `json:"last_error" gorm:"type:text"`
	LockOwner       string `json:"-" gorm:"size:191"`
	LockUntil       int64  `json:"-"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (BillingReportState) TableName() string {
	return "billing_report_state"
}

type BillingReportJob struct {
	Id            uint64 `json:"id" gorm:"primaryKey"`
	StartDate     string `json:"start_date" gorm:"size:10"`
	EndDate       string `json:"end_date" gorm:"size:10"`
	CurrentDate   string `json:"current_date" gorm:"size:10"`
	CursorId      int    `json:"cursor_id"`
	CutoffId      int    `json:"cutoff_id"`
	Status        string `json:"status" gorm:"size:32;index"`
	ProcessedLogs int64  `json:"processed_logs"`
	ProcessedDays int    `json:"processed_days"`
	TotalDays     int    `json:"total_days"`
	ErrorMessage  string `json:"error_message" gorm:"type:text"`
	CreatedAt     int64  `json:"created_at"`
	StartedAt     int64  `json:"started_at"`
	FinishedAt    int64  `json:"finished_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

func (BillingReportJob) TableName() string {
	return "billing_report_jobs"
}

func EnsureBillingReportState(db *gorm.DB) error {
	now := time.Now().Unix()
	state := BillingReportState{
		Id:          BillingReportStateID,
		AutoEnabled: true,
		Status:      "idle",
		UpdatedAt:   now,
	}
	return db.Where("id = ?", BillingReportStateID).FirstOrCreate(&state).Error
}

type BillingReportFilters struct {
	StartDate       string
	EndDate         string
	Username        string
	UserGroup       string
	ThirdPartyGroup string
	ChannelTag      string
	ChannelName     string
	UpstreamUrl     string
	ModelName       string
	TokenName       string
}

type BillingReportTotals struct {
	CallCount        int64           `json:"call_count"`
	InputTokens      int64           `json:"input_tokens"`
	OutputTokens     int64           `json:"output_tokens"`
	CacheReadTokens  int64           `json:"cache_read_tokens"`
	CacheWriteTokens int64           `json:"cache_write_tokens"`
	OriginalTotal    decimal.Decimal `json:"original_total"`
	AdjustedTotal    decimal.Decimal `json:"adjusted_total"`
}

func applyBillingReportFilters(tx *gorm.DB, filters BillingReportFilters) *gorm.DB {
	if filters.StartDate != "" {
		tx = tx.Where("bill_date >= ?", filters.StartDate)
	}
	if filters.EndDate != "" {
		tx = tx.Where("bill_date <= ?", filters.EndDate)
	}
	contains := []struct {
		column string
		value  string
	}{
		{"username", filters.Username},
		{"user_group", filters.UserGroup},
		{"third_party_group", filters.ThirdPartyGroup},
		{"channel_tag", filters.ChannelTag},
		{"channel_name", filters.ChannelName},
		{"upstream_url", filters.UpstreamUrl},
		{"model_name", filters.ModelName},
		{"token_name", filters.TokenName},
	}
	for _, filter := range contains {
		if filter.value != "" {
			tx = tx.Where(filter.column+" LIKE ?", "%"+filter.value+"%")
		}
	}
	return tx
}

func QueryBillingReport(filters BillingReportFilters, offset int, limit int) ([]BillingReportDaily, int64, BillingReportTotals, error) {
	tx := applyBillingReportFilters(LOG_DB.Model(&BillingReportDaily{}).Where("job_id = ?", uint64(0)), filters)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, BillingReportTotals{}, err
	}
	var rows []BillingReportDaily
	if err := tx.Order("bill_date DESC, id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, BillingReportTotals{}, err
	}
	var totals BillingReportTotals
	err := applyBillingReportFilters(LOG_DB.Model(&BillingReportDaily{}).Where("job_id = ?", uint64(0)), filters).
		Select("COALESCE(SUM(call_count), 0) AS call_count, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens, COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens, COALESCE(SUM(original_total), 0) AS original_total, COALESCE(SUM(adjusted_total), 0) AS adjusted_total").
		Scan(&totals).Error
	return rows, total, totals, err
}

func CountBillingReportForExport(filters BillingReportFilters) (int64, error) {
	var total int64
	err := applyBillingReportFilters(LOG_DB.Model(&BillingReportDaily{}).Where("job_id = ?", uint64(0)), filters).
		Count(&total).Error
	return total, err
}

func IterateBillingReportForExport(filters BillingReportFilters, visit func(BillingReportDaily) error) error {
	rows, err := applyBillingReportFilters(LOG_DB.Model(&BillingReportDaily{}).Where("job_id = ?", uint64(0)), filters).
		Order("bill_date ASC, username ASC, model_name ASC, id ASC").
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row BillingReportDaily
		if err := LOG_DB.ScanRows(rows, &row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}
