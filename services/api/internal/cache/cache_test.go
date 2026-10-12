package cache

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMemoryCache(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	_ = m.Set(ctx, "k", []byte("v"), time.Minute)
	if v, ok, _ := m.Get(ctx, "k"); !ok || string(v) != "v" {
		t.Fatal("expected hit")
	}
	clock = clock.Add(2 * time.Minute)
	if _, ok, _ := m.Get(ctx, "k"); ok {
		t.Fatal("expected expiry")
	}
}

func TestMemoryTokenBucket(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		if ok, _, _ := m.Allow(ctx, "ip", 3, time.Minute); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	ok, wait, _ := m.Allow(ctx, "ip", 3, time.Minute)
	if ok || wait <= 0 || wait > 20*time.Second {
		t.Fatalf("4th request should be limited with ~20s wait, got ok=%v wait=%v", ok, wait)
	}
	clock = clock.Add(20 * time.Second)
	if ok, _, _ := m.Allow(ctx, "ip", 3, time.Minute); !ok {
		t.Fatal("bucket should have refilled one token")
	}
}

func TestRedisTokenBucket(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	ctx := context.Background()
	r, err := NewRedis(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	key := "test:" + time.Now().Format(time.RFC3339Nano)
	for i := 0; i < 2; i++ {
		if ok, _, err := r.Allow(ctx, key, 2, time.Minute); err != nil || !ok {
			t.Fatalf("request %d: ok=%v err=%v", i, ok, err)
		}
	}
	if ok, wait, _ := r.Allow(ctx, key, 2, time.Minute); ok || wait <= 0 {
		t.Fatalf("third request must be limited (wait=%v)", wait)
	}
	_ = r.Set(ctx, key+":v", []byte("x"), time.Second)
	if v, ok, _ := r.Get(ctx, key+":v"); !ok || string(v) != "x" {
		t.Fatal("redis get/set failed")
	}
	if Round5(75.857749) != "75.85775" || Key("a", "b") != "a:b" {
		t.Fatal("key helpers")
	}
}
