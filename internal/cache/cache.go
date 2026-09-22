// Package cache wraps Redis for sessions, response caching and login locks.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Cache struct {
	rdb *redis.Client
}

type Session struct {
	UserID    string    `json:"userId"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func New(ctx context.Context, addr, password string, db int) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Cache{rdb: rdb}, nil
}

func (c *Cache) Close() error { return c.rdb.Close() }

// ---------- sessions ----------

func (c *Cache) SaveSession(ctx context.Context, token string, s Session, ttl time.Duration) error {
	b, _ := json.Marshal(s)
	return c.rdb.Set(ctx, "session:"+token, b, ttl).Err()
}

func (c *Cache) GetSession(ctx context.Context, token string, ttl time.Duration) (*Session, error) {
	key := "session:" + token
	b, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	// sliding expiry
	_ = c.rdb.Expire(ctx, key, ttl).Err()
	return &s, nil
}

func (c *Cache) DeleteSession(ctx context.Context, token string) error {
	return c.rdb.Del(ctx, "session:"+token).Err()
}

// ---------- login idempotency ----------

// AcquireLoginLock returns true if this call is the first to claim the
// (bda, date) login; concurrent logins on the same day see false.
func (c *Cache) AcquireLoginLock(ctx context.Context, bdaID, date string) (bool, error) {
	return c.rdb.SetNX(ctx, fmt.Sprintf("login:%s:%s", bdaID, date), 1, 36*time.Hour).Result()
}

func (c *Cache) ReleaseLoginLock(ctx context.Context, bdaID, date string) error {
	return c.rdb.Del(ctx, fmt.Sprintf("login:%s:%s", bdaID, date)).Err()
}

// ---------- read cache ----------
//
// Cached responses are keyed with a global version number. Any write bumps
// the version, which orphans every existing key (they expire on their own).

func (c *Cache) version(ctx context.Context) string {
	v, err := c.rdb.Get(ctx, "cache:ver").Result()
	if err != nil {
		return "0"
	}
	return v
}

func (c *Cache) Invalidate(ctx context.Context) error {
	return c.rdb.Incr(ctx, "cache:ver").Err()
}

func (c *Cache) key(ctx context.Context, name string) string {
	return "cache:v" + c.version(ctx) + ":" + name
}

// GetJSON loads a cached value into v. ok is false on a miss.
func (c *Cache) GetJSON(ctx context.Context, name string, v any) (ok bool) {
	b, err := c.rdb.Get(ctx, c.key(ctx, name)).Bytes()
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (c *Cache) SetJSON(ctx context.Context, name string, v any, ttl time.Duration) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = c.rdb.Set(ctx, c.key(ctx, name), b, ttl).Err()
}

// FlushAll clears everything (demo reset).
func (c *Cache) FlushAll(ctx context.Context) error {
	return c.rdb.FlushDB(ctx).Err()
}
