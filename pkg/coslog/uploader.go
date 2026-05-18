package coslog

import "context"

type Uploader interface {
	Upload(ctx context.Context, objectKey string, filePath string) error
}
