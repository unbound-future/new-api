package coslog

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Enabled           bool
	Bucket            string
	Region            string
	Prefix            string
	SecretID          string
	SecretKey         string
	FlushSize         int
	FlushInterval     time.Duration
	MaxFileSize       int64
	LocalDir          string
	DeleteAfterUpload bool
}

func LoadConfig() Config {
	cfg := Config{
		Enabled:           os.Getenv("COSLOG_ENABLED") == "true",
		Bucket:            os.Getenv("COS_BUCKET"),
		Region:            os.Getenv("COS_REGION"),
		Prefix:            os.Getenv("COS_PREFIX"),
		SecretID:          os.Getenv("COS_SECRET_ID"),
		SecretKey:         os.Getenv("COS_SECRET_KEY"),
		FlushSize:         10000,
		FlushInterval:     120 * time.Second,
		MaxFileSize:       100 * 1024 * 1024,
		LocalDir:          "./oss_log",
		DeleteAfterUpload: true,
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
