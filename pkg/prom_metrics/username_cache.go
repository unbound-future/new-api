package prom_metrics

import (
	"container/list"
	"sync"
	"time"
)

// usernameCache 是一个进程内 LRU,带 TTL,用于把 user_id 映射到 username。
// 实现纯 stdlib (container/list + sync.Mutex),不引入新外部依赖。
type usernameCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	ll       *list.List            // front = MRU, back = LRU
	idx      map[int]*list.Element // user_id -> list element
}

type cacheEntry struct {
	userID    int
	name      string
	expiresAt time.Time
}

func newUsernameCache(capacity int, ttl time.Duration) *usernameCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &usernameCache{
		capacity: capacity,
		ttl:      ttl,
		ll:       list.New(),
		idx:      make(map[int]*list.Element, capacity),
	}
}

func (c *usernameCache) Get(id int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.idx[id]
	if !ok {
		return "", false
	}
	e := el.Value.(*cacheEntry)
	if time.Now().After(e.expiresAt) {
		c.ll.Remove(el)
		delete(c.idx, id)
		return "", false
	}
	c.ll.MoveToFront(el)
	return e.name, true
}

func (c *usernameCache) Set(id int, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.idx[id]; ok {
		e := el.Value.(*cacheEntry)
		e.name = name
		e.expiresAt = time.Now().Add(c.ttl)
		c.ll.MoveToFront(el)
		return
	}
	e := &cacheEntry{userID: id, name: name, expiresAt: time.Now().Add(c.ttl)}
	el := c.ll.PushFront(e)
	c.idx[id] = el

	for c.ll.Len() > c.capacity {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		old := oldest.Value.(*cacheEntry)
		c.ll.Remove(oldest)
		delete(c.idx, old.userID)
	}
}

// ResolveWith 是 hot path 帮助函数:命中即返回;未命中调用 fetcher,失败兜底 LabelUnknown。
func (c *usernameCache) ResolveWith(id int, fetcher func(int) (string, error)) string {
	if v, ok := c.Get(id); ok {
		return v
	}
	name, err := fetcher(id)
	if err != nil || name == "" {
		return LabelUnknown
	}
	c.Set(id, name)
	return name
}
