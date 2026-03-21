package lock

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// testRedisClient returns a *redis.Client pointing to a local test Redis.
// If the connection fails, the test is skipped.
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skipping redis lock tests — no local Redis: %v", err)
	}
	t.Cleanup(func() { client.FlushDB(context.Background()); client.Close() })
	return client
}

func TestRedisLock_AcquireAndRelease(t *testing.T) {
	client := testRedisClient(t)
	lock := NewRedisLock(client)

	ctx := context.Background()
	key := "test:lock:acquire_release"

	acquired, err := lock.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.True(t, acquired)

	err = lock.Unlock(ctx, key)
	assert.NoError(t, err)

	// Key should be gone
	exists, _ := client.Exists(ctx, key).Result()
	assert.Equal(t, int64(0), exists)
}

func TestRedisLock_MutualExclusion(t *testing.T) {
	client := testRedisClient(t)
	lock1 := NewRedisLock(client)
	lock2 := NewRedisLock(client)

	ctx := context.Background()
	key := "test:lock:mutex"

	acquired, err := lock1.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.True(t, acquired)

	// Second lock with a different owner should fail
	acquired2, err := lock2.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.False(t, acquired2)

	// Release first lock
	err = lock1.Unlock(ctx, key)
	assert.NoError(t, err)

	// Now second lock should succeed
	acquired3, err := lock2.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.True(t, acquired3)

	lock2.Unlock(ctx, key)
}

func TestRedisLock_SafeRelease(t *testing.T) {
	client := testRedisClient(t)
	lock1 := NewRedisLock(client)
	lock2 := NewRedisLock(client)

	ctx := context.Background()
	key := "test:lock:safe_release"

	// lock1 acquires
	acquired, err := lock1.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.True(t, acquired)

	// lock2 tries to unlock — should NOT delete lock1's lock (Lua check)
	err = lock2.Unlock(ctx, key)
	assert.NoError(t, err)

	// Key should still exist (lock1 still holds it)
	exists, _ := client.Exists(ctx, key).Result()
	assert.Equal(t, int64(1), exists)

	// lock1 can still unlock
	err = lock1.Unlock(ctx, key)
	assert.NoError(t, err)
}

func TestRedisLock_TTLExpiry(t *testing.T) {
	client := testRedisClient(t)
	lock := NewRedisLock(client)

	ctx := context.Background()
	key := "test:lock:ttl"

	acquired, err := lock.Lock(ctx, key, 200*time.Millisecond)
	assert.NoError(t, err)
	assert.True(t, acquired)

	// Wait for TTL to expire
	time.Sleep(300 * time.Millisecond)

	// Another lock should now succeed
	lock2 := NewRedisLock(client)
	acquired2, err := lock2.Lock(ctx, key, 10*time.Second)
	assert.NoError(t, err)
	assert.True(t, acquired2)

	lock2.Unlock(ctx, key)
}
