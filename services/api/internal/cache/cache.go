// Package cache provides the route cache and token-bucket rate limiter, backed
// by Redis in production and by process memory in unit tests.
package cache

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is a byte cache plus a token-bucket limiter.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Allow takes one token from the bucket `key` that refills `rate` tokens per `per`
	// with capacity `rate`. It returns whether the request is allowed and, if not,
	// how long until a token is available.
	Allow(ctx context.Context, key string, rate int, per time.Duration) (bool, time.Duration, error)
	Ping(ctx context.Context) error
}

// Redis implements Cache on a go-redis client.
type Redis struct {
	C *redis.Client
}

// NewRedis parses a redis:// URL.
func NewRedis(url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &Redis{C: redis.NewClient(opt)}, nil
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := r.C.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return r.C.Set(ctx, key, val, ttl).Err()
}

func (r *Redis) Ping(ctx context.Context) error { return r.C.Ping(ctx).Err() }

// tokenBucket keeps {tokens, ts_ms} in a hash and refills lazily.
var tokenBucket = redis.NewScript(`
local key = KEYS[1]
local cap = tonumber(ARGV[1])
local per_ms = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then tokens = cap; ts = now end
local refill = (now - ts) * cap / per_ms
tokens = math.min(cap, tokens + refill)
local allowed = 0
local wait = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = math.ceil((1 - tokens) * per_ms / cap)
end
redis.call('HSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, per_ms * 2)
return {allowed, wait}
`)

func (r *Redis) Allow(ctx context.Context, key string, rate int, per time.Duration) (bool, time.Duration, error) {
	res, err := tokenBucket.Run(ctx, r.C, []string{"rl:" + key}, rate, per.Milliseconds(), time.Now().UnixMilli()).Int64Slice()
	if err != nil {
		return true, 0, err // fail open: rate limiting must not take the API down
	}
	return res[0] == 1, time.Duration(res[1]) * time.Millisecond, nil
}

// Memory is an in-process Cache for tests and Redis-less dev.
type Memory struct {
	mu      sync.Mutex
	items   map[string]memItem
	buckets map[string]*bucket
	now     func() time.Time
}

type memItem struct {
	val []byte
	exp time.Time
}

type bucket struct {
	tokens float64
	ts     time.Time
}

// NewMemory returns an empty in-memory cache.
func NewMemory() *Memory {
	return &Memory{items: map[string]memItem{}, buckets: map[string]*bucket{}, now: time.Now}
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[key]
	if !ok || m.now().After(it.exp) {
		delete(m.items, key)
		return nil, false, nil
	}
	return it.val, true, nil
}

func (m *Memory) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[key] = memItem{val: append([]byte(nil), val...), exp: m.now().Add(ttl)}
	return nil
}

func (m *Memory) Ping(context.Context) error { return nil }

func (m *Memory) Allow(_ context.Context, key string, rate int, per time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(rate), ts: now}
		m.buckets[key] = b
	}
	b.tokens = math.Min(float64(rate), b.tokens+now.Sub(b.ts).Seconds()*float64(rate)/per.Seconds())
	b.ts = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0, nil
	}
	wait := time.Duration((1 - b.tokens) * per.Seconds() / float64(rate) * float64(time.Second))
	return false, wait, nil
}

// Key joins parts into a cache key.
func Key(parts ...string) string {
	n := 0
	for _, p := range parts {
		n += len(p) + 1
	}
	b := make([]byte, 0, n)
	for i, p := range parts {
		if i > 0 {
			b = append(b, ':')
		}
		b = append(b, p...)
	}
	return string(b)
}

// Round5 formats a coordinate rounded to 5 decimal places (≈1 m).
func Round5(v float64) string { return strconv.FormatFloat(math.Round(v*1e5)/1e5, 'f', 5, 64) }
