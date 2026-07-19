package coslog

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/QuantumNous/new-api/common"
)

const pubSubQueueCapacity = 10000

type pubSubDelivery struct {
	data []byte
	done chan bool
}

// PubSubTransport decouples COSLOG production from GCS aggregation. Messages
// are acknowledged only after the JSONL object has been uploaded successfully.
type PubSubTransport struct {
	cfg          Config
	ctx          context.Context
	cancel       context.CancelFunc
	client       *pubsub.Client
	topic        *pubsub.Topic
	subscription *pubsub.Subscription
	uploader     Uploader

	publishCh chan COSLOG
	receiveCh chan *pubSubDelivery
	wg        sync.WaitGroup
	enqueueMu sync.RWMutex
	closed    bool

	bufferedEntries atomic.Int64
}

func NewPubSubTransport(cfg Config) (*PubSubTransport, error) {
	if cfg.StorageType != "gcs" {
		return nil, fmt.Errorf("pubsub transport requires COSLOG_STORAGE_TYPE=gcs")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("pubsub transport requires OSS_BUCKET")
	}
	if cfg.PubSubProjectID == "" || cfg.PubSubTopicID == "" || cfg.PubSubSubID == "" {
		return nil, fmt.Errorf("pubsub project, topic, and subscription are required")
	}
	if cfg.FlushSize <= 0 || cfg.FlushInterval <= 0 || cfg.MaxFileSize <= 0 {
		return nil, fmt.Errorf("invalid COSLOG flush configuration")
	}
	if err := os.MkdirAll(cfg.LocalDir, 0755); err != nil {
		return nil, fmt.Errorf("create local dir: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := pubsub.NewClient(ctx, cfg.PubSubProjectID)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create pubsub client: %w", err)
	}
	uploader, err := NewGCSUploader(cfg)
	if err != nil {
		cancel()
		_ = client.Close()
		return nil, fmt.Errorf("init gcs uploader: %w", err)
	}

	t := &PubSubTransport{
		cfg:          cfg,
		ctx:          ctx,
		cancel:       cancel,
		client:       client,
		topic:        client.Topic(cfg.PubSubTopicID),
		subscription: client.Subscription(cfg.PubSubSubID),
		uploader:     uploader,
		publishCh:    make(chan COSLOG, pubSubQueueCapacity),
		receiveCh:    make(chan *pubSubDelivery, pubSubQueueCapacity),
	}
	t.subscription.ReceiveSettings.MaxOutstandingMessages = 20000
	t.subscription.ReceiveSettings.MaxOutstandingBytes = 512 * 1024 * 1024
	t.subscription.ReceiveSettings.NumGoroutines = 4
	t.subscription.ReceiveSettings.MaxExtension = 30 * time.Minute

	t.wg.Add(cfg.PubSubWorkers + 2)
	for i := 0; i < cfg.PubSubWorkers; i++ {
		go t.publishLoop()
	}
	go t.batchLoop()
	go t.receiveLoop()
	return t, nil
}

func (t *PubSubTransport) Write(entry COSLOG) {
	t.enqueueMu.RLock()
	defer t.enqueueMu.RUnlock()
	if t.closed {
		recordDropped()
		return
	}
	select {
	case t.publishCh <- entry:
	default:
		recordDropped()
	}
}

func (t *PubSubTransport) Close() {
	t.enqueueMu.Lock()
	if t.closed {
		t.enqueueMu.Unlock()
		return
	}
	t.closed = true
	close(t.publishCh)
	t.enqueueMu.Unlock()
	t.cancel()
	t.topic.Stop()
	t.wg.Wait()
	_ = t.client.Close()
}

func (t *PubSubTransport) publishLoop() {
	defer t.wg.Done()
	for entry := range t.publishCh {
		data, err := common.Marshal(entry)
		if err != nil {
			common.SysError("coslog pubsub marshal error: " + err.Error())
			recordDropped()
			continue
		}
		if len(data) > t.cfg.PubSubMaxBytes {
			if !t.uploadRecords([][]byte{data}, false) {
				recordDropped()
			}
			continue
		}

		result := t.topic.Publish(t.ctx, &pubsub.Message{
			Data: data,
			Attributes: map[string]string{
				"request_id":     entry.RequestID,
				"schema_version": "1",
			},
		})
		if _, err := result.Get(t.ctx); err != nil {
			common.SysError("coslog pubsub publish failed: " + err.Error())
			recordDropped()
		}
	}
}

