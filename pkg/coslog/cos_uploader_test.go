package coslog

import (
	"context"
	"errors"
	"testing"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type fakeCOSMultipartUploader struct {
	name     string
	filePath string
	opt      *cos.MultiUploadOptions
	err      error
}

func (f *fakeCOSMultipartUploader) MultiUpload(_ context.Context, name string, filePath string, opt *cos.MultiUploadOptions) (*cos.CompleteMultipartUploadResult, *cos.Response, error) {
	f.name = name
	f.filePath = filePath
	f.opt = opt
	return nil, nil, f.err
}

func TestCOSUploaderUsesMultipartUpload(t *testing.T) {
	fake := &fakeCOSMultipartUploader{}
	uploader := &COSUploader{objects: fake}

	err := uploader.Upload(context.Background(), "us/log.jsonl", "/tmp/log.jsonl")
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if fake.name != "us/log.jsonl" {
		t.Fatalf("object name = %q, want %q", fake.name, "us/log.jsonl")
	}
	if fake.filePath != "/tmp/log.jsonl" {
		t.Fatalf("file path = %q, want %q", fake.filePath, "/tmp/log.jsonl")
	}
	if fake.opt == nil {
		t.Fatal("multipart options are nil")
	}
	if fake.opt.ThreadPoolSize != 4 {
		t.Fatalf("thread pool size = %d, want 4", fake.opt.ThreadPoolSize)
	}
	if fake.opt.PartSize != 0 {
		t.Fatalf("part size = %d, want SDK automatic sizing", fake.opt.PartSize)
	}
}

func TestCOSUploaderReturnsMultipartError(t *testing.T) {
	wantErr := errors.New("multipart upload failed")
	fake := &fakeCOSMultipartUploader{err: wantErr}
	uploader := &COSUploader{objects: fake}

	err := uploader.Upload(context.Background(), "us/log.jsonl", "/tmp/log.jsonl")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Upload() error = %v, want %v", err, wantErr)
	}
}
