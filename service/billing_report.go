package service

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	billingReportBatchSize        = 10000
	billingReportAutoInterval     = 5 * time.Minute
	billingReportWorkerInterval   = 5 * time.Second
	billingReportLeaseDuration    = 90 * time.Second
	billingReportBackfillStart    = "2026-07-01"
	billingReportStatusIdle       = "idle"
	billingReportStatusSyncing    = "syncing"
	billingReportStatusRebuilding = "rebuilding"
	billingReportStatusError      = "error"
	billingReportHistoryJobID     = uint64(1<<63 - 1)
)

var billingReportWorkerStarted atomic.Bool
var billingReportLocation = loadBillingReportLocation()
var billingReportFeatureEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("BILLING_REPORT_ENABLED")), "true")

type billingChannelSnapshot struct {
	Name        string
	Tag         string
	UpstreamUrl string
}

type billingReportSnapshot struct {
	UserGroup        string
	ThirdPartyGroup  string
	ChannelName      string
	ChannelTag       string
	UpstreamUrl      string
	QuotaAfterGroup  float64
	QuotaBeforeGroup float64
}

type billingLogPricing struct {
	ModelRatio           float64
	GroupRatio           float64
	GroupRatioKnown      bool
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreationRatio5m float64
	CacheCreationRatio1h float64
	CacheReadTokens      int64
	CacheWriteTokens     int64
	CacheWriteTokens5m   int64
	CacheWriteTokens1h   int64
	ModelPrice           float64
	BillingMode          string
	MatchedTier          string
	UsageSemantic        string
	TierPrices           billingexpr.TierUnitPrices
	TierPricesKnown      bool
	Snapshot             billingReportSnapshot
}

type BillingReportStatus struct {
	Enabled           bool                     `json:"enabled"`
	State             model.BillingReportState `json:"state"`
	ActiveJob         *model.BillingReportJob  `json:"active_job,omitempty"`
	PendingJobs       int64                    `json:"pending_jobs"`
	AutoIntervalSec   int64                    `json:"auto_interval_seconds"`
	BatchSize         int                      `json:"batch_size"`
	BackfillStartDate string                   `json:"backfill_start_date"`
}

func loadBillingReportLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		return location
	}
	return time.FixedZone("CST", 8*60*60)
}

func BillingReportEnabled() bool {
	return billingReportFeatureEnabled
}

func StartBillingReportWorker() {
	if !BillingReportEnabled() || !billingReportWorkerStarted.CompareAndSwap(false, true) {
		return
	}
	if err := model.EnsureBillingReportState(model.LOG_DB); err != nil {
		common.SysError("billing report state initialization failed: " + err.Error())
		billingReportWorkerStarted.Store(false)
		return
	}
	go func() {
		common.SysLog("billing report worker started")
		ticker := time.NewTicker(billingReportWorkerInterval)
		defer ticker.Stop()
		runBillingReportWorkerOnce()
		for range ticker.C {
			runBillingReportWorkerOnce()
		}
	}()
}

func billingReportOwner() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func acquireBillingReportLease(owner string) (bool, error) {
	now := time.Now().Unix()
	result := model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ? AND (lock_until < ? OR lock_owner = ?)", model.BillingReportStateID, now, owner).
		Updates(map[string]interface{}{
			"lock_owner": owner,
			"lock_until": now + int64(billingReportLeaseDuration/time.Second),
			"updated_at": now,
		})
	return result.RowsAffected == 1, result.Error
}

