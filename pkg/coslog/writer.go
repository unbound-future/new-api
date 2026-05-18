package coslog

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type JSONLWriter struct {
	cfg         Config
	file        *os.File
	currentFile string
	buffer      []COSLOG
	mu          sync.Mutex
	ch          chan COSLOG
	wg          sync.WaitGroup
	closed      bool
	uploader    Uploader
}

var defaultWriter *JSONLWriter

func NewJSONLWriter(cfg Config) (*JSONLWriter, error) {
	if err := os.MkdirAll(cfg.LocalDir, 0755); err != nil {
		return nil, fmt.Errorf("create local dir: %w", err)
	}
	w := &JSONLWriter{
		cfg:    cfg,
		buffer: make([]COSLOG, 0, cfg.FlushSize),
		ch:     make(chan COSLOG, 10000),
	}
	if cfg.Bucket != "" {
		switch cfg.StorageType {
		case "gcs":
			uploader, err := NewGCSUploader(cfg)
			if err != nil {
				return nil, fmt.Errorf("init gcs uploader: %w", err)
			}
			w.uploader = uploader
		default:
			if cfg.Region != "" && cfg.SecretID != "" && cfg.SecretKey != "" {
				uploader, err := NewCOSUploader(cfg)
				if err != nil {
					return nil, fmt.Errorf("init cos uploader: %w", err)
				}
				w.uploader = uploader
			}
		}
	}
	if err := w.newFile(); err != nil {
		return nil, err
	}
	w.wg.Add(1)
	go w.run()
	return w, nil
}

func (w *JSONLWriter) Write(entry COSLOG) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	w.ch <- entry
}

func (w *JSONLWriter) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()
	close(w.ch)
	w.wg.Wait()
	w.mu.Lock()
	w.flushBuffer("close")
	if w.file != nil {
		w.uploadAndRemove(w.currentFile)
		w.file.Close()
	}
	w.mu.Unlock()
}

func (w *JSONLWriter) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case entry, ok := <-w.ch:
			if !ok {
				return
			}
			w.mu.Lock()
			w.buffer = append(w.buffer, entry)
			if len(w.buffer) >= w.cfg.FlushSize {
				w.flushBuffer("buffer_full")
			}
			w.mu.Unlock()
		case <-ticker.C:
			w.mu.Lock()
			if len(w.buffer) > 0 {
				w.flushBuffer("ticker")
			}
			w.mu.Unlock()
		}
	}
}

func (w *JSONLWriter) flushBuffer(reason string) {
	if len(w.buffer) == 0 {
		return
	}
	for _, entry := range w.buffer {
		b, err := common.Marshal(entry)
		if err != nil {
			common.SysError("coslog marshal error: " + err.Error())
			continue
		}
		if w.file != nil {
			w.file.Write(append(b, '\n'))
		}
	}
	w.buffer = w.buffer[:0]

	if w.file != nil {
		info, err := w.file.Stat()
		if err == nil && info.Size() >= w.cfg.MaxFileSize {
			oldFile := w.currentFile
			w.file.Close()
			w.uploadAndRemove(oldFile)
			w.newFile()
		}
	}
}

func (w *JSONLWriter) uploadAndRemove(filePath string) {
	if w.uploader == nil {
		return
	}
	objectKey := filepath.Base(filePath)
	if w.cfg.Prefix != "" {
		objectKey = w.cfg.Prefix + "/" + objectKey
	}
	err := w.uploader.Upload(context.Background(), objectKey, filePath)
	if err != nil {
		common.SysError("coslog upload failed: " + err.Error())
		return
	}
	if w.cfg.DeleteAfterUpload {
		os.Remove(filePath)
	}
}

func (w *JSONLWriter) newFile() error {
	now := time.Now()
	ts := now.Format("20060102_150405")
	r := rand.New(rand.NewSource(now.UnixNano()))
	filename := filepath.Join(w.cfg.LocalDir, fmt.Sprintf("log_%s_%06d.jsonl", ts, r.Intn(1000000)))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.currentFile = filename
	return nil
}

func Init() {
	cfg := LoadConfig()
	if !cfg.Enabled {
		return
	}
	writer, err := NewJSONLWriter(cfg)
	if err != nil {
		common.SysError("coslog init failed: " + err.Error())
		return
	}
	defaultWriter = writer
	common.SysLog("coslog initialized, local dir: " + cfg.LocalDir)
}
