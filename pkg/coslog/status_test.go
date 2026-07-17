package coslog

import (
	"testing"
	"time"
)

func TestWriterDropsWhenQueueIsFull(t *testing.T) {
	before := droppedTotal.Load()
	w := &JSONLWriter{ch: make(chan COSLOG, 1)}
	w.Write(COSLOG{RequestID: "first"})
	w.Write(COSLOG{RequestID: "second"})
	if got := droppedTotal.Load() - before; got != 1 {
		t.Fatalf("dropped %d entries, want 1", got)
	}
}

func TestWriterEnqueueDoesNotWaitForFileWorker(t *testing.T) {
	w := &JSONLWriter{ch: make(chan COSLOG, 1)}
	w.mu.Lock()
	done := make(chan struct{})
	go func() {
		w.Write(COSLOG{RequestID: "request"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		w.mu.Unlock()
		t.Fatal("enqueue waited for the file worker mutex")
	}
	w.mu.Unlock()
}

func TestResetDroppedTotal(t *testing.T) {
	original := droppedTotal.Swap(7)
	t.Cleanup(func() { droppedTotal.Store(original) })

	if got := ResetDroppedTotal(); got != 7 {
		t.Fatalf("reset returned %d, want 7", got)
	}
	if got := droppedTotal.Load(); got != 0 {
		t.Fatalf("dropped total after reset = %d, want 0", got)
	}
}