func renewBillingReportLease(tx *gorm.DB, owner string) error {
	now := time.Now().Unix()
	nextExpiry := now + int64(billingReportLeaseDuration/time.Second)
	result := tx.Model(&model.BillingReportState{}).
		Where("id = ? AND lock_owner = ?", model.BillingReportStateID, owner).
		Updates(map[string]interface{}{
			// MySQL reports zero affected rows when an UPDATE writes the same
			// values. Always advance the lease by at least one second so a
			// successful renewal cannot be mistaken for a lost lease.
			"lock_until": gorm.Expr(
				"CASE WHEN lock_until >= ? THEN lock_until + 1 ELSE ? END",
				nextExpiry,
				nextExpiry,
			),
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("billing report lease lost")
	}
	return nil
}

func releaseBillingReportLease(owner string) {
	now := time.Now().Unix()
	_ = model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ? AND lock_owner = ?", model.BillingReportStateID, owner).
		Updates(map[string]interface{}{
			"lock_owner": "",
			"lock_until": 0,
			"updated_at": now,
		}).Error
}

func runBillingReportWorkerOnce() {
	if !BillingReportEnabled() {
		return
	}
	owner := billingReportOwner()
	acquired, err := acquireBillingReportLease(owner)
	if err != nil || !acquired {
		return
	}
	defer releaseBillingReportLease(owner)

	if err := initializeBillingReportState(owner); err != nil {
		setBillingReportError(err)
		return
	}

	job, err := nextBillingReportJob()
	if err != nil {
		setBillingReportError(err)
		return
	}
	hadJob := job != nil
	if job != nil {
		if err := processBillingReportJobBatch(owner, job); err != nil {
			failBillingReportJob(job.Id, err)
			return
		}
	}

	var state model.BillingReportState
	if err := model.LOG_DB.First(&state, model.BillingReportStateID).Error; err != nil {
		setBillingReportError(err)
		return
	}
	if !state.AutoEnabled {
		return
	}

	now := time.Now().Unix()
	if state.LastAutoRunAt == 0 || now-state.LastAutoRunAt >= int64(billingReportAutoInterval/time.Second) {
		if err := syncBillingReportLive(owner); err != nil {
			setBillingReportError(err)
			return
		}
	}
	if hadJob {
		return
	}
	if err := processBillingReportHistoryBatch(owner); err != nil {
		setBillingReportError(err)
	}
}

func initializeBillingReportState(owner string) error {
	var state model.BillingReportState
	if err := model.LOG_DB.First(&state, model.BillingReportStateID).Error; err != nil {
		return err
	}
	if state.Initialized {
		return nil
	}
	today := time.Now().In(billingReportLocation).Format("2006-01-02")
	todayStart, _ := time.ParseInLocation("2006-01-02", today, billingReportLocation)
	var liveCursor int
	if err := model.LOG_DB.Model(&model.Log{}).
		Where("created_at < ?", todayStart.Unix()).
		Select("COALESCE(MAX(id), 0)").
		Scan(&liveCursor).Error; err != nil {
		return err
	}
	var historyCutoff int
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&historyCutoff).Error; err != nil {
		return err
	}
	now := time.Now().Unix()
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		return tx.Model(&model.BillingReportState{}).
			Where("id = ? AND initialized = ?", model.BillingReportStateID, false).
			Updates(map[string]interface{}{
				"initialized":       true,
				"live_cursor_id":    liveCursor,
				"history_date":      billingReportBackfillStart,
				"history_cursor_id": 0,
				"history_cutoff_id": historyCutoff,
				"status":            billingReportStatusIdle,
				"last_error":        "",
				"updated_at":        now,
			}).Error
	})
}

func setBillingReportError(err error) {
	if err == nil {
		return
	}
	common.SysError("billing report: " + err.Error())
	now := time.Now().Unix()
	_ = model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ?", model.BillingReportStateID).
		Updates(map[string]interface{}{
			"status":     billingReportStatusError,
			"last_error": err.Error(),
			"updated_at": now,
		}).Error
}

func nextBillingReportJob() (*model.BillingReportJob, error) {
	var job model.BillingReportJob
	result := model.LOG_DB.
		Where("status IN ?", []string{model.BillingReportJobRunning, model.BillingReportJobPending}).
		Order("CASE WHEN status = 'running' THEN 0 ELSE 1 END, id ASC").
		Limit(1).
		Find(&job)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &job, nil
}

func syncBillingReportLive(owner string) error {
	var state model.BillingReportState
	if err := model.LOG_DB.First(&state, model.BillingReportStateID).Error; err != nil {
		return err
	}
	var highWatermark int
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&highWatermark).Error; err != nil {
		return err
	}
	now := time.Now().Unix()
	if err := model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ?", model.BillingReportStateID).
		Updates(map[string]interface{}{
			"status":           billingReportStatusSyncing,
			"last_auto_run_at": now,
			"last_error":       "",
			"updated_at":       now,
		}).Error; err != nil {
		return err
	}

	cursor := state.LiveCursorId
	for cursor < highWatermark {
		logs, err := fetchBillingLogs(cursor, highWatermark, "", "")
		if err != nil {
			return err
		}
		if len(logs) == 0 {
			return advanceLiveCursor(owner, highWatermark, 0, 0)
		}
		aggregates, latestLogAt := buildBillingAggregates(logs)
		nextCursor := logs[len(logs)-1].Id
		if err := model.LOG_DB.Transaction(func(tx *gorm.DB) error {
			if err := renewBillingReportLease(tx, owner); err != nil {
				return err
			}
			if err := applyBillingAggregates(tx, aggregates, false, 0); err != nil {
				return err
			}
			return updateLiveCursor(tx, nextCursor, int64(len(logs)), latestLogAt)
		}); err != nil {
			return err
		}
		cursor = nextCursor
	}
	return model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ?", model.BillingReportStateID).
		Updates(map[string]interface{}{
			"status":     billingReportStatusIdle,
			"last_error": "",
			"updated_at": time.Now().Unix(),
		}).Error
}

func advanceLiveCursor(owner string, cursor int, processed int64, latestLogAt int64) error {
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		return updateLiveCursor(tx, cursor, processed, latestLogAt)
	})
}

func updateLiveCursor(tx *gorm.DB, cursor int, processed int64, latestLogAt int64) error {
	updates := map[string]interface{}{
		"live_cursor_id": cursor,
		"last_synced_at": time.Now().Unix(),
		"status":         billingReportStatusIdle,
		"last_error":     "",
		"updated_at":     time.Now().Unix(),
	}
	if processed > 0 {
		updates["processed_logs"] = gorm.Expr("processed_logs + ?", processed)
	}
	if latestLogAt > 0 {
		updates["last_source_log_at"] = latestLogAt
	}
	return tx.Model(&model.BillingReportState{}).
		Where("id = ?", model.BillingReportStateID).
		Updates(updates).Error
}

