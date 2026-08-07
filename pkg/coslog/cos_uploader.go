package coslog

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type COSUploader struct {
	objects cosMultipartUploader
}

type cosMultipartUploader interface {
	MultiUpload(ctx context.Context, name string, filePath string, opt *cos.MultiUploadOptions) (*cos.CompleteMultipartUploadResult, *cos.Response, error)
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
	return &COSUploader{objects: client.Object}, nil
}

func (u *COSUploader) Upload(ctx context.Context, objectKey string, filePath string) error {
	_, _, err := u.objects.MultiUpload(ctx, objectKey, filePath, &cos.MultiUploadOptions{
		ThreadPoolSize: 4,
	})
	return err
}
