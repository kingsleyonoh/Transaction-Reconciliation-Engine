package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// JobFunc is a function executed by the scheduler on each tick.
type JobFunc func(ctx context.Context) error

// Job describes a recurring task.
type Job struct {
	Name     string
	Interval time.Duration
	Fn       JobFunc
}

// Scheduler runs registered jobs at their configured intervals.
type Scheduler struct {
	jobs   []Job
	wg     sync.WaitGroup
	logger zerolog.Logger
}

// New creates a new Scheduler with a zerolog logger.
func New(logger zerolog.Logger) *Scheduler {
	return &Scheduler{logger: logger}
}

// Register adds a job to the scheduler. Must be called before Start.
func (s *Scheduler) Register(name string, interval time.Duration, fn JobFunc) {
	s.jobs = append(s.jobs, Job{Name: name, Interval: interval, Fn: fn})
}

// Start launches a goroutine per job. Each goroutine ticks at the job's interval
// and stops when ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		s.wg.Add(1)
		go s.run(ctx, j)
	}
	s.logger.Info().Int("job_count", len(s.jobs)).Msg("scheduler started")
}

// Stop blocks until all job goroutines have exited.
func (s *Scheduler) Stop() {
	s.wg.Wait()
	s.logger.Info().Msg("scheduler: all jobs stopped")
}

func (s *Scheduler) run(ctx context.Context, j Job) {
	defer s.wg.Done()
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()

	s.logger.Info().Str("job", j.Name).Str("interval", j.Interval.String()).Msg("job registered")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info().Str("job", j.Name).Msg("job stopping")
			return
		case <-ticker.C:
			start := time.Now()
			s.logger.Info().Str("job", j.Name).Msg("job starting")
			if err := j.Fn(ctx); err != nil {
				s.logger.Error().Str("job", j.Name).Dur("duration", time.Since(start)).Err(err).Msg("job failed")
			} else {
				s.logger.Info().Str("job", j.Name).Dur("duration", time.Since(start)).Msg("job completed")
			}
		}
	}
}