func processBillingReportHistoryBatch(owner string) error {
	var state model.BillingReportState
	if err := model.LOG_DB.First(&state, model.BillingReportStateID).Error; err != nil {
		return err
	}
	if state.HistoryDate == "" {
		return nil
	}
	historyDate, err := time.ParseInLocation("2006-01-02", state.HistoryDate, billingReportLocation)
	if err != nil {
		return err
	}
	yesterday := time.Now().In(billingReportLocation).AddDate(0, 0, -1)
	if historyDate.After(yesterday) {
		return nil
	}

	startUnix := historyDate.Unix()
	endUnix := historyDate.AddDate(0, 0, 1).Unix()
	logs, err := fetchBillingLogs(state.HistoryCursorId, state.HistoryCutoffId, strconv.FormatInt(startUnix, 10), strconv.FormatInt(endUnix, 10))
	if err != nil {
		return err
	}
	if len(logs) == 0 {
		return finalizeHistoryDay(owner, state.HistoryDate)
	}
	aggregates, latestLogAt := buildBillingAggregates(logs)
	nextCursor := logs[len(logs)-1].Id
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		if state.HistoryCursorId == 0 {
			if err := tx.Where("job_id = ? AND bill_date = ?", billingReportHistoryJobID, state.HistoryDate).
				Delete(&model.BillingReportDaily{}).Error; err != nil {
				return err
			}
		}
		if err := applyBillingAggregates(tx, aggregates, true, billingReportHistoryJobID); err != nil {
			return err
		}
		return tx.Model(&model.BillingReportState{}).
			Where("id = ?", model.BillingReportStateID).
			Updates(map[string]interface{}{
				"history_cursor_id":  nextCursor,
				"processed_logs":     gorm.Expr("processed_logs + ?", len(logs)),
				"last_source_log_at": latestLogAt,
				"status":             billingReportStatusRebuilding,
				"last_error":         "",
				"updated_at":         time.Now().Unix(),
			}).Error
	})
}

func finalizeHistoryDay(owner string, billDate string) error {
	nextDate, err := time.ParseInLocation("2006-01-02", billDate, billingReportLocation)
	if err != nil {
		return err
	}
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		if err := replaceBillingReportDayFromStaging(tx, billingReportHistoryJobID, billDate); err != nil {
			return err
		}
		return tx.Model(&model.BillingReportState{}).
			Where("id = ?", model.BillingReportStateID).
			Updates(map[string]interface{}{
				"history_date":      nextDate.AddDate(0, 0, 1).Format("2006-01-02"),
				"history_cursor_id": 0,
				"status":            billingReportStatusIdle,
				"last_error":        "",
				"updated_at":        time.Now().Unix(),
			}).Error
	})
}

func fetchBillingLogs(cursor int, cutoff int, startUnix string, endUnix string) ([]model.Log, error) {
	tx := model.LOG_DB.Model(&model.Log{}).
		Select([]string{"id", "user_id", "created_at", "username", "token_name", "model_name", "quota", "prompt_tokens", "completion_tokens", "channel_id", "token_id", "group", "other"}).
		Where("type = ? AND id > ? AND id <= ?", model.LogTypeConsume, cursor, cutoff)
	if startUnix != "" {
		tx = tx.Where("created_at >= ?", startUnix)
	}
	if endUnix != "" {
		tx = tx.Where("created_at < ?", endUnix)
	}
	var logs []model.Log
	err := tx.Order("id ASC").Limit(billingReportBatchSize).Find(&logs).Error
	return logs, err
}

func buildBillingAggregates(logs []model.Log) (map[string]*model.BillingReportDaily, int64) {
	channels := loadBillingChannelSnapshots(logs)
	userGroups := loadBillingUserGroups(logs)
	aggregates := make(map[string]*model.BillingReportDaily)
	var latestLogAt int64
	for i := range logs {
		log := &logs[i]
		if log.CreatedAt > latestLogAt {
			latestLogAt = log.CreatedAt
		}
		row := billingRowFromLog(log, channels[log.ChannelId], userGroups[log.UserId])
		if existing, ok := aggregates[row.BillDate+"|"+row.BucketKey]; ok {
			addBillingRows(existing, row)
		} else {
			aggregates[row.BillDate+"|"+row.BucketKey] = row
		}
	}
	return aggregates, latestLogAt
}

func loadBillingUserGroups(logs []model.Log) map[int]string {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for i := range logs {
		if logs[i].UserId == 0 {
			continue
		}
		if _, ok := seen[logs[i].UserId]; ok {
			continue
		}
		seen[logs[i].UserId] = struct{}{}
		ids = append(ids, logs[i].UserId)
	}
	result := make(map[int]string)
	if len(ids) == 0 {
		return result
	}
	var users []struct {
		Id    int
		Group string
	}
	if err := model.DB.Model(&model.User{}).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return result
	}
	for i := range users {
		result[users[i].Id] = users[i].Group
	}
	return result
}

