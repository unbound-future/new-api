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
		t.Fatalf("expected 2 files after rotation, got %d", len(entries))
	}
}
