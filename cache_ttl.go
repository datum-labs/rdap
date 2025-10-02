package rdapclient

import (
	"container/list"
	"sync"
	"time"
)

type ttlItem[T any] struct {
	key     string
	val     T
	expires time.Time
}

type ttlCache[T any] struct {
	mu    sync.Mutex
	ll    *list.List
	tab   map[string]*list.Element
	cap   int
	ttl   time.Duration
	now   func() time.Time
}

func newTTLCache[T any](ttl time.Duration, capacity int) *ttlCache[T] {
	return &ttlCache[T]{ll: list.New(), tab: make(map[string]*list.Element), cap: capacity, ttl: ttl, now: time.Now}
}

func (c *ttlCache[T]) Resize(n int) { c.mu.Lock(); c.cap = n; c.mu.Unlock() }

func (c *ttlCache[T]) Get(k string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	if el, ok := c.tab[k]; ok {
		it := el.Value.(ttlItem[T])
		if c.now().Before(it.expires) { c.ll.MoveToFront(el); return it.val, true }
		delete(c.tab, k); c.ll.Remove(el)
	}
	return zero, false
}

func (c *ttlCache[T]) Set(k string, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.tab[k]; ok {
		el.Value = ttlItem[T]{key: k, val: v, expires: c.now().Add(c.ttl)}
		c.ll.MoveToFront(el); return
	}
	el := c.ll.PushFront(ttlItem[T]{key: k, val: v, expires: c.now().Add(c.ttl)})
	c.tab[k] = el
	for c.ll.Len() > c.cap {
		b := c.ll.Back(); delete(c.tab, b.Value.(ttlItem[T]).key); c.ll.Remove(b)
	}
}