func (t *PubSubTransport) receiveLoop() {
	defer t.wg.Done()
	for t.ctx.Err() == nil {
		err := t.subscription.Receive(t.ctx, func(ctx context.Context, message *pubsub.Message) {
			delivery := &pubSubDelivery{
				data: append([]byte(nil), message.Data...),
				done: make(chan bool, 1),
			}
			select {
			case t.receiveCh <- delivery:
			case <-ctx.Done():
				message.Nack()
				return
			}
			select {
			case uploaded := <-delivery.done:
				if uploaded {
					message.Ack()
				} else {
					message.Nack()
				}
			case <-ctx.Done():
				message.Nack()
			}
		})
		if t.ctx.Err() != nil {
			return
		}
		if err != nil {
			common.SysError("coslog pubsub receive failed: " + err.Error())
		}
		select {
		case <-time.After(5 * time.Second):
		case <-t.ctx.Done():
			return
		}
	}
}

func (t *PubSubTransport) batchLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(t.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]*pubSubDelivery, 0, t.cfg.FlushSize)
	var batchBytes int64
	flush := func() {
		if len(batch) == 0 {
			return
		}
		records := make([][]byte, len(batch))
		for i, delivery := range batch {
			records[i] = delivery.data
		}
		uploaded := t.uploadRecords(records, true)
		for _, delivery := range batch {
			delivery.done <- uploaded
		}
		batch = batch[:0]
		batchBytes = 0
		t.bufferedEntries.Store(0)
	}

	for {
		select {
		case delivery := <-t.receiveCh:
			batch = append(batch, delivery)
			batchBytes += int64(len(delivery.data) + 1)
			t.bufferedEntries.Store(int64(len(batch)))
			if len(batch) >= t.cfg.FlushSize || batchBytes >= t.cfg.MaxFileSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-t.ctx.Done():
			flush()
			return
		}
	}
}

// uploadRecords writes the exact Pub/Sub payloads as JSONL. When nackOnFailure
// is true the local temporary file is removed after a failed upload because
// Pub/Sub remains the durable source and will redeliver the messages.
func (t *PubSubTransport) uploadRecords(records [][]byte, nackOnFailure bool) bool {
	if len(records) == 0 {
		return true
	}
	filePath, err := t.writeRecords(records)
	if err != nil {
		common.SysError("coslog pubsub local write failed: " + err.Error())
		return false
	}

	objectKey := filepath.Base(filePath)
	if t.cfg.Prefix != "" {
		objectKey = t.cfg.Prefix + "/" + objectKey
	}
	if err := t.uploader.Upload(context.Background(), objectKey, filePath); err != nil {
		common.SysError("coslog pubsub upload failed: " + err.Error())
		if nackOnFailure {
			_ = os.Remove(filePath)
		}
		return false
	}
	recordUploadSuccess()
	if t.cfg.DeleteAfterUpload {
		_ = os.Remove(filePath)
	}
	return true
}

func (t *PubSubTransport) writeRecords(records [][]byte) (string, error) {
	now := time.Now()
	r := rand.New(rand.NewSource(now.UnixNano()))
	filePath := filepath.Join(t.cfg.LocalDir, fmt.Sprintf("log_%s_%06d.jsonl", now.Format("20060102_150405"), r.Intn(1000000)))
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(filePath)
		}
	}()
	for _, record := range records {
		if _, err = file.Write(record); err != nil {
			return "", err
		}
		if _, err = file.Write([]byte{'\n'}); err != nil {
			return "", err
		}
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	success = true
	return filePath, nil
}

func (t *PubSubTransport) isClosed() bool {
	t.enqueueMu.RLock()
	defer t.enqueueMu.RUnlock()
	return t.closed
}

func (t *PubSubTransport) queueDepth() int {
	return len(t.publishCh)
}

func (t *PubSubTransport) queueCapacity() int {
	return cap(t.publishCh)
}

func (t *PubSubTransport) buffered() int {
	return int(t.bufferedEntries.Load())
}
