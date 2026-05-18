package coslog

import (
	"context"
	"fmt"
	"io"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

type GCSUploader struct {
	client *storage.Client
	bucket string
}

func NewGCSUploader(cfg Config) (*GCSUploader, error) {
	ctx := context.Background()
	var opts []option.ClientOption
	if cfg.ServiceAccountKey != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.ServiceAccountKey)))
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create gcs client: %w", err)
	}
	return &GCSUploader{client: client, bucket: cfg.Bucket}, nil
}

func (u *GCSUploader) Upload(ctx context.Context, objectKey string, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	writer := u.client.Bucket(u.bucket).Object(objectKey).NewWriter(ctx)
	if _, err := io.Copy(writer, f); err != nil {
		writer.Close()
		return fmt.Errorf("upload to gcs: %w", err)
	}
	return writer.Close()
}
