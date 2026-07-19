package coslog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingUploader struct {
	objectKey string
	contents  []byte
	err       error
}

func (u *recordingUploader) Upload(_ context.Context, objectKey string, filePath string) error {
	u.objectKey = objectKey
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	u.contents = contents
	return u.err
}

func TestPubSubUploadPreservesPayloadAndPrefix(t *testing.T) {
	dir := t.TempDir()
	uploader := &recordingUploader{}
	transport := &PubSubTransport{
		cfg: Config{
			LocalDir:          dir,
			Prefix:            "existing/path",
			DeleteAfterUpload: true,
		},
		uploader: uploader,
	}
	records := [][]byte{
		[]byte(`{"request_id":"req_A1","request_body":"完整请求"}`),
		[]byte(`{"request_id":"req_B2","response_body":"完整响应"}`),
	}

	if !transport.uploadRecords(records, true) {
		t.Fatal("uploadRecords returned false")
	}
	want := string(records[0]) + "\n" + string(records[1]) + "\n"
	if string(uploader.contents) != want {
		t.Fatalf("uploaded payload changed:\n got %q\nwant %q", uploader.contents, want)
	}
	if !strings.HasPrefix(uploader.objectKey, "existing/path/log_") || !strings.HasSuffix(uploader.objectKey, ".jsonl") {
		t.Fatalf("unexpected object key: %s", uploader.objectKey)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("successful upload left %d local files", len(entries))
	}
}

func TestPubSubFailedBatchUploadIsNackedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	transport := &PubSubTransport{
		cfg:      Config{LocalDir: dir, DeleteAfterUpload: true},
		uploader: &recordingUploader{err: errors.New("temporary gcs failure")},
	}

	if transport.uploadRecords([][]byte{[]byte(`{"request_id":"req_retry"}`)}, true) {
		t.Fatal("failed upload returned true")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("nacked batch left %d local files", len(entries))
	}
}

func TestOversizedDirectUploadFailureKeepsCompleteLocalFile(t *testing.T) {
	dir := t.TempDir()
	uploader := &recordingUploader{err: errors.New("temporary gcs failure")}
	transport := &PubSubTransport{
		cfg:      Config{LocalDir: dir, DeleteAfterUpload: true},
		uploader: uploader,
	}
	payload := []byte(`{"request_id":"req_large","request_body":"not truncated"}`)

	if transport.uploadRecords([][]byte{payload}, false) {
		t.Fatal("failed oversized upload returned true")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one retained file, got %d", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload)+"\n" {
		t.Fatalf("retained payload changed: %q", data)
	}
}

func TestLoadConfigPubSubDefaults(t *testing.T) {
	t.Setenv("COSLOG_TRANSPORT", "pubsub")
	t.Setenv("COSLOG_PUBSUB_PROJECT_ID", "project")
	t.Setenv("COSLOG_PUBSUB_TOPIC", "topic")
	t.Setenv("COSLOG_PUBSUB_SUBSCRIPTION", "subscription")
	t.Setenv("COSLOG_PUBSUB_MAX_MESSAGE_BYTES", "")
	t.Setenv("COSLOG_PUBSUB_PUBLISH_WORKERS", "")

	cfg := LoadConfig()
	if cfg.Transport != "pubsub" || cfg.PubSubProjectID != "project" || cfg.PubSubTopicID != "topic" || cfg.PubSubSubID != "subscription" {
		t.Fatalf("unexpected pubsub config: %+v", cfg)
	}
	if cfg.PubSubMaxBytes != 9_000_000 || cfg.PubSubWorkers != 32 {
		t.Fatalf("unexpected pubsub defaults: max=%d workers=%d", cfg.PubSubMaxBytes, cfg.PubSubWorkers)
	}
	if cfg.FlushSize != 10000 || cfg.FlushInterval != 120*time.Second || cfg.MaxFileSize != 100*1024*1024 {
		t.Fatalf("existing flush defaults changed: %+v", cfg)
	}
}
