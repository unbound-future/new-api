# COS Audit Trail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Archive every user relay request/response as jsonl entries, rolling at 100MB per file and uploading to Tencent Cloud COS.

**Architecture:** Reuse existing settlement hook pattern (same layer as `pkg/prom_metrics`). A `ResponseCaptureMiddleware` caches response body/headers into `gin.Context`; the settlement hook assembles `COSLOG` and asynchronously flushes to local jsonl via `pkg/coslog.JSONLWriter`, which rotates on 100MB and uploads to COS.

**Tech Stack:** Go 1.22, `github.com/tencentyun/cos-go-sdk-v5`, `github.com/bytedance/gopkg/util/gopool`, existing `BodyStorage` for request body.

---

### Task 1: Add Tencent Cloud COS SDK dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add dependency**

Run:
```bash
cd /Users/admin/UnBound/code/new-api && go get github.com/tencentyun/cos-go-sdk-v5
```

- [ ] **Step 2: Verify go.sum updated**

Run:
```bash
git diff go.mod go.sum
```
Expected: `cos-go-sdk-v5` appears as a new direct require.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add tencent cloud cos sdk"
```

---

### Task 2: Create `pkg/coslog/config.go`

**Files:**
- Create: `pkg/coslog/config.go`

- [ ] **Step 1: Write config.go**

```go
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
```

- [ ] **Step 2: Commit**

```bash
git add pkg/coslog/config.go
git commit -m "feat(coslog): add config and env parsing"
```

---

### Task 3: Create `pkg/coslog/log_entry.go`

**Files:**
- Create: `pkg/coslog/log_entry.go`

- [ ] **Step 1: Write log_entry.go**

```go
package coslog

import (
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type COSLOG struct {
	UserName        string `json:"user_name"`
	ModelName       string `json:"model_name"`
	Url             string `json:"url"`
	RequestID       string `json:"request_id"`
	RequestBody     string `json:"request_body"`
	RequestHeaders  string `json:"request_headers"`
	ResponseHeaders string `json:"response_headers"`
	ResponseBody    string `json:"response_body"`
}

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"

func Record(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if defaultWriter == nil || ctx == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			common.SysError("coslog.Record panic: " + fmt.Sprint(r))
		}
	}()

	var reqBody string
	if bs, err := common.GetRequestBody(ctx); err == nil {
		if b, err := io.ReadAll(bs); err == nil {
			reqBody = string(b)
		}
	}

	var reqHeaders string
	if h, err := common.Marshal(headersToMap(ctx.Request.Header)); err == nil {
		reqHeaders = string(h)
	}

	var respBody, respHeaders string
	if v, exists := ctx.Get(ctxKeyResponseBody); exists {
		respBody, _ = v.(string)
	}
	if v, exists := ctx.Get(ctxKeyResponseHeaders); exists {
		respHeaders, _ = v.(string)
	}

	requestId := ctx.GetString(common.RequestIdKey)

	entry := COSLOG{
		UserName:        ctx.GetString("username"),
		ModelName:       relayInfo.OriginModelName,
		Url:             ctx.Request.URL.String(),
		RequestID:       requestId,
		RequestBody:     reqBody,
		RequestHeaders:  reqHeaders,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBody,
	}

	defaultWriter.Write(entry)
}

