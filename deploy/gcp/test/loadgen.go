package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type counters struct {
	started        atomic.Uint64
	completed      atomic.Uint64
	transportErr   atomic.Uint64
	status2xx      atomic.Uint64
	status4xx      atomic.Uint64
	status5xx      atomic.Uint64
	otherStatus    atomic.Uint64
	inFlight       atomic.Int64
	peakInFlight   atomic.Int64
	firstByteNanos atomic.Uint64
	totalNanos     atomic.Uint64
}

type summary struct {
	RPM                int            `json:"rpm"`
	DurationSeconds    float64        `json:"duration_seconds"`
	StreamPercent      int            `json:"stream_percent"`
	InputBytes         int            `json:"input_bytes"`
	Started            uint64         `json:"started"`
	Completed          uint64         `json:"completed"`
	TransportErrors    uint64         `json:"transport_errors"`
	Status2xx          uint64         `json:"status_2xx"`
	Status4xx          uint64         `json:"status_4xx"`
	Status5xx          uint64         `json:"status_5xx"`
	OtherStatus        uint64         `json:"other_status"`
	PeakInFlight       int64          `json:"peak_in_flight"`
	AverageFirstByteMS float64        `json:"average_first_byte_ms"`
	AverageTotalMS     float64        `json:"average_total_ms"`
	StatusCounts       map[int]uint64 `json:"status_counts"`
	ElapsedSeconds     float64        `json:"elapsed_seconds"`
}

func updatePeak(peak *atomic.Int64, current int64) {
	for {
		old := peak.Load()
		if current <= old || peak.CompareAndSwap(old, current) {
			return
		}
	}
}

func main() {
	target := flag.String("target", "http://136.68.218.109/v1/chat/completions", "NewAPI chat completions URL")
	tokenFile := flag.String("token-file", "", "file containing the API token")
	rpm := flag.Int("rpm", 5000, "request rate per minute")
	duration := flag.Duration("duration", 2*time.Minute, "traffic generation duration")
	streamPercent := flag.Int("stream-percent", 85, "percentage of streaming requests")
	inputBytes := flag.Int("input-bytes", 16384, "approximate user prompt size")
	requestTimeout := flag.Duration("request-timeout", 2*time.Minute, "per-request timeout")
	maxInFlight := flag.Int("max-in-flight", 20000, "maximum concurrent requests")
	flag.Parse()

	if *tokenFile == "" || *rpm <= 0 || *duration <= 0 || *streamPercent < 0 || *streamPercent > 100 || *maxInFlight <= 0 {
		log.Fatal("invalid arguments")
	}
	tokenBytes, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatalf("read token: %v", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		log.Fatal("empty token")
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          20000,
		MaxIdleConnsPerHost:   20000,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
	}
	client := &http.Client{Transport: transport}
	prompt := "gcp-load-validation " + strings.Repeat("x", *inputBytes)
	streamBody, _ := json.Marshal(map[string]any{
		"model": "gcp-validation-model", "messages": []any{map[string]any{"role": "user", "content": prompt}}, "stream": true,
	})
	nonStreamBody, _ := json.Marshal(map[string]any{
		"model": "gcp-validation-model", "messages": []any{map[string]any{"role": "user", "content": prompt}}, "stream": false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), *duration+*requestTimeout+time.Minute)
	defer cancel()
	var stats counters
	statusCounts := make(map[int]uint64)
	var statusMu sync.Mutex
	semaphore := make(chan struct{}, *maxInFlight)
	var wg sync.WaitGroup
	start := time.Now()
	deadline := start.Add(*duration)
	interval := time.Minute / time.Duration(*rpm)

scheduleLoop:
	for sequence := 0; ; sequence++ {
		scheduled := start.Add(time.Duration(sequence) * interval)
		if !scheduled.Before(deadline) {
			break
		}
		if wait := time.Until(scheduled); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				break scheduleLoop
			}
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			break scheduleLoop
		}

		isStream := sequence%100 < *streamPercent
		body := nonStreamBody
		if isStream {
			body = streamBody
		}
		stats.started.Add(1)
		inFlight := stats.inFlight.Add(1)
		updatePeak(&stats.peakInFlight, inFlight)
		wg.Add(1)
		go func(payload []byte) {
			defer wg.Done()
			defer func() {
				<-semaphore
				stats.inFlight.Add(-1)
			}()

			requestStart := time.Now()
			requestCtx, requestCancel := context.WithTimeout(ctx, *requestTimeout)
			defer requestCancel()
			req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, *target, bytes.NewReader(payload))
			if err != nil {
				stats.transportErr.Add(1)
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				stats.transportErr.Add(1)
				return
			}
			firstByteAt := time.Now()
			_, copyErr := io.Copy(io.Discard, resp.Body)
			closeErr := resp.Body.Close()
			if copyErr != nil || closeErr != nil {
				stats.transportErr.Add(1)
			}
			stats.completed.Add(1)
			stats.firstByteNanos.Add(uint64(firstByteAt.Sub(requestStart)))
			stats.totalNanos.Add(uint64(time.Since(requestStart)))
			statusMu.Lock()
			statusCounts[resp.StatusCode]++
			statusMu.Unlock()
			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 300:
				stats.status2xx.Add(1)
			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				stats.status4xx.Add(1)
			case resp.StatusCode >= 500 && resp.StatusCode < 600:
				stats.status5xx.Add(1)
			default:
				stats.otherStatus.Add(1)
			}
		}(body)
	}

	wg.Wait()
	elapsed := time.Since(start)
	completed := stats.completed.Load()
	result := summary{
		RPM:             *rpm,
		DurationSeconds: (*duration).Seconds(),
		StreamPercent:   *streamPercent,
		InputBytes:      *inputBytes,
		Started:         stats.started.Load(),
		Completed:       completed,
		TransportErrors: stats.transportErr.Load(),
		Status2xx:       stats.status2xx.Load(),
		Status4xx:       stats.status4xx.Load(),
		Status5xx:       stats.status5xx.Load(),
		OtherStatus:     stats.otherStatus.Load(),
		PeakInFlight:    stats.peakInFlight.Load(),
		StatusCounts:    statusCounts,
		ElapsedSeconds:  elapsed.Seconds(),
	}
	if completed > 0 {
		result.AverageFirstByteMS = float64(stats.firstByteNanos.Load()) / float64(completed) / float64(time.Millisecond)
		result.AverageTotalMS = float64(stats.totalNanos.Load()) / float64(completed) / float64(time.Millisecond)
	}

	keys := make([]int, 0, len(statusCounts))
	for code := range statusCounts {
		keys = append(keys, code)
	}
	sort.Ints(keys)
	ordered := make(map[string]uint64, len(keys))
	for _, code := range keys {
		ordered[fmt.Sprint(code)] = statusCounts[code]
	}
	output := map[string]any{"summary": result, "status_counts_ordered": ordered}
	encoded, _ := json.MarshalIndent(output, "", "  ")
	fmt.Println(string(encoded))
}
