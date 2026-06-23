package cache

import (
	"fmt"
	"time"
)

// NopCache is a no-operation cache. All reads return a cache miss.
// Useful in tests or when caching is explicitly disabled.
type NopCache struct{}

// NewNop returns a new NopCache. It satisfies the Cache interface and is safe
// for use wherever a Cache is required but caching is not desired.
func NewNop() Cache { return NopCache{} }

func (NopCache) Get(key string) ([]byte, error)                { return nil, fmt.Errorf("cache miss: %s", key) }
func (NopCache) Set(_ string, _ []byte, _ time.Duration) error { return nil }
func (NopCache) Delete(_ string) error                         { return nil }
func (NopCache) InvalidateTable(_ string) error                { return nil }
func (NopCache) Flush() error                                  { return nil }
func (NopCache) Stats() Stats                                  { return Stats{} }
func (NopCache) Close() error                                  { return nil }