func loadBillingChannelSnapshots(logs []model.Log) map[int]billingChannelSnapshot {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for i := range logs {
		if logs[i].ChannelId == 0 {
			continue
		}
		if _, ok := seen[logs[i].ChannelId]; ok {
			continue
		}
		seen[logs[i].ChannelId] = struct{}{}
		ids = append(ids, logs[i].ChannelId)
	}
	result := make(map[int]billingChannelSnapshot)
	if len(ids) == 0 {
		return result
	}
	var channels []model.Channel
	if err := model.DB.Select("id, name, tag, base_url").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return result
	}
	for i := range channels {
		result[channels[i].Id] = billingChannelSnapshot{
			Name:        channels[i].Name,
			Tag:         channels[i].GetTag(),
			UpstreamUrl: channels[i].GetBaseURL(),
		}
	}
	return result
}

func billingRowFromLog(log *model.Log, channel billingChannelSnapshot, currentUserGroup string) *model.BillingReportDaily {
	pricing := parseBillingLogPricing(log.Other)
	if pricing.Snapshot.ChannelName != "" {
		channel.Name = pricing.Snapshot.ChannelName
	}
	if pricing.Snapshot.ChannelTag != "" {
		channel.Tag = pricing.Snapshot.ChannelTag
	}
	if pricing.Snapshot.UpstreamUrl != "" {
		channel.UpstreamUrl = pricing.Snapshot.UpstreamUrl
	}
	userGroup := pricing.Snapshot.UserGroup
	if userGroup == "" {
		userGroup = currentUserGroup
	}
	thirdPartyGroup := pricing.Snapshot.ThirdPartyGroup
	if thirdPartyGroup == "" {
		thirdPartyGroup = log.Group
	}

	inputTokens := int64(log.PromptTokens)
	if pricing.UsageSemantic != "anthropic" {
		if pricing.BillingMode == "tiered_expr" && pricing.TierPricesKnown {
			if pricing.TierPrices.UsedVars["cr"] {
				inputTokens -= pricing.CacheReadTokens
			}
			if pricing.CacheWriteTokens5m > 0 || pricing.CacheWriteTokens1h > 0 {
				if pricing.TierPrices.UsedVars["cc"] {
					inputTokens -= pricing.CacheWriteTokens5m
				}
				if pricing.TierPrices.UsedVars["cc1h"] {
					inputTokens -= pricing.CacheWriteTokens1h
				}
			} else if pricing.TierPrices.UsedVars["cc"] {
				inputTokens -= pricing.CacheWriteTokens
			}
		} else {
			inputTokens -= pricing.CacheReadTokens + pricing.CacheWriteTokens
		}
		if inputTokens < 0 {
			inputTokens = 0
		}
	}

	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	million := decimal.NewFromInt(1_000_000)
	groupRatio := decimal.NewFromFloat(pricing.GroupRatio)
	modelRatio := decimal.NewFromFloat(pricing.ModelRatio)
	inputUnit := modelRatio.Mul(million).Div(quotaPerUnit)
	outputUnit := inputUnit.Mul(decimal.NewFromFloat(pricing.CompletionRatio))
	cacheReadUnit := inputUnit.Mul(decimal.NewFromFloat(pricing.CacheRatio))
	cacheWrite5mUnit := inputUnit.Mul(decimal.NewFromFloat(pricing.CacheCreationRatio5m))
	cacheWrite1hUnit := inputUnit.Mul(decimal.NewFromFloat(pricing.CacheCreationRatio1h))
	cacheWriteUnit := inputUnit.Mul(decimal.NewFromFloat(pricing.CacheCreationRatio))
	pricingBreakdownKnown := pricing.BillingMode == "token"
	if pricing.BillingMode == "tiered_expr" {
		pricingBreakdownKnown = pricing.TierPricesKnown
		if pricingBreakdownKnown {
			inputUnit = decimal.NewFromFloat(pricing.TierPrices.Input)
			outputUnit = decimal.NewFromFloat(pricing.TierPrices.Output)
			cacheReadUnit = decimal.NewFromFloat(pricing.TierPrices.CacheRead)
			cacheWrite5mUnit = decimal.NewFromFloat(pricing.TierPrices.CacheWrite)
			cacheWrite1hUnit = decimal.NewFromFloat(pricing.TierPrices.CacheWrite1h)
			cacheWriteUnit = cacheWrite5mUnit
		} else {
			inputUnit = decimal.Zero
			outputUnit = decimal.Zero
			cacheReadUnit = decimal.Zero
			cacheWrite5mUnit = decimal.Zero
			cacheWrite1hUnit = decimal.Zero
			cacheWriteUnit = decimal.Zero
		}
	}
	if !pricingBreakdownKnown {
		inputUnit = decimal.Zero
		outputUnit = decimal.Zero
		cacheReadUnit = decimal.Zero
		cacheWrite5mUnit = decimal.Zero
		cacheWrite1hUnit = decimal.Zero
		cacheWriteUnit = decimal.Zero
	}

	originalInput := decimal.NewFromInt(inputTokens).Mul(inputUnit).Div(million)
	originalOutput := decimal.NewFromInt(int64(log.CompletionTokens)).Mul(outputUnit).Div(million)
	originalCacheRead := decimal.NewFromInt(pricing.CacheReadTokens).Mul(cacheReadUnit).Div(million)
	originalCacheWrite := decimal.Zero
	if pricing.CacheWriteTokens5m > 0 || pricing.CacheWriteTokens1h > 0 {
		originalCacheWrite = decimal.NewFromInt(pricing.CacheWriteTokens5m).Mul(cacheWrite5mUnit).Div(million).
			Add(decimal.NewFromInt(pricing.CacheWriteTokens1h).Mul(cacheWrite1hUnit).Div(million))
	} else {
		originalCacheWrite = decimal.NewFromInt(pricing.CacheWriteTokens).Mul(cacheWriteUnit).Div(million)
	}
	if pricing.CacheWriteTokens > 0 {
		cacheWriteUnit = originalCacheWrite.Mul(million).Div(decimal.NewFromInt(pricing.CacheWriteTokens))
	}

	adjustedTotal := decimal.NewFromInt(int64(log.Quota)).Div(quotaPerUnit)
	originalTotal := decimal.Zero
	if pricing.GroupRatioKnown {
		beforeQuota := pricing.Snapshot.QuotaBeforeGroup
		if beforeQuota <= 0 && pricing.GroupRatio > 0 {
			beforeQuota = float64(log.Quota) / pricing.GroupRatio
		}
		if beforeQuota <= 0 && pricing.ModelPrice > 0 {
			beforeQuota = pricing.ModelPrice * common.QuotaPerUnit
		}
		if beforeQuota > 0 {
			originalTotal = decimal.NewFromFloat(beforeQuota).Div(quotaPerUnit)
		}
	}
	if originalTotal.IsZero() && pricing.GroupRatioKnown && adjustedTotal.IsZero() {
		originalTotal = originalInput.Add(originalOutput).Add(originalCacheRead).Add(originalCacheWrite)
	}

	adjustedInput := originalInput.Mul(groupRatio)
	adjustedOutput := originalOutput.Mul(groupRatio)
	adjustedCacheRead := originalCacheRead.Mul(groupRatio)
	adjustedCacheWrite := originalCacheWrite.Mul(groupRatio)
	originalOther := originalTotal.Sub(originalInput).Sub(originalOutput).Sub(originalCacheRead).Sub(originalCacheWrite)
	adjustedOther := adjustedTotal.Sub(adjustedInput).Sub(adjustedOutput).Sub(adjustedCacheRead).Sub(adjustedCacheWrite)

	billDate := time.Unix(log.CreatedAt, 0).In(billingReportLocation).Format("2006-01-02")
	bucketSource := strings.Join([]string{
		billDate,
		strconv.Itoa(log.UserId),
		log.Username,
		userGroup,
		thirdPartyGroup,
		strconv.Itoa(log.ChannelId),
		channel.Name,
		channel.Tag,
		channel.UpstreamUrl,
		log.ModelName,
		strconv.Itoa(log.TokenId),
		log.TokenName,
		pricing.BillingMode,
		pricing.MatchedTier,
		strconv.FormatBool(pricingBreakdownKnown),
		strconv.FormatBool(pricing.GroupRatioKnown),
		groupRatio.String(),
		inputUnit.String(),
		outputUnit.String(),
		cacheReadUnit.String(),
		cacheWriteUnit.String(),
	}, "\x1f")
	bucketKey := fmt.Sprintf("%x", sha256.Sum256([]byte(bucketSource)))
	now := time.Now().Unix()
	return &model.BillingReportDaily{
		BillDate:               billDate,
		BucketKey:              bucketKey,
		UserId:                 log.UserId,
		Username:               log.Username,
		UserGroup:              userGroup,
		ThirdPartyGroup:        thirdPartyGroup,
		ChannelId:              log.ChannelId,
		ChannelName:            channel.Name,
		ChannelTag:             channel.Tag,
		UpstreamUrl:            channel.UpstreamUrl,
		ModelName:              log.ModelName,
		TokenId:                log.TokenId,
		TokenName:              log.TokenName,
		BillingMode:            pricing.BillingMode,
		MatchedTier:            pricing.MatchedTier,
		PricingBreakdownKnown:  pricingBreakdownKnown,
		GroupRatio:             groupRatio,
		GroupRatioKnown:        pricing.GroupRatioKnown,
		InputTokens:            inputTokens,
		OutputTokens:           int64(log.CompletionTokens),
		CacheReadTokens:        pricing.CacheReadTokens,
		CacheWriteTokens:       pricing.CacheWriteTokens,
		CallCount:              1,
		OriginalInput:          originalInput,
		OriginalOutput:         originalOutput,
		OriginalCacheRead:      originalCacheRead,
		OriginalCacheWrite:     originalCacheWrite,
		OriginalOther:          originalOther,
		OriginalTotal:          originalTotal,
		AdjustedInput:          adjustedInput,
		AdjustedOutput:         adjustedOutput,
		AdjustedCacheRead:      adjustedCacheRead,
		AdjustedCacheWrite:     adjustedCacheWrite,
		AdjustedOther:          adjustedOther,
		AdjustedTotal:          adjustedTotal,
		OriginalInputUnit:      inputUnit,
		OriginalOutputUnit:     outputUnit,
		OriginalCacheReadUnit:  cacheReadUnit,
		OriginalCacheWriteUnit: cacheWriteUnit,
		AdjustedInputUnit:      inputUnit.Mul(groupRatio),
		AdjustedOutputUnit:     outputUnit.Mul(groupRatio),
		AdjustedCacheReadUnit:  cacheReadUnit.Mul(groupRatio),
		AdjustedCacheWriteUnit: cacheWriteUnit.Mul(groupRatio),
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

func parseBillingLogPricing(otherText string) billingLogPricing {
	pricing := billingLogPricing{
		CompletionRatio:      1,
		CacheRatio:           1,
		CacheCreationRatio:   1,
		CacheCreationRatio5m: 1,
		CacheCreationRatio1h: 1,
	}
	other, err := common.StrToMap(otherText)
	if err != nil {
		return pricing
	}
	pricing.ModelRatio = floatValue(other["model_ratio"])
	if value, ok := numberValue(other["group_ratio"]); ok {
		pricing.GroupRatio = value
		pricing.GroupRatioKnown = true
	}
	if value, ok := numberValue(other["completion_ratio"]); ok {
		pricing.CompletionRatio = value
	}
	if value, ok := numberValue(other["cache_ratio"]); ok {
		pricing.CacheRatio = value
	}
	if value, ok := numberValue(other["cache_creation_ratio"]); ok {
		pricing.CacheCreationRatio = value
	}
	if value, ok := numberValue(other["cache_creation_ratio_5m"]); ok {
		pricing.CacheCreationRatio5m = value
	} else {
		pricing.CacheCreationRatio5m = pricing.CacheCreationRatio
	}
	if value, ok := numberValue(other["cache_creation_ratio_1h"]); ok {
		pricing.CacheCreationRatio1h = value
	} else {
		pricing.CacheCreationRatio1h = pricing.CacheCreationRatio
	}
	pricing.CacheReadTokens = int64(floatValue(other["cache_tokens"]))
	pricing.CacheWriteTokens5m = int64(floatValue(other["cache_creation_tokens_5m"]))
	pricing.CacheWriteTokens1h = int64(floatValue(other["cache_creation_tokens_1h"]))
	pricing.CacheWriteTokens = int64(floatValue(other["cache_write_tokens"]))
	if pricing.CacheWriteTokens == 0 {
		if pricing.CacheWriteTokens5m > 0 || pricing.CacheWriteTokens1h > 0 {
			pricing.CacheWriteTokens = pricing.CacheWriteTokens5m + pricing.CacheWriteTokens1h
		} else {
			pricing.CacheWriteTokens = int64(floatValue(other["cache_creation_tokens"]))
		}
	}
	pricing.ModelPrice = floatValue(other["model_price"])
	pricing.BillingMode = stringValue(other["billing_mode"])
	if pricing.BillingMode == "" {
		if pricing.ModelPrice > 0 && pricing.ModelRatio == 0 {
			pricing.BillingMode = "fixed"
		} else {
			pricing.BillingMode = "token"
		}
	}
	pricing.MatchedTier = stringValue(other["matched_tier"])
	pricing.UsageSemantic = stringValue(other["usage_semantic"])
	if pricing.BillingMode == "tiered_expr" && pricing.MatchedTier != "" {
		encodedExpression := stringValue(other["expr_b64"])
		if decodedExpression, decodeErr := base64.StdEncoding.DecodeString(encodedExpression); decodeErr == nil {
			pricing.TierPrices, pricing.TierPricesKnown = billingexpr.ExtractTierUnitPrices(
				string(decodedExpression),
				pricing.MatchedTier,
			)
		}
	}
	if snapshot, ok := other["billing_report"].(map[string]interface{}); ok {
		pricing.Snapshot = billingReportSnapshot{
			UserGroup:        stringValue(snapshot["user_group"]),
			ThirdPartyGroup:  stringValue(snapshot["third_party_group"]),
			ChannelName:      stringValue(snapshot["channel_name"]),
			ChannelTag:       stringValue(snapshot["channel_tag"]),
			UpstreamUrl:      stringValue(snapshot["upstream_url"]),
			QuotaAfterGroup:  floatValue(snapshot["quota_after_group"]),
			QuotaBeforeGroup: floatValue(snapshot["quota_before_group"]),
		}
	}
	return pricing
}

func numberValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func floatValue(value interface{}) float64 {
	result, _ := numberValue(value)
	return result
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func addBillingRows(target *model.BillingReportDaily, value *model.BillingReportDaily) {
	target.InputTokens += value.InputTokens
	target.OutputTokens += value.OutputTokens
	target.CacheReadTokens += value.CacheReadTokens
	target.CacheWriteTokens += value.CacheWriteTokens
	target.CallCount += value.CallCount
	target.OriginalInput = target.OriginalInput.Add(value.OriginalInput)
	target.OriginalOutput = target.OriginalOutput.Add(value.OriginalOutput)
	target.OriginalCacheRead = target.OriginalCacheRead.Add(value.OriginalCacheRead)
	target.OriginalCacheWrite = target.OriginalCacheWrite.Add(value.OriginalCacheWrite)
	target.OriginalOther = target.OriginalOther.Add(value.OriginalOther)
	target.OriginalTotal = target.OriginalTotal.Add(value.OriginalTotal)
	target.AdjustedInput = target.AdjustedInput.Add(value.AdjustedInput)
	target.AdjustedOutput = target.AdjustedOutput.Add(value.AdjustedOutput)
	target.AdjustedCacheRead = target.AdjustedCacheRead.Add(value.AdjustedCacheRead)
	target.AdjustedCacheWrite = target.AdjustedCacheWrite.Add(value.AdjustedCacheWrite)
	target.AdjustedOther = target.AdjustedOther.Add(value.AdjustedOther)
	target.AdjustedTotal = target.AdjustedTotal.Add(value.AdjustedTotal)
	target.UpdatedAt = time.Now().Unix()
}

func applyBillingAggregates(tx *gorm.DB, aggregates map[string]*model.BillingReportDaily, staging bool, jobId uint64) error {
	if len(aggregates) == 0 {
		return nil
	}
	if !staging {
		jobId = 0
	}
	rows := make([]model.BillingReportDaily, 0, len(aggregates))
	for _, aggregate := range aggregates {
		aggregate.Id = 0
		aggregate.JobId = jobId
		rows = append(rows, *aggregate)
	}
	return tx.Clauses(billingReportUpsertClause(tx)).
		CreateInBatches(rows, 500).Error
}

func billingReportUpsertClause(tx *gorm.DB) clause.OnConflict {
	conflictColumns := []clause.Column{
		{Name: "job_id"},
		{Name: "bill_date"},
		{Name: "bucket_key"},
	}
	incoming := func(column string) clause.Expr {
		if tx.Dialector.Name() == "mysql" {
			return gorm.Expr(column + " + VALUES(" + column + ")")
		}
		return gorm.Expr(column + " + excluded." + column)
	}
	assignments := map[string]interface{}{}
	for _, column := range []string{
		"input_tokens",
		"output_tokens",
		"cache_read_tokens",
		"cache_write_tokens",
		"call_count",
		"original_input",
		"original_output",
		"original_cache_read",
		"original_cache_write",
		"original_other",
		"original_total",
		"adjusted_input",
		"adjusted_output",
		"adjusted_cache_read",
		"adjusted_cache_write",
		"adjusted_other",
		"adjusted_total",
	} {
		assignments[column] = incoming(column)
	}
	if tx.Dialector.Name() == "mysql" {
		assignments["updated_at"] = gorm.Expr("VALUES(updated_at)")
	} else {
		assignments["updated_at"] = gorm.Expr("excluded.updated_at")
	}
	return clause.OnConflict{
		Columns:   conflictColumns,
		DoUpdates: clause.Assignments(assignments),
	}
}

func CreateBillingReportJob(startDate string, endDate string) (*model.BillingReportJob, error) {
	if !BillingReportEnabled() {
		return nil, errors.New("billing report module is disabled")
	}
	start, err := time.ParseInLocation("2006-01-02", startDate, billingReportLocation)
	if err != nil {
		return nil, errors.New("invalid start date")
	}
	end, err := time.ParseInLocation("2006-01-02", endDate, billingReportLocation)
	if err != nil {
		return nil, errors.New("invalid end date")
	}
	if end.Before(start) {
		return nil, errors.New("end date must not be before start date")
	}
	var active int64
	if err := model.LOG_DB.Model(&model.BillingReportJob{}).
		Where("status IN ?", []string{model.BillingReportJobPending, model.BillingReportJobRunning}).
		Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, errors.New("another billing rebuild is already running")
	}
	var cutoff int
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&cutoff).Error; err != nil {
		return nil, err
	}
	totalDays := int(end.Sub(start).Hours()/24) + 1
	now := time.Now().Unix()
	job := &model.BillingReportJob{
		StartDate:   startDate,
		EndDate:     endDate,
		CurrentDate: startDate,
		CutoffId:    cutoff,
		Status:      model.BillingReportJobPending,
		TotalDays:   totalDays,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := model.LOG_DB.Create(job).Error; err != nil {
		return nil, err
	}
	return job, nil
}

func processBillingReportJobBatch(owner string, job *model.BillingReportJob) error {
	currentDate, err := time.ParseInLocation("2006-01-02", job.CurrentDate, billingReportLocation)
	if err != nil {
		return err
	}
	if job.Status == model.BillingReportJobPending {
		if err := model.LOG_DB.Transaction(func(tx *gorm.DB) error {
			if err := renewBillingReportLease(tx, owner); err != nil {
				return err
			}
			if err := tx.Where("job_id = ?", job.Id).Delete(&model.BillingReportDaily{}).Error; err != nil {
				return err
			}
			return tx.Model(&model.BillingReportJob{}).Where("id = ?", job.Id).
				Updates(map[string]interface{}{
					"status":     model.BillingReportJobRunning,
					"started_at": time.Now().Unix(),
					"updated_at": time.Now().Unix(),
				}).Error
		}); err != nil {
			return err
		}
		job.Status = model.BillingReportJobRunning
	}
	startUnix := currentDate.Unix()
	endUnix := currentDate.AddDate(0, 0, 1).Unix()
	logs, err := fetchBillingLogs(job.CursorId, job.CutoffId, strconv.FormatInt(startUnix, 10), strconv.FormatInt(endUnix, 10))
	if err != nil {
		return err
	}
	if len(logs) == 0 {
		return finalizeBillingReportJobDay(owner, job)
	}
	aggregates, _ := buildBillingAggregates(logs)
	nextCursor := logs[len(logs)-1].Id
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		if err := applyBillingAggregates(tx, aggregates, true, job.Id); err != nil {
			return err
		}
		return tx.Model(&model.BillingReportJob{}).Where("id = ?", job.Id).
			Updates(map[string]interface{}{
				"cursor_id":      nextCursor,
				"processed_logs": gorm.Expr("processed_logs + ?", len(logs)),
				"updated_at":     time.Now().Unix(),
			}).Error
	})
}

