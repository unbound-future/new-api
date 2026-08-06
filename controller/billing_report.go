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
	"github.com/shopspring/decimal"
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
		"日期", "客户", "用户分组", "使用分组", "渠道标签", "渠道名称", "上游地址", "模型", "令牌名称",
		"计费方式", "命中阶梯", "输入Token", "输出Token", "缓存读Token", "缓存写Token", "调用次数",
		"倍率前总价",
		"用户/分组倍率",
		"折算后实际总价",
		"输入单价/M", "输出单价/M", "缓存读单价/M", "缓存写单价/M",
	}
	newHeaderStyle := func(color string) (int, error) {
		return file.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		})
	}
	normalHeaderStyle, err := newHeaderStyle("374151")
	if err != nil {
		return nil, err
	}
	originalHeaderStyle, err := newHeaderStyle("1D4ED8")
	if err != nil {
		return nil, err
	}
	ratioHeaderStyle, err := newHeaderStyle("92400E")
	if err != nil {
		return nil, err
	}
	adjustedHeaderStyle, err := newHeaderStyle("047857")
	if err != nil {
		return nil, err
	}
	unitHeaderStyle, err := newHeaderStyle("6D28D9")
	if err != nil {
		return nil, err
	}
	wholeNumberFormat := "#,##0"
	wholeNumberStyle, err := file.NewStyle(&excelize.Style{CustomNumFmt: &wholeNumberFormat})
	if err != nil {
		return nil, err
	}
	decimalNumberFormat := "#,##0.########"
	decimalNumberStyle, err := file.NewStyle(&excelize.Style{CustomNumFmt: &decimalNumberFormat})
	if err != nil {
		return nil, err
	}
	numberCell := func(value decimal.Decimal) excelize.Cell {
		style := decimalNumberStyle
		if value.Equal(value.Truncate(0)) {
			style = wholeNumberStyle
		}
		return excelize.Cell{StyleID: style, Value: value.InexactFloat64()}
	}
	stream, err := file.NewStreamWriter(sheet)
	if err != nil {
		return nil, err
	}
	if err := stream.SetPanes(&excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection: []excelize.Selection{
			{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"},
		},
	}); err != nil {
		return nil, err
	}
	for _, width := range []struct {
		start int
		end   int
		value float64
	}{
		{1, 1, 13},
		{2, 6, 16},
		{7, 7, 34},
		{8, 11, 18},
		{12, 15, 15},
		{16, 16, 12},
		{17, len(headers), 19},
	} {
		if err := stream.SetColWidth(width.start, width.end, width.value); err != nil {
			return nil, err
		}
	}
	headerCells := make([]interface{}, len(headers))
	for i := range headers {
		style := normalHeaderStyle
		switch {
		case i == 16:
			style = originalHeaderStyle
		case i == 17:
			style = ratioHeaderStyle
		case i == 18:
			style = adjustedHeaderStyle
		case i >= 19:
			style = unitHeaderStyle
		}
		headerCells[i] = excelize.Cell{StyleID: style, Value: headers[i]}
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
		unitPrice := func(value decimal.Decimal) interface{} {
			if !row.PricingBreakdownKnown {
				return ""
			}
			return numberCell(value)
		}
		values := []interface{}{
			row.BillDate, row.Username, row.UserGroup, row.ThirdPartyGroup, row.ChannelTag, row.ChannelName, row.UpstreamUrl, row.ModelName, row.TokenName,
			row.BillingMode, row.MatchedTier,
			row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.CallCount,
			numberCell(row.OriginalTotal),
			ratio,
			numberCell(row.AdjustedTotal),
			unitPrice(row.OriginalInputUnit),
			unitPrice(row.OriginalOutputUnit),
			unitPrice(row.OriginalCacheReadUnit),
			unitPrice(row.OriginalCacheWriteUnit),
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
	lastColumn, _ := excelize.ColumnNumberToName(len(headers))
	lastRow := index + 1
	if lastRow < 2 {
		lastRow = 2
	}
	showRowStripes := false
	if err := stream.AddTable(&excelize.Table{
		Range:          fmt.Sprintf("A1:%s%d", lastColumn, lastRow),
		Name:           "BillingUsage",
		ShowRowStripes: &showRowStripes,
	}); err != nil {
		return nil, err
	}
	if err := stream.Flush(); err != nil {
		return nil, err
	}
	return file, nil
}