func headersToMap(h map[string][]string) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/coslog/log_entry.go
git commit -m "feat(coslog): add COSLOG struct and Record helper"
```

---

### Task 4: Create `pkg/coslog/writer.go`

**Files:**
- Create: `pkg/coslog/writer.go`

- [ ] **Step 1: Write writer.go**

```go
package coslog

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/tencentyun/cos-go-sdk-v5"
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
	cosClient   *cos.Client
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
	if cfg.Bucket != "" && cfg.Region != "" && cfg.SecretID != "" && cfg.SecretKey != "" {
		u, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Region))
		if err != nil {
			return nil, fmt.Errorf("invalid cos url: %w", err)
		}
		w.cosClient = cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
			Transport: &cos.AuthorizationTransport{
				SecretID:  cfg.SecretID,
				SecretKey: cfg.SecretKey,
			},
		})
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
	if w.cosClient == nil {
		return
	}
	objectKey := filepath.Base(filePath)
	if w.cfg.Prefix != "" {
		objectKey = w.cfg.Prefix + "/" + objectKey
	}
	_, err := w.cosClient.Object.PutFromFile(context.Background(), objectKey, filePath, nil)
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
```

- [ ] **Step 2: Commit**

```bash
git add pkg/coslog/writer.go
git commit -m "feat(coslog): add JSONLWriter with COS upload and rotation"
```

---

### Task 5: Create `pkg/coslog/writer_test.go`

**Files:**
- Create: `pkg/coslog/writer_test.go`

- [ ] **Step 1: Write writer_test.go**

```go
package coslog

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestJSONLWriter_WriteAndFlush(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		FlushSize:         2,
		FlushInterval:     10 * time.Second,
		MaxFileSize:       1024 * 1024,
		LocalDir:          dir,
		DeleteAfterUpload: false,
	}
	w, err := NewJSONLWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write(COSLOG{UserName: "alice", ModelName: "gpt-4"})
	w.Write(COSLOG{UserName: "bob", ModelName: "gpt-4"})
	time.Sleep(200 * time.Millisecond)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one jsonl file")
	}

	data, err := os.ReadFile(dir + "/" + entries[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"user_name":"alice"`) {
		t.Fatalf("unexpected first line: %s", lines[0])
	}
}

func TestJSONLWriter_FileRotation(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		FlushSize:         1,
		FlushInterval:     10 * time.Second,
		MaxFileSize:       10,
		LocalDir:          dir,
		DeleteAfterUpload: false,
	}
	w, err := NewJSONLWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Write(COSLOG{UserName: "alice", ModelName: "gpt-4"})
	time.Sleep(200 * time.Millisecond)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		// one file written, one file opened after rotation
		t.Fatalf("expected 2 files after rotation, got %d", len(entries))
	}
}
```

- [ ] **Step 2: Run tests**

Run:
```bash
cd /Users/admin/UnBound/code/new-api && go test ./pkg/coslog/... -v
```
Expected: 2 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add pkg/coslog/writer_test.go
git commit -m "test(coslog): add writer tests"
```

---

### Task 6: Create `middleware/response_capture.go`

**Files:**
- Create: `middleware/response_capture.go`

- [ ] **Step 1: Write response_capture.go**

```go
package middleware

import (
	"bytes"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const ctxKeyResponseBody = "coslog_response_body"
const ctxKeyResponseHeaders = "coslog_response_headers"

type captureWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	headers http.Header
}

func (w *captureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteHeader(code int) {
	for k, v := range w.ResponseWriter.Header() {
		w.headers[k] = v
	}
	w.ResponseWriter.WriteHeader(code)
}

func ResponseCaptureMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cw := &captureWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
			headers:        make(http.Header),
		}
		c.Writer = cw
		c.Next()

		hBytes, _ := common.Marshal(headersToMap(cw.headers))
		c.Set(ctxKeyResponseBody, cw.body.String())
		c.Set(ctxKeyResponseHeaders, string(hBytes))
	}
}

func headersToMap(h http.Header) map[string]string {
	m := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}
```

- [ ] **Step 2: Commit**

```bash
git add middleware/response_capture.go
git commit -m "feat(middleware): add ResponseCaptureMiddleware for coslog"
```

---

### Task 7: Create `middleware/response_capture_test.go`

**Files:**
- Create: `middleware/response_capture_test.go`

- [ ] **Step 1: Write response_capture_test.go**

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponseCaptureMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/v1/chat/completions", nil)

	mw := ResponseCaptureMiddleware()
	handler := gin.HandlerFunc(func(c *gin.Context) {
		c.Writer.Header().Set("X-Test", "value")
		c.Writer.Write([]byte(`{"ok":true}`))
	})

	mw(c)
	handler(c)

	body, exists := c.Get(ctxKeyResponseBody)
	if !exists {
		t.Fatal("expected response body in context")
	}
	if body != `{"ok":true}` {
		t.Fatalf("unexpected body: %v", body)
	}

	headers, exists := c.Get(ctxKeyResponseHeaders)
	if !exists {
		t.Fatal("expected response headers in context")
	}
	if !strings.Contains(headers.(string), "X-Test") {
		t.Fatalf("expected headers to contain X-Test: %v", headers)
	}
}
```

Note: add `"strings"` to imports.

- [ ] **Step 2: Run tests**

Run:
```bash
cd /Users/admin/UnBound/code/new-api && go test ./middleware/... -run TestResponseCaptureMiddleware -v
```
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add middleware/response_capture_test.go
git commit -m "test(middleware): add response capture middleware test"
```

---

### Task 8: Mount middleware in relay router

**Files:**
- Modify: `router/relay-router.go:14-19`

- [ ] **Step 1: Add import and middleware**

Add import:
```go
	coslog "github.com/QuantumNous/new-api/pkg/coslog"
