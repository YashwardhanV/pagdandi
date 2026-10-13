package plan

import (
	"container/list"
	"sync"
	"time"
)

// lru is a small in-memory cache with a TTL. Plans are cheap to recompute;
// the cache only absorbs repeated taps on the same request.
type lru struct {
	mu  sync.Mutex
	max int
	ttl time.Duration
	ll  *list.List
	m   map[string]*list.Element
}

type entry struct {
	key string
	val any
	exp time.Time
}

func newLRU(max int, ttl time.Duration) *lru {
	return &lru{max: max, ttl: ttl, ll: list.New(), m: map[string]*list.Element{}}
}

func (c *lru) get(k string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok {
		return nil, false
	}
	en := e.Value.(*entry)
	if time.Now().After(en.exp) {
		c.ll.Remove(e)
		delete(c.m, k)
		return nil, false
	}
	c.ll.MoveToFront(e)
	return en.val, true
}

func (c *lru) put(k string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[k]; ok {
		e.Value = &entry{k, v, time.Now().Add(c.ttl)}
		c.ll.MoveToFront(e)
		return
	}
	c.m[k] = c.ll.PushFront(&entry{k, v, time.Now().Add(c.ttl)})
	for c.ll.Len() > c.max {
		old := c.ll.Back()
		c.ll.Remove(old)
		delete(c.m, old.Value.(*entry).key)
	}
}
