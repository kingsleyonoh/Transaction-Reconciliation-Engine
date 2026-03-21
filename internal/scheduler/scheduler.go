package scheduler

import (
	"context"
	"log"
	"sync"
	"time"
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
	jobs []Job
	wg   sync.WaitGroup
}

// New creates a new Scheduler.
func New() *Scheduler {
	return &Scheduler{}
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
	log.Printf("scheduler: started %d jobs", len(s.jobs))
}

// Stop blocks until all job goroutines have exited.
func (s *Scheduler) Stop() {
	s.wg.Wait()
	log.Println("scheduler: all jobs stopped")
}

func (s *Scheduler) run(ctx context.Context, j Job) {
	defer s.wg.Done()
	ticker := time.NewTicker(j.Interval)
	defer ticker.Stop()

	log.Printf("scheduler: job %q registered (every %s)", j.Name, j.Interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("scheduler: job %q stopping", j.Name)
			return
		case <-ticker.C:
			start := time.Now()
			log.Printf("scheduler: job %q starting", j.Name)
			if err := j.Fn(ctx); err != nil {
				log.Printf("scheduler: job %q failed after %s: %v", j.Name, time.Since(start), err)
			} else {
				log.Printf("scheduler: job %q completed in %s", j.Name, time.Since(start))
			}
		}
	}
}
