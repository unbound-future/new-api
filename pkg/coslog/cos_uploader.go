package coslog

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type COSUploader struct {
	client *cos.Client
}

func NewCOSUploader(cfg Config) (*COSUploader, error) {
	u, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.Bucket, cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("invalid cos url: %w", err)
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
		},
	})
	return &COSUploader{client: client}, nil
}

func (u *COSUploader) Upload(ctx context.Context, objectKey string, filePath string) error {
	_, err := u.client.Object.PutFromFile(ctx, objectKey, filePath, nil)
	return err
}
