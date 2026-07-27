package controller

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

var billingReportExportRunning atomic.Bool

type billingReportAutoRequest struct {
	Enabled bool `json:"enabled"`
}

type billingReportRebuildRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func billingReportFiltersFromContext(c *gin.Context) model.BillingReportFilters {
	return model.BillingReportFilters{
		StartDate:       c.Query("start_date"),
		EndDate:         c.Query("end_date"),
		Username:        c.Query("username"),
		UserGroup:       c.Query("user_group"),
		ThirdPartyGroup: c.Query("third_party_group"),
		ChannelTag:      c.Query("channel_tag"),
		ChannelName:     c.Query("channel_name"),
		UpstreamUrl:     c.Query("upstream_url"),
		ModelName:       c.Query("model_name"),
		TokenName:       c.Query("token_name"),
	}
}

func ensureBillingReportEnabled(c *gin.Context) bool {
	if service.BillingReportEnabled() {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"message": "billing report module is disabled",
	})
	return false
}

func GetBillingReport(c *gin.Context) {
	if !ensureBillingReportEnabled(c) {
		return
	}
	pageInfo := common.GetPageQuery(c)
	rows, total, totals, err := model.QueryBillingReport(
		billingReportFiltersFromContext(c),
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     rows,
			"total":     total,
			"page":      pageInfo.GetPage(),
			"page_size": pageInfo.GetPageSize(),
			"totals":    totals,
		},
	})
}

func GetBillingReportStatus(c *gin.Context) {
	status, err := service.GetBillingReportStatus()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}

func UpdateBillingReportAuto(c *gin.Context) {
	if !ensureBillingReportEnabled(c) {
		return
	}
	var request billingReportAutoRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.SetBillingReportAutoEnabled(request.Enabled); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"auto_enabled": request.Enabled})
}

func CreateBillingReportRebuild(c *gin.Context) {
	if !ensureBillingReportEnabled(c) {
		return
	}
	var request billingReportRebuildRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	job, err := service.CreateBillingReportJob(request.StartDate, request.EndDate)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "",
		"data":    job,
	})
}

func ExportBillingReport(c *gin.Context) {
	if !ensureBillingReportEnabled(c) {
		return
	}
	if !billingReportExportRunning.CompareAndSwap(false, true) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "another billing export is running",
		})
		return
	}
	defer billingReportExportRunning.Store(false)

	filters := billingReportFiltersFromContext(c)
	total, err := model.CountBillingReportForExport(filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if total > 1_048_000 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"success": false,
			"message": "too many rows for one Excel worksheet; narrow the date range",
		})
		return
	}
	file, err := createBillingReportWorkbook(filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()

	startDate := filters.StartDate
	endDate := filters.EndDate
	if startDate == "" {
		startDate = "all"
	}
	if endDate == "" {
		endDate = time.Now().Format("2006-01-02")
	}
	filename := fmt.Sprintf("billing_usage_%s_%s.xlsx", startDate, endDate)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Status(http.StatusOK)
	if err := file.Write(c.Writer); err != nil {
		common.SysError("billing report export failed: " + err.Error())
	}
}

func createBillingReportWorkbook(filters model.BillingReportFilters) (*excelize.File, error) {
	file := excelize.NewFile()
	const sheet = "使用明细"
	defaultSheet := file.GetSheetName(0)
	if err := file.SetSheetName(defaultSheet, sheet); err != nil {
		return nil, err
	}
	headers := []interface{}{
		"日期", "客户", "用户分组", "第三方分组", "渠道标签", "渠道名称", "上游地址", "模型", "Token名称",
		"输入Token", "输出Token", "缓存读Token", "缓存写Token", "调用次数",
		"倍率前输入价格", "倍率前输出价格", "倍率前缓存读价格", "倍率前缓存写价格", "倍率前其他费用", "倍率前总价",
		"用户/分组倍率",
		"折算后输入价格", "折算后输出价格", "折算后缓存读价格", "折算后缓存写价格", "折算后其他费用", "折算后实际总价",
		"倍率前输入单价/M", "倍率前输出单价/M", "倍率前缓存读单价/M", "倍率前缓存写单价/M",
		"折算后输入单价/M", "折算后输出单价/M", "折算后缓存读单价/M", "折算后缓存写单价/M",
	}
	headerStyle, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"374151"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	moneyFormat := "#,##0.00000000"
	moneyStyle, err := file.NewStyle(&excelize.Style{CustomNumFmt: &moneyFormat})
	if err != nil {
		return nil, err
	}
	stream, err := file.NewStreamWriter(sheet)
	if err != nil {
		return nil, err
	}
	headerCells := make([]interface{}, len(headers))
	for i := range headers {
		headerCells[i] = excelize.Cell{StyleID: headerStyle, Value: headers[i]}
	}
	if err := stream.SetRow("A1", headerCells, excelize.RowOpts{Height: 24}); err != nil {
		return nil, err
	}
	index := 0
	err = model.IterateBillingReportForExport(filters, func(row model.BillingReportDaily) error {
		ratio := interface{}("未知")
		if row.GroupRatioKnown {
			ratio = row.GroupRatio.InexactFloat64()
		}
		values := []interface{}{
			row.BillDate, row.Username, row.UserGroup, row.ThirdPartyGroup, row.ChannelTag, row.ChannelName, row.UpstreamUrl, row.ModelName, row.TokenName,
			row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.CallCount,
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalInput.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalOutput.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalCacheRead.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalCacheWrite.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalOther.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalTotal.InexactFloat64()},
			ratio,
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedInput.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedOutput.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedCacheRead.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedCacheWrite.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedOther.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedTotal.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalInputUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalOutputUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalCacheReadUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.OriginalCacheWriteUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedInputUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedOutputUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedCacheReadUnit.InexactFloat64()},
			excelize.Cell{StyleID: moneyStyle, Value: row.AdjustedCacheWriteUnit.InexactFloat64()},
		}
		cell, _ := excelize.CoordinatesToCellName(1, index+2)
		if err := stream.SetRow(cell, values); err != nil {
			return err
		}
		index++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := stream.Flush(); err != nil {
		return nil, err
	}
	if err := file.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	}); err != nil {
		return nil, err
	}
	lastColumn, _ := excelize.ColumnNumberToName(len(headers))
	if err := file.AutoFilter(sheet, "A1:"+lastColumn+"1", nil); err != nil {
		return nil, err
	}
	_ = file.SetColWidth(sheet, "A", "F", 16)
	_ = file.SetColWidth(sheet, "G", "G", 34)
	_ = file.SetColWidth(sheet, "H", lastColumn, 18)
	return file, nil
}
