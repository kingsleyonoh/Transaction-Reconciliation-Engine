package lock

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisLock implements engine.DistributedLocker using Redis SETNX + Lua for safe release.
type RedisLock struct {
	client *redis.Client
	owner  string // unique identifier for this process instance
}

// NewRedisLock creates a new RedisLock with the given Redis client.
func NewRedisLock(client *redis.Client) *RedisLock {
	return &RedisLock{
		client: client,
		owner:  uuid.New().String(),
	}
}

// Lock acquires a distributed lock with the given key and TTL.
// Returns (true, nil) on success, (false, nil) if the lock is held by another owner.
func (r *RedisLock) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := r.client.SetNX(ctx, key, r.owner, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// unlockScript atomically deletes the key only if it still belongs to this owner.
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// Unlock releases the distributed lock only if it is still held by this owner.
func (r *RedisLock) Unlock(ctx context.Context, key string) error {
	_, err := unlockScript.Run(ctx, r.client, []string{key}, r.owner).Result()
	return err
}