```

Wait — `pkg/coslog` is not used directly in router. Only `middleware.ResponseCaptureMiddleware()` is needed, which is already in the `middleware` package.

Modify `router/relay-router.go:19`:

```go
	router.Use(prom_metrics.GinMiddleware())
	router.Use(middleware.ResponseCaptureMiddleware()) // 新增
```

- [ ] **Step 2: Commit**

```bash
git add router/relay-router.go
git commit -m "feat(router): mount ResponseCaptureMiddleware on relay routes"
```

---

### Task 9: Wire coslog into settlement hooks

**Files:**
- Modify: `service/quota.go`, `service/text_quota.go`

This task updates the three main settlement functions to call `coslog.Record` alongside `prom_metrics.RecordRelaySettled`.

- [ ] **Step 1: Add import in service/quota.go**

Add to imports:
```go
	coslog "github.com/QuantumNous/new-api/pkg/coslog"
```

- [ ] **Step 2: Update `PostWssConsumeQuota` (service/quota.go ~line 259)**

Find:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			Quota:            quota,
		})
	})
```

Replace with:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			Quota:            quota,
		})
		coslog.Record(ctx, relayInfo)
	})
```

- [ ] **Step 3: Update `PostAudioConsumeQuota` (service/quota.go ~line 390)**

Find:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			Quota:            quota,
		})
	})
```

Replace with:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			Quota:            quota,
		})
		coslog.Record(ctx, relayInfo)
	})
```

- [ ] **Step 4: Add import in service/text_quota.go**

Add to imports:
```go
	coslog "github.com/QuantumNous/new-api/pkg/coslog"
```

- [ ] **Step 5: Update `PostTextConsumeQuota` (service/text_quota.go ~line 481)**

Find:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:        summary.PromptTokens,
			CompletionTokens:    summary.CompletionTokens,
			CacheReadTokens:     summary.CacheTokens,
			CacheCreationTokens: summary.CacheCreationTokens,
			Quota:               summary.Quota,
		})
	})
```

Replace with:
```go
	gopool.Go(func() {
		prom_metrics.RecordRelaySettled(relayInfo, prom_metrics.SettledSample{
			PromptTokens:        summary.PromptTokens,
			CompletionTokens:    summary.CompletionTokens,
			CacheReadTokens:     summary.CacheTokens,
			CacheCreationTokens: summary.CacheCreationTokens,
			Quota:               summary.Quota,
		})
		coslog.Record(ctx, relayInfo)
	})
```

- [ ] **Step 6: Commit**

```bash
git add service/quota.go service/text_quota.go
git commit -m "feat(coslog): wire Record into settlement hooks"
```

---

### Task 10: Initialize coslog in main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Add import**

Add to imports:
```go
	coslog "github.com/QuantumNous/new-api/pkg/coslog"
```

- [ ] **Step 2: Add Init call**

Find in `InitResources()`:
```go
	perfmetrics.Init()
	prom_metrics.Init()
```

Replace with:
```go
	perfmetrics.Init()
	prom_metrics.Init()
	coslog.Init()
```

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(main): initialize coslog during resource init"
```

---

### Task 11: Build and verify

- [ ] **Step 1: Build the project**

Run:
```bash
cd /Users/admin/UnBound/code/new-api && go build -o /tmp/new-api .
```
Expected: compilation succeeds with no errors.

- [ ] **Step 2: Run all affected tests**

Run:
```bash
cd /Users/admin/UnBound/code/new-api && go test ./pkg/coslog/... ./middleware/... -v
```
Expected: all PASS.

- [ ] **Step 3: Commit (if any fixes needed)**

If compilation or tests required fixes, commit them.

---

## Spec Coverage Check

| Spec Requirement | Task |
|---|---|
| COSLOG struct with 8 fields | Task 3 |
| JSONLWriter async batch write | Task 4 |
| 100MB file rotation + COS upload | Task 4 |
| ResponseCaptureMiddleware | Task 6 |
| Settlement hook integration | Task 9 |
| main.go Init() | Task 10 |
| Environment variable config | Task 2 |
| Tests | Task 5, Task 7 |

No gaps.

## Placeholder Scan

No TBD, TODO, or vague requirements found.

## Type Consistency

- `COSLOG` struct fields match `Record()` usage and writer `Write()` signature throughout.
- `ctxKeyResponseBody` / `ctxKeyResponseHeaders` constants defined in `middleware/response_capture.go` match `pkg/coslog/log_entry.go` reads.
