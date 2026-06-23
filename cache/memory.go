package cache

import (
	"container/list"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─────────────────────────────────────────────────────────────
//  MemoryCache — LRU in-process cache
// ─────────────────────────────────────────────────────────────

// MemoryCache is a goroutine-safe in-memory LRU cache.
// It evicts least-recently-used entries when the byte budget is exceeded.
type MemoryCache struct {
	mu      sync.Mutex
	items   map[string]*list.Element // O(1) lookup
	lru     *list.List               // front = MRU, back = LRU
	maxSize int64                    // max bytes
	curSize int64                    // current bytes

	hits   atomic.Int64
	misses atomic.Int64

	stopCleanup chan struct{}
}

// cacheEntry is stored as list.Element.Value.
type cacheEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
	tableTag  string // first segment after "godb:" for fast table invalidation
	size      int64
}

// NewMemoryCache creates an in-process LRU cache with the given byte budget.
func NewMemoryCache(maxBytes int64) *MemoryCache {
	mc := &MemoryCache{
		items:       make(map[string]*list.Element),
		lru:         list.New(),
		maxSize:     maxBytes,
		stopCleanup: make(chan struct{}),
	}
	go mc.cleanupLoop()
	return mc
}

// ─────────────────────────────────────────────────────────────
//  Cache interface implementation
// ─────────────────────────────────────────────────────────────

func (mc *MemoryCache) Get(key string) ([]byte, error) {
	mc.mu.Lock()
	elem, ok := mc.items[key]
	if !ok {
		mc.mu.Unlock()
		mc.misses.Add(1)
		return nil, fmt.Errorf("cache miss: %s", key)
	}
	entry := elem.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		mc.removeElement(elem)
		mc.mu.Unlock()
		mc.misses.Add(1)
		return nil, fmt.Errorf("cache expired: %s", key)
	}
	mc.lru.MoveToFront(elem)
	data := make([]byte, len(entry.value))
	copy(data, entry.value) // safe copy before releasing lock
	mc.mu.Unlock()
	mc.hits.Add(1)
	return data, nil
}

func (mc *MemoryCache) Set(key string, value []byte, ttl time.Duration) error {
	size := int64(len(value))
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Evict until we have room
	for mc.curSize+size > mc.maxSize && mc.lru.Len() > 0 {
		mc.evictLRU()
	}

	// Update existing
	if elem, exists := mc.items[key]; exists {
		old := elem.Value.(*cacheEntry)
		mc.curSize -= old.size
		mc.lru.Remove(elem)
		delete(mc.items, key)
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
		tableTag:  extractTableTag(key),
		size:      size,
	}
	elem := mc.lru.PushFront(entry)
	mc.items[key] = elem
	mc.curSize += size
	return nil
}

func (mc *MemoryCache) Delete(key string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if elem, ok := mc.items[key]; ok {
		mc.removeElement(elem)
	}
	return nil
}

// InvalidateTable removes all cache entries whose table tag matches.
// Time complexity: O(n) where n is the number of cached entries.
// In practice n is bounded by the maxSize budget, so this is acceptable.
func (mc *MemoryCache) InvalidateTable(table string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	tag := strings.ToLower(table)
	for key, elem := range mc.items {
		entry := elem.Value.(*cacheEntry)
		if entry.tableTag == tag {
			mc.curSize -= entry.size
			mc.lru.Remove(elem)
			delete(mc.items, key)
		}
	}
	return nil
}

func (mc *MemoryCache) Flush() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.items = make(map[string]*list.Element)
	mc.lru.Init()
	mc.curSize = 0
	return nil
}

func (mc *MemoryCache) Stats() Stats {
	mc.mu.Lock()
	entries := int64(mc.lru.Len())
	sizeKB := mc.curSize / 1024
	mc.mu.Unlock()
	return Stats{
		Hits:    mc.hits.Load(),
		Misses:  mc.misses.Load(),
		Entries: entries,
		SizeKB:  sizeKB,
	}
}

func (mc *MemoryCache) Close() error {
	close(mc.stopCleanup)
	return mc.Flush()
}

// ─────────────────────────────────────────────────────────────
//  Internal helpers
// ─────────────────────────────────────────────────────────────

// evictLRU removes the least-recently-used entry. Caller must hold mc.mu.
func (mc *MemoryCache) evictLRU() {
	if back := mc.lru.Back(); back != nil {
		mc.removeElement(back)
	}
}

// removeElement removes an element from both the list and map. Caller must hold mc.mu.
func (mc *MemoryCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	mc.curSize -= entry.size
	mc.lru.Remove(elem)
	delete(mc.items, entry.key)
}

// cleanupLoop periodically removes expired entries to prevent stale memory.
func (mc *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-mc.stopCleanup:
			return
		case <-ticker.C:
			mc.evictExpired()
		}
	}
}

func (mc *MemoryCache) evictExpired() {
	now := time.Now()
	mc.mu.Lock()
	defer mc.mu.Unlock()
	for _, elem := range mc.items {
		entry := elem.Value.(*cacheEntry)
		if now.After(entry.expiresAt) {
			mc.removeElement(elem)
		}
	}
}

// extractTableTag parses the table name from a godb cache key.
// Key format: "godb:<table>:<hash>"
func extractTableTag(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) >= 2 {
		return strings.ToLower(parts[1])
	}
	return ""
}