func finalizeBillingReportJobDay(owner string, job *model.BillingReportJob) error {
	current, err := time.ParseInLocation("2006-01-02", job.CurrentDate, billingReportLocation)
	if err != nil {
		return err
	}
	end, err := time.ParseInLocation("2006-01-02", job.EndDate, billingReportLocation)
	if err != nil {
		return err
	}
	next := current.AddDate(0, 0, 1)
	finished := next.After(end)
	return model.LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := renewBillingReportLease(tx, owner); err != nil {
			return err
		}
		if err := replaceBillingReportDayFromStaging(tx, job.Id, job.CurrentDate); err != nil {
			return err
		}
		jobUpdates := map[string]interface{}{
			"processed_days": gorm.Expr("processed_days + 1"),
			"cursor_id":      0,
			"updated_at":     time.Now().Unix(),
		}
		if finished {
			jobUpdates["status"] = model.BillingReportJobCompleted
			jobUpdates["finished_at"] = time.Now().Unix()
		} else {
			jobUpdates["current_date"] = next.Format("2006-01-02")
		}
		if err := tx.Model(&model.BillingReportJob{}).Where("id = ?", job.Id).Updates(jobUpdates).Error; err != nil {
			return err
		}

		today := time.Now().In(billingReportLocation).Format("2006-01-02")
		if job.CurrentDate == today {
			if err := tx.Model(&model.BillingReportState{}).
				Where("id = ?", model.BillingReportStateID).
				Update("live_cursor_id", job.CutoffId).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func replaceBillingReportDayFromStaging(tx *gorm.DB, jobId uint64, billDate string) error {
	if err := tx.Where("job_id = ? AND bill_date = ?", uint64(0), billDate).
		Delete(&model.BillingReportDaily{}).Error; err != nil {
		return err
	}
	return tx.Model(&model.BillingReportDaily{}).
		Where("job_id = ? AND bill_date = ?", jobId, billDate).
		Updates(map[string]interface{}{
			"job_id":     uint64(0),
			"updated_at": time.Now().Unix(),
		}).Error
}

func failBillingReportJob(jobId uint64, err error) {
	setBillingReportError(err)
	_ = model.LOG_DB.Model(&model.BillingReportJob{}).Where("id = ?", jobId).
		Updates(map[string]interface{}{
			"status":        model.BillingReportJobFailed,
			"error_message": err.Error(),
			"finished_at":   time.Now().Unix(),
			"updated_at":    time.Now().Unix(),
		}).Error
}

func SetBillingReportAutoEnabled(enabled bool) error {
	if !BillingReportEnabled() {
		return errors.New("billing report module is disabled")
	}
	if err := model.EnsureBillingReportState(model.LOG_DB); err != nil {
		return err
	}
	return model.LOG_DB.Model(&model.BillingReportState{}).
		Where("id = ?", model.BillingReportStateID).
		Updates(map[string]interface{}{
			"auto_enabled": enabled,
			"updated_at":   time.Now().Unix(),
		}).Error
}

func GetBillingReportStatus() (BillingReportStatus, error) {
	status := BillingReportStatus{
		Enabled:           BillingReportEnabled(),
		AutoIntervalSec:   int64(billingReportAutoInterval / time.Second),
		BatchSize:         billingReportBatchSize,
		BackfillStartDate: billingReportBackfillStart,
	}
	if !status.Enabled {
		return status, nil
	}
	if err := model.EnsureBillingReportState(model.LOG_DB); err != nil {
		return status, err
	}
	if err := model.LOG_DB.First(&status.State, model.BillingReportStateID).Error; err != nil {
		return status, err
	}
	var active model.BillingReportJob
	result := model.LOG_DB.Where("status IN ?", []string{model.BillingReportJobRunning, model.BillingReportJobPending}).
		Order("id ASC").
		Limit(1).
		Find(&active)
	if result.Error != nil {
		return status, result.Error
	}
	if result.RowsAffected > 0 {
		status.ActiveJob = &active
	}
	if err := model.LOG_DB.Model(&model.BillingReportJob{}).
		Where("status = ?", model.BillingReportJobPending).
		Count(&status.PendingJobs).Error; err != nil {
		return status, err
	}
	return status, nil
}
