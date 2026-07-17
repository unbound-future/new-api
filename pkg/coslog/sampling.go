package coslog

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const ctxKeySampled = "coslog_sampled"

var sampleSubscriberOnce sync.Once

func ShouldSampleRequestID(requestID string) bool {
	basisPoints := common.GetCosLogSampleBasisPoints()
	if basisPoints <= 0 {
		return false
	}
	if basisPoints >= 10000 {
		return true
	}
	if requestID == "" {
		return false
	}
	sum := sha256.Sum256([]byte(requestID))
	bucket := binary.BigEndian.Uint64(sum[:8]) % 10000
	return int64(bucket) < basisPoints
}

func DecideForContext(ctx *gin.Context) bool {
	if ctx == nil || !common.CosLogEnabled {
		return false
	}
	sampled := ShouldSampleRequestID(ctx.GetString(common.RequestIdKey))
	ctx.Set(ctxKeySampled, sampled)
	return sampled
}

func IsSampled(ctx *gin.Context) bool {
	if ctx == nil || !common.CosLogEnabled {
		return false
	}
	sampled, exists := ctx.Get(ctxKeySampled)
	if !exists {
		return false
	}
	value, ok := sampled.(bool)
	return ok && value
}

func startSampleConfigSubscriber() {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	sampleSubscriberOnce.Do(func() {
		go func() {
			pubsub := common.RDB.Subscribe(context.Background(), common.CosLogSampleRedisChannel)
			defer pubsub.Close()
			for message := range pubsub.Channel() {
				if err := common.SetCosLogSamplePercent(message.Payload); err != nil {
					common.SysLog("ignored invalid COSLOG sample percent from Redis: " + err.Error())
					continue
				}
				common.OptionMapRWMutex.Lock()
				if common.OptionMap != nil {
					common.OptionMap[common.CosLogSamplePercentOption] = common.FormatCosLogSamplePercent(common.GetCosLogSampleBasisPoints())
				}
				common.OptionMapRWMutex.Unlock()
			}
		}()
	})
}
