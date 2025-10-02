package rdapclient

import (
	"container/list"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type cachedMeta struct {
	ETag         string
	LastModified time.Time
	expiresAt    time.Time
	negUntil     time.Time
}

type cachedResponse struct {
	url  string
	body []byte
	meta cachedMeta
}

type respCache struct {
	mu     sync.Mutex
	cap    int
	ll     *list.List
	tab    map[string]*list.Element // key: URL
	defTTL time.Duration
	now    func() time.Time
}

func newRespCache(capacity int, defaultTTL time.Duration) *respCache {
	return &respCache{
		cap:    capacity,
		ll:     list.New(),
		tab:    make(map[string]*list.Element),
		defTTL: defaultTTL,
		now:    time.Now,
	}
}

func (c *respCache) Resize(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cap = n
	// If shrinking, evict immediately so memory pressure drops deterministically.
	for c.ll.Len() > c.cap {
		back := c.ll.Back()
		cr := back.Value.(cachedResponse)
		delete(c.tab, cr.url)
		c.ll.Remove(back)
	}
}

func (c *respCache) Get(u string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.tab[u]; ok {
		it := el.Value.(cachedResponse)
		// Negative cache hit: treat as a miss until negUntil expires.
		if !it.meta.negUntil.IsZero() && c.now().Before(it.meta.negUntil) {
			return nil, false
		}
		// Fresh positive entry with body.
		if c.now().Before(it.meta.expiresAt) && len(it.body) > 0 {
			c.ll.MoveToFront(el)
			return it.body, true
		}
	}
	return nil, false
}

func (c *respCache) FreshBody(u string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.tab[u]; ok {
		return el.Value.(cachedResponse).body
	}
	return nil
}

func (c *respCache) Meta(u string) (cachedMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.tab[u]; ok {
		return el.Value.(cachedResponse).meta, true
	}
	return cachedMeta{}, false
}

func (c *respCache) UpdateFreshness(u string, hdr http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.tab[u]; ok {
		it := el.Value.(cachedResponse)
		it.meta = mergeMeta(it.meta, hdr, c.defTTL, c.now())
		// Clear negative state on successful validator refresh.
		it.meta.negUntil = time.Time{}
		el.Value = it
		c.ll.MoveToFront(el)
	}
}

func (c *respCache) Store(u string, body []byte, hdr http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta := makeMeta(hdr, c.defTTL, c.now())
	cp := append([]byte(nil), body...)
	resp := cachedResponse{url: u, body: cp, meta: meta}

	if el, ok := c.tab[u]; ok {
		el.Value = resp
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(resp)
	c.tab[u] = el
	for c.ll.Len() > c.cap {
		back := c.ll.Back()
		cr := back.Value.(cachedResponse)
		delete(c.tab, cr.url) // correct key: URL
		c.ll.Remove(back)
	}
}

func (c *respCache) StoreNegative(u string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta := cachedMeta{negUntil: c.now().Add(d)}
	if el, ok := c.tab[u]; ok {
		it := el.Value.(cachedResponse)
		it.meta.negUntil = meta.negUntil
		el.Value = it
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(cachedResponse{url: u, meta: meta})
	c.tab[u] = el
}

func (c *respCache) StoreMeta(u string, hdr http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta := makeMeta(hdr, c.defTTL, c.now())
	if el, ok := c.tab[u]; ok {
		it := el.Value.(cachedResponse)
		it.meta = mergeMeta(it.meta, hdr, c.defTTL, c.now())
		el.Value = it
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(cachedResponse{url: u, meta: meta})
	c.tab[u] = el
}

func makeMeta(h http.Header, defTTL time.Duration, now time.Time) cachedMeta {
	m := cachedMeta{ETag: h.Get("ETag")}
	if lm := h.Get("Last-Modified"); lm != "" {
		if t, err := time.Parse(http.TimeFormat, lm); err == nil {
			m.LastModified = t
		}
	}
	m.expiresAt = now.Add(expiryFromHeaders(h, defTTL, now))
	return m
}

func mergeMeta(prev cachedMeta, h http.Header, defTTL time.Duration, now time.Time) cachedMeta {
	m := prev
	if et := h.Get("ETag"); et != "" {
		m.ETag = et
	}
	if lm := h.Get("Last-Modified"); lm != "" {
		if t, err := time.Parse(http.TimeFormat, lm); err == nil {
			m.LastModified = t
		}
	}
	m.expiresAt = now.Add(expiryFromHeaders(h, defTTL, now))
	return m
}

func expiryFromHeaders(h http.Header, defTTL time.Duration, now time.Time) time.Duration {
	cc := h.Get("Cache-Control")
	if cc != "" {
		lcc := strings.ToLower(cc)
		// Honor explicit no-store / no-cache with zero TTL.
		if strings.Contains(lcc, "no-store") || strings.Contains(lcc, "no-cache") {
			return 0
		}
		for _, p := range strings.Split(cc, ",") {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(strings.ToLower(p), "max-age=") {
				if n, err := strconv.Atoi(strings.TrimPrefix(p, "max-age=")); err == nil && n >= 0 {
					return time.Duration(n) * time.Second
				}
			}
		}
	}
	if exp := h.Get("Expires"); exp != "" {
		if t, err := time.Parse(http.TimeFormat, exp); err == nil {
			if d := t.Sub(now); d > 0 {
				return d
			}
		}
	}
	return defTTL
}
