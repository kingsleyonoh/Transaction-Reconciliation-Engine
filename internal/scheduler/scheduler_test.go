package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestScheduler_JobExecutes(t *testing.T) {
	var count int64

	s := New(zerolog.Nop())
	s.Register("test_job", 50*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Wait enough time for ~3 ticks
	time.Sleep(180 * time.Millisecond)
	cancel()
	s.Stop()

	got := atomic.LoadInt64(&count)
	assert.True(t, got >= 2, "expected at least 2 executions, got %d", got)
}

func TestScheduler_ContextCancellation(t *testing.T) {
	var count int64

	s := New(zerolog.Nop())
	s.Register("cancel_job", 20*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Let it tick a few times
	time.Sleep(80 * time.Millisecond)
	cancel()
	s.Stop()

	before := atomic.LoadInt64(&count)

	// Wait more — should NOT increase
	time.Sleep(80 * time.Millisecond)
	after := atomic.LoadInt64(&count)

	assert.Equal(t, before, after, "job should not execute after context cancellation")
}

func TestScheduler_MultipleJobs(t *testing.T) {
	var countA, countB int64

	s := New(zerolog.Nop())
	s.Register("job_a", 40*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt64(&countA, 1)
		return nil
	})
	s.Register("job_b", 40*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt64(&countB, 1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(120 * time.Millisecond)
	cancel()
	s.Stop()

	assert.True(t, atomic.LoadInt64(&countA) >= 1, "job_a should have executed")
	assert.True(t, atomic.LoadInt64(&countB) >= 1, "job_b should have executed")
}

func TestScheduler_ErrorHandling(t *testing.T) {
	// Verify that a failing job does not crash the scheduler
	var count int64

	s := New(zerolog.Nop())
	s.Register("error_job", 30*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return assert.AnError
	})

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()
	s.Stop()

	got := atomic.LoadInt64(&count)
	assert.True(t, got >= 2, "error_job should keep running despite errors")
}
