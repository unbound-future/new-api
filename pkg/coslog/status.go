package coslog

import (
	"io/fs"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type Status struct {
	Enabled              bool    `json:"enabled"`
	Initialized          bool    `json:"initialized"`
	UploaderReady        bool    `json:"uploader_ready"`
	StorageType          string  `json:"storage_type"`
	SamplePercent        float64 `json:"sample_percent"`
	QueueDepth           int     `json:"queue_depth"`
	QueueCapacity        int     `json:"queue_capacity"`
	BufferedEntries      int     `json:"buffered_entries"`
	LocalDir             string  `json:"local_dir"`
	LocalBytes           int64   `json:"local_bytes"`
	LastSuccessfulUpload int64   `json:"last_successful_upload"`
	DroppedTotal         uint64  `json:"dropped_total"`
	FlushSize            int     `json:"flush_size"`
	FlushIntervalSeconds int64   `json:"flush_interval_seconds"`
	MaxFileSize          int64   `json:"max_file_size"`
}

var droppedTotal atomic.Uint64
var lastSuccessfulUpload atomic.Int64

func recordDropped() {
	droppedTotal.Add(1)
}

func ResetDroppedTotal() uint64 {
	return droppedTotal.Swap(0)
}

func recordUploadSuccess() {
	lastSuccessfulUpload.Store(time.Now().Unix())
}

func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func GetStatus() Status {
	cfg := LoadConfig()
	status := Status{
		Enabled:              common.CosLogEnabled,
		StorageType:          cfg.StorageType,
		SamplePercent:        common.GetCosLogSamplePercent(),
		LocalDir:             cfg.LocalDir,
		LocalBytes:           directorySize(cfg.LocalDir),
		LastSuccessfulUpload: lastSuccessfulUpload.Load(),
		DroppedTotal:         droppedTotal.Load(),
		FlushSize:            cfg.FlushSize,
		FlushIntervalSeconds: int64(cfg.FlushInterval / time.Second),
		MaxFileSize:          cfg.MaxFileSize,
	}
	if defaultWriter == nil {
		return status
	}

	defaultWriter.enqueueMu.RLock()
	status.Initialized = !defaultWriter.closed
	defaultWriter.enqueueMu.RUnlock()
	status.UploaderReady = defaultWriter.uploader != nil
	status.QueueDepth = len(defaultWriter.ch)
	status.QueueCapacity = cap(defaultWriter.ch)
	if defaultWriter.mu.TryLock() {
		status.BufferedEntries = len(defaultWriter.buffer)
		defaultWriter.mu.Unlock()
	}
	return status
}
