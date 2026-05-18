package coslog

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Enabled           bool
	StorageType       string // "cos" or "gcs"
	Bucket            string
	Region            string
	Prefix            string
	SecretID          string
	SecretKey         string
	ServiceAccountKey string // GCS service account JSON key
	FlushSize         int
	FlushInterval     time.Duration
	MaxFileSize       int64
	LocalDir          string
	DeleteAfterUpload bool
}

func LoadConfig() Config {
	cfg := Config{
		Enabled:           os.Getenv("COSLOG_ENABLED") == "true",
		StorageType:       os.Getenv("COSLOG_STORAGE_TYPE"),
		Bucket:            os.Getenv("OSS_BUCKET"),
		Region:            os.Getenv("OSS_REGION"),
		Prefix:            os.Getenv("OSS_PREFIX"),
		SecretID:          os.Getenv("OSS_SECRET_ID"),
		SecretKey:         os.Getenv("OSS_SECRET_KEY"),
		ServiceAccountKey: os.Getenv("OSS_SERVICE_ACCOUNT_KEY"),
		FlushSize:         10000,
		FlushInterval:     120 * time.Second,
		MaxFileSize:       100 * 1024 * 1024,
		LocalDir:          "./oss_log",
		DeleteAfterUpload: true,
	}
	if cfg.StorageType == "" {
		cfg.StorageType = "cos"
	}
	if v := os.Getenv("COSLOG_FLUSH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.FlushSize = n
		}
	}
	if v := os.Getenv("COSLOG_FLUSH_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.FlushInterval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("COSLOG_MAX_FILE_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.MaxFileSize = n
		}
	}
	if v := os.Getenv("COSLOG_LOCAL_DIR"); v != "" {
		cfg.LocalDir = v
	}
	if os.Getenv("COSLOG_DELETE_AFTER_UPLOAD") == "false" {
		cfg.DeleteAfterUpload = false
	}
	return cfg
}
