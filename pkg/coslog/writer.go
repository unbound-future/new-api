package coslog

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type JSONLWriter struct {
	cfg         Config
	file        *os.File
	currentFile string
	currentSize int64
	buffer      []COSLOG
	mu          sync.Mutex
	enqueueMu   sync.RWMutex
	ch          chan COSLOG
	wg          sync.WaitGroup
	uploadCh    chan string
	uploadWG    sync.WaitGroup
	uploadMu    sync.Mutex
	uploading   map[string]struct{}
	diskFull    atomic.Bool
	closed      bool
	uploader    Uploader
}

var defaultWriter *JSONLWriter

func NewJSONLWriter(cfg Config) (*JSONLWriter, error) {
	if err := os.MkdirAll(cfg.LocalDir, 0755); err != nil {
		return nil, fmt.Errorf("create local dir: %w", err)
	}
	w := &JSONLWriter{
		cfg:       cfg,
		buffer:    make([]COSLOG, 0, cfg.FlushSize),
		ch:        make(chan COSLOG, 10000),
		uploading: make(map[string]struct{}),
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
	if w.uploader != nil {
		w.uploadCh = make(chan string, 128)
		for i := 0; i < 2; i++ {
			w.uploadWG.Add(1)
			go w.uploadWorker()
		}
		w.retryPendingUploads()
	}
	w.wg.Add(1)
	go w.run()
	return w, nil
}

func (w *JSONLWriter) Write(entry COSLOG) {
	if w.diskFull.Load() {
		recordDropped()
		return
	}
	w.enqueueMu.RLock()
	defer w.enqueueMu.RUnlock()
	if w.closed {
		recordDropped()
		return
	}
	select {
	case w.ch <- entry:
	default:
		recordDropped()
	}
}

func (w *JSONLWriter) Close() {
	w.enqueueMu.Lock()
	if w.closed {
		w.enqueueMu.Unlock()
		return
	}
	w.closed = true
	close(w.ch)
	w.enqueueMu.Unlock()
	w.wg.Wait()
	w.mu.Lock()
	w.flushBuffer("close")
	if w.file != nil {
		filePath := w.currentFile
		fileSize := w.currentSize
		_ = w.file.Close()
		w.file = nil
		if fileSize > 0 {
			w.enqueueUpload(filePath)
		} else {
			_ = os.Remove(filePath)
		}
	}
	w.mu.Unlock()
	if w.uploadCh != nil {
		close(w.uploadCh)
		w.uploadWG.Wait()
	}
}

func (w *JSONLWriter) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()
	retryTicker := time.NewTicker(60 * time.Second)
	defer retryTicker.Stop()
	diskTicker := time.NewTicker(5 * time.Second)
	defer diskTicker.Stop()
	w.refreshDiskLimit()
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
		case <-retryTicker.C:
			w.retryPendingUploads()
		case <-diskTicker.C:
			w.refreshDiskLimit()
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
			recordDropped()
			continue
		}
		if w.file != nil {
			line := append(b, '\n')
			if w.currentSize > 0 && w.currentSize+int64(len(line)) > w.cfg.MaxFileSize {
				if err := w.rotateFile(); err != nil {
					common.SysError("coslog rotate error: " + err.Error())
					recordDropped()
					continue
				}
			}
			n, writeErr := w.file.Write(line)
			if writeErr != nil || n != len(line) {
				recordDropped()
				if writeErr != nil {
					common.SysError("coslog write error: " + writeErr.Error())
				} else {
					common.SysError("coslog short write")
				}
			} else {
				w.currentSize += int64(n)
				if w.currentSize >= w.cfg.MaxFileSize {
					if err := w.rotateFile(); err != nil {
						common.SysError("coslog rotate error: " + err.Error())
					}
				}
			}
		} else {
			recordDropped()
		}
	}
	w.buffer = w.buffer[:0]

}

func (w *JSONLWriter) rotateFile() error {
	if w.file == nil {
		return w.newFile()
	}
	oldFile := w.currentFile
	oldSize := w.currentSize
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	w.currentFile = ""
	w.currentSize = 0
	if oldSize > 0 {
		w.enqueueUpload(oldFile)
	}
	return w.newFile()
}

func (w *JSONLWriter) enqueueUpload(filePath string) {
	if w.uploader == nil || w.uploadCh == nil || filePath == "" {
		return
	}
	w.uploadMu.Lock()
	if _, exists := w.uploading[filePath]; exists {
		w.uploadMu.Unlock()
		return
	}
	w.uploading[filePath] = struct{}{}
	w.uploadMu.Unlock()
	select {
	case w.uploadCh <- filePath:
	default:
		w.uploadMu.Lock()
		delete(w.uploading, filePath)
		w.uploadMu.Unlock()
	}
}

func (w *JSONLWriter) uploadWorker() {
	defer w.uploadWG.Done()
	for filePath := range w.uploadCh {
		w.uploadAndRemove(filePath)
		w.uploadMu.Lock()
		delete(w.uploading, filePath)
		w.uploadMu.Unlock()
	}
}

func (w *JSONLWriter) uploadAndRemove(filePath string) {
	objectKey := filepath.Base(filePath)
	if w.cfg.Prefix != "" {
		objectKey = w.cfg.Prefix + "/" + objectKey
	}
	err := w.uploader.Upload(context.Background(), objectKey, filePath)
	if err != nil {
		common.SysError("coslog upload failed: " + err.Error())
		return
	}
	recordUploadSuccess()
	if w.cfg.DeleteAfterUpload {
		os.Remove(filePath)
	}
}

func (w *JSONLWriter) retryPendingUploads() {
	if w.uploader == nil {
		return
	}
	files, err := filepath.Glob(filepath.Join(w.cfg.LocalDir, "*.jsonl"))
	if err != nil {
		return
	}
	for _, filePath := range files {
		if filePath == w.currentFile {
			continue
		}
		w.enqueueUpload(filePath)
	}
}

func (w *JSONLWriter) refreshDiskLimit() {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(w.cfg.LocalDir, &stat); err != nil || stat.Blocks == 0 {
		return
	}
	used := stat.Blocks - stat.Bavail
	w.diskFull.Store(used*100 >= stat.Blocks*85)
}

func (w *JSONLWriter) newFile() error {
	now := time.Now()
	ts := now.Format("20060102_150405")
	hostname, _ := os.Hostname()
	hostname = strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(hostname)
	if hostname == "" {
		hostname = "node"
	}
	r := rand.New(rand.NewSource(now.UnixNano()))
	filename := filepath.Join(w.cfg.LocalDir, fmt.Sprintf("log_%s_%s_%06d.jsonl", hostname, ts, r.Intn(1000000)))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.currentFile = filename
	if info, statErr := f.Stat(); statErr == nil {
		w.currentSize = info.Size()
	} else {
		w.currentSize = 0
	}
	return nil
}

func Init() {
	cfg := LoadConfig()
	if !cfg.Enabled {
		return
	}
	startSampleConfigSubscriber()
	writer, err := NewJSONLWriter(cfg)
	if err != nil {
		common.SysError("coslog init failed: " + err.Error())
		return
	}
	defaultWriter = writer
	common.SysLog("coslog initialized, local dir: " + cfg.LocalDir)
}

func Close() {
	if defaultWriter == nil {
		return
	}
	defaultWriter.Close()
	defaultWriter = nil
}
