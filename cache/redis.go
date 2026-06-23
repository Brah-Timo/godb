package cache

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ─────────────────────────────────────────────────────────────
//  RedisCache — Redis-backed distributed cache
// ─────────────────────────────────────────────────────────────

// RedisCache implements Cache using a Redis instance.
// It is safe for concurrent use and suitable for multi-replica deployments.
//
// Cache keys follow the pattern: "<prefix>:<table>:<hash>"
// Table invalidation uses Redis SCAN + DEL (atomic per key).
type RedisCache struct {
	client *goredis.Client
	prefix string
	hits   atomic.Int64
	misses atomic.Int64
}

// NewRedisCache creates a RedisCache connected to the given address.
func NewRedisCache(addr, password string, db int, prefix string) (*RedisCache, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("godb/cache/redis: ping failed: %w", err)
	}

	return &RedisCache{client: client, prefix: prefix}, nil
}

// ─────────────────────────────────────────────────────────────
//  Cache interface
// ─────────────────────────────────────────────────────────────

func (r *RedisCache) Get(key string) ([]byte, error) {
	ctx := context.Background()
	data, err := r.client.Get(ctx, r.prefix+":"+key).Bytes()
	if err != nil {
		r.misses.Add(1)
		if err == goredis.Nil {
			return nil, fmt.Errorf("cache miss: %s", key)
		}
		return nil, err
	}
	r.hits.Add(1)
	return data, nil
}

func (r *RedisCache) Set(key string, value []byte, ttl time.Duration) error {
	ctx := context.Background()
	return r.client.Set(ctx, r.prefix+":"+key, value, ttl).Err()
}

func (r *RedisCache) Delete(key string) error {
	ctx := context.Background()
	return r.client.Del(ctx, r.prefix+":"+key).Err()
}

// InvalidateTable scans for all keys matching "prefix:godb:<table>:*" and deletes them.
// Uses SCAN for safe iteration (does not block the Redis server like KEYS would).
func (r *RedisCache) InvalidateTable(table string) error {
	ctx := context.Background()
	pattern := fmt.Sprintf("%s:godb:%s:*", r.prefix, table)

	var cursor uint64
	for {
		keys, newCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("godb/cache/redis: scan failed: %w", err)
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("godb/cache/redis: delete failed: %w", err)
			}
		}
		cursor = newCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (r *RedisCache) Flush() error {
	ctx := context.Background()
	pattern := r.prefix + ":*"
	var cursor uint64
	for {
		keys, newCursor, err := r.client.Scan(ctx, cursor, pattern, 500).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			r.client.Del(ctx, keys...)
		}
		cursor = newCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (r *RedisCache) Stats() Stats {
	ctx := context.Background()
	info, _ := r.client.DBSize(ctx).Result()
	return Stats{
		Hits:    r.hits.Load(),
		Misses:  r.misses.Load(),
		Entries: info,
	}
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}
