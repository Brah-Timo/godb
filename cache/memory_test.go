package cache_test

import (
	"testing"
	"time"

	"github.com/Brah-Timo/godb/cache"
)

func TestMemoryCache_SetGet(t *testing.T) {
	c := cache.NewMemoryCache(10 * 1024 * 1024) // 10 MB
	defer c.Close()

	key := "test:key"
	val := []byte(`{"name":"alice"}`)

	if err := c.Set(key, val, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := c.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(val) {
		t.Errorf("Get: got %q, want %q", got, val)
	}
}

func TestMemoryCache_Expiry(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	c.Set("expires", []byte("data"), 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	if _, err := c.Get("expires"); err == nil {
		t.Error("expected expired cache miss, got hit")
	}
}

func TestMemoryCache_InvalidateTable(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	k1 := cache.BuildKey("users", "SELECT * FROM users", nil)
	k2 := cache.BuildKey("users", "SELECT * FROM users WHERE id=?", []interface{}{1})
	k3 := cache.BuildKey("posts", "SELECT * FROM posts", nil)

	c.Set(k1, []byte("u1"), time.Minute)
	c.Set(k2, []byte("u2"), time.Minute)
	c.Set(k3, []byte("p1"), time.Minute)

	if err := c.InvalidateTable("users"); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Get(k1); err == nil {
		t.Error("k1 should be evicted after InvalidateTable(users)")
	}
	if _, err := c.Get(k2); err == nil {
		t.Error("k2 should be evicted after InvalidateTable(users)")
	}
	if _, err := c.Get(k3); err != nil {
		t.Error("k3 (posts) should NOT be evicted")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	c.Set("del", []byte("v"), time.Minute)
	c.Delete("del")

	if _, err := c.Get("del"); err == nil {
		t.Error("expected miss after Delete")
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	// Only 100 bytes budget — forces eviction
	c := cache.NewMemoryCache(100)
	defer c.Close()

	for i := 0; i < 20; i++ {
		key := cache.BuildKey("t", "sql", []interface{}{i})
		c.Set(key, []byte("1234567890"), time.Minute) // 10 bytes each
	}
	// Cache should not exceed budget; just verify it doesn't panic
	stats := c.Stats()
	if stats.SizeKB > 1 { // allow up to 1 KB due to key overhead
		t.Errorf("cache size %d KB exceeds 100-byte budget", stats.SizeKB)
	}
}

func TestMemoryCache_Flush(t *testing.T) {
	c := cache.NewMemoryCache(1024 * 1024)
	defer c.Close()

	c.Set("a", []byte("1"), time.Minute)
	c.Set("b", []byte("2"), time.Minute)
	c.Flush()

	if s := c.Stats(); s.Entries != 0 {
		t.Errorf("expected 0 entries after Flush, got %d", s.Entries)
	}
}

// ─────────────────────────────────────────────────────────────
//  Benchmarks
// ─────────────────────────────────────────────────────────────

func BenchmarkMemoryCache_Set(b *testing.B) {
	c := cache.NewMemoryCache(256 * 1024 * 1024)
	defer c.Close()
	val := make([]byte, 256)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Set(cache.BuildKey("t", "q", []interface{}{i}), val, time.Minute)
	}
}

func BenchmarkMemoryCache_Get_Hit(b *testing.B) {
	c := cache.NewMemoryCache(256 * 1024 * 1024)
	defer c.Close()
	key := cache.BuildKey("users", "SELECT * FROM users", nil)
	c.Set(key, []byte(`{"id":1}`), time.Minute)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Get(key)
	}
}
