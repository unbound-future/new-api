package common

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
)

const (
	CosLogSamplePercentOption = "CosLogSamplePercent"
	CosLogSampleRedisChannel  = "new-api:coslog:sample-percent"
	cosLogSampleBasis         = int64(10000)
)

var cosLogSampleBasisPoints atomic.Int64

func init() {
	// Keep the pre-sampling behavior when this option does not exist yet.
	cosLogSampleBasisPoints.Store(cosLogSampleBasis)
}

func ParseCosLogSamplePercent(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("COSLOG sample percent is required")
	}
	percent, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0, fmt.Errorf("invalid COSLOG sample percent")
	}
	if percent < 0 || percent > 100 {
		return 0, fmt.Errorf("COSLOG sample percent must be between 0 and 100")
	}
	basisPointsValue := percent * 100
	basisPoints := int64(math.Round(basisPointsValue))
	if math.Abs(basisPointsValue-float64(basisPoints)) > 1e-7 {
		return 0, fmt.Errorf("COSLOG sample percent supports at most two decimal places")
	}
	return basisPoints, nil
}

func SetCosLogSamplePercent(value string) error {
	basisPoints, err := ParseCosLogSamplePercent(value)
	if err != nil {
		return err
	}
	cosLogSampleBasisPoints.Store(basisPoints)
	return nil
}

func SetCosLogSampleBasisPoints(value int64) {
	if value < 0 {
		value = 0
	}
	if value > cosLogSampleBasis {
		value = cosLogSampleBasis
	}
	cosLogSampleBasisPoints.Store(value)
}

func GetCosLogSampleBasisPoints() int64 {
	return cosLogSampleBasisPoints.Load()
}

func GetCosLogSamplePercent() float64 {
	return float64(GetCosLogSampleBasisPoints()) / 100
}

func FormatCosLogSamplePercent(basisPoints int64) string {
	return strconv.FormatFloat(float64(basisPoints)/100, 'f', -1, 64)
}
