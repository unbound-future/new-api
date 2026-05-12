package prom_metrics

import (
	"errors"
	"testing"
	"time"
)

func TestLRU_GetSet(t *testing.T) {
	c := newUsernameCache(3, time.Minute)
	c.Set(1, "alice")
	c.Set(2, "bob")
	if v, ok := c.Get(1); !ok || v != "alice" {
		t.Fatalf("expected alice/true got %q/%v", v, ok)
	}
	if _, ok := c.Get(999); ok {
		t.Fatalf("expected miss for 999")
	}
}

func TestLRU_Eviction(t *testing.T) {
	c := newUsernameCache(2, time.Minute)
	c.Set(1, "a")
	c.Set(2, "b")
	c.Set(3, "c") // 应淘汰最旧的 1
	if _, ok := c.Get(1); ok {
		t.Fatalf("expected 1 to be evicted")
	}
	if v, ok := c.Get(2); !ok || v != "b" {
		t.Fatalf("expected 2/b, got %v/%q", ok, v)
	}
}

func TestLRU_TTL(t *testing.T) {
	c := newUsernameCache(2, 10*time.Millisecond)
	c.Set(1, "a")
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get(1); ok {
		t.Fatalf("expected expired entry to miss")
	}
}

func TestLRU_TouchOnGet(t *testing.T) {
	c := newUsernameCache(2, time.Minute)
	c.Set(1, "a")
	c.Set(2, "b")
	_, _ = c.Get(1) // 触摸 1,2 变成 LRU
	c.Set(3, "c")   // 应淘汰 2
	if _, ok := c.Get(2); ok {
		t.Fatalf("expected 2 to be evicted, not 1")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatalf("expected 1 to remain after touch")
	}
}

func TestResolveUsername_Fallback(t *testing.T) {
	c := newUsernameCache(8, time.Minute)
	// 注入失败 fetcher,返回 unknown
	got := c.ResolveWith(42, func(int) (string, error) {
		return "", errors.New("boom")
	})
	if got != LabelUnknown {
		t.Fatalf("expected %q on fetcher error, got %q", LabelUnknown, got)
	}
}

func TestResolveUsername_FetchAndCache(t *testing.T) {
	c := newUsernameCache(8, time.Minute)
	calls := 0
	fetch := func(id int) (string, error) {
		calls++
		return "alice", nil
	}
	if got := c.ResolveWith(1, fetch); got != "alice" {
		t.Fatalf("expected alice, got %q", got)
	}
	if got := c.ResolveWith(1, fetch); got != "alice" {
		t.Fatalf("expected alice, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 fetcher call after warm cache, got %d", calls)
	}
}

func TestLRU_SetUpdate(t *testing.T) {
	c := newUsernameCache(2, time.Minute)
	c.Set(1, "old")
	c.Set(1, "new")
	if v, _ := c.Get(1); v != "new" {
		t.Fatalf("expected updated value 'new', got %q", v)
	}
	c.Set(2, "b") // 不应触发淘汰,因为 1 是 update
	if _, ok := c.Get(1); !ok {
		t.Fatalf("expected 1 to survive after update + Set(2)")
	}
}

func TestResolveUsername_EmptyNameNotCached(t *testing.T) {
	c := newUsernameCache(8, time.Minute)
	calls := 0
	fetch := func(int) (string, error) { calls++; return "", nil }
	if got := c.ResolveWith(7, fetch); got != LabelUnknown {
		t.Fatalf("expected %q, got %q", LabelUnknown, got)
	}
	if got := c.ResolveWith(7, fetch); got != LabelUnknown {
		t.Fatalf("expected %q, got %q", LabelUnknown, got)
	}
	if calls != 2 {
		t.Fatalf("empty-name miss should not cache; expected 2 calls, got %d", calls)
	}
}
