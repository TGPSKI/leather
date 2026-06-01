package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// JobHandler is the function signature invoked by the scheduler on each trigger.
type JobHandler func(ctx context.Context, job model.Job) error

// Options configures a Scheduler instance.
type Options struct {
	// MaxConcurrent caps the number of simultaneously running handlers.
	// Defaults to 4 if zero.
	MaxConcurrent int
	// StateDir is the directory where job state is persisted after each run.
	// If empty, state persistence is disabled.
	StateDir string
	// TickInterval controls how often the scheduler wakes to check for due jobs.
	// Defaults to time.Minute if zero. Set to time.Second for second-granularity schedules.
	TickInterval time.Duration
}

// Scheduler drives periodic agent execution based on cron expressions.
// A background goroutine ticks every 30 seconds and dispatches due jobs.
// Each job runs in its own goroutine, bounded by a semaphore.
type Scheduler struct {
	opts Options
	mu   sync.RWMutex
	jobs map[string]*scheduledJob
	wg   sync.WaitGroup
	sem  chan struct{}
}

type scheduledJob struct {
	job      model.Job
	schedule *Schedule
	handler  JobHandler
}

// New returns a Scheduler configured with opts.
func New(opts Options) *Scheduler {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 4
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = time.Minute
	}
	return &Scheduler{
		opts: opts,
		jobs: make(map[string]*scheduledJob),
		sem:  make(chan struct{}, opts.MaxConcurrent),
	}
}

// Register adds a job for name with the given schedule expression and handler.
// It returns an error if scheduleExpr cannot be parsed or if name is empty.
func (s *Scheduler) Register(name, scheduleExpr string, handler JobHandler) error {
	if name == "" {
		return fmt.Errorf("scheduler/Register: name must not be empty")
	}

	sched, err := ParseSchedule(scheduleExpr)
	if err != nil {
		return fmt.Errorf("scheduler/Register %q: %w", name, err)
	}

	now := time.Now()
	var nextRun int64
	if !sched.Once() {
		nextRun = sched.Next(now).Unix()
	} else {
		nextRun = now.Unix() // fire immediately on first tick
	}

	s.mu.Lock()
	s.jobs[name] = &scheduledJob{
		job: model.Job{
			AgentName: name,
			Status:    model.JobStatusPending,
			NextRun:   nextRun,
		},
		schedule: sched,
		handler:  handler,
	}
	s.mu.Unlock()
	return nil
}

// Start runs the scheduler loop until ctx is cancelled.
// It performs an immediate tick on entry, then ticks at the configured TickInterval
// (set via Options; defaults to 1 minute). Use a sub-minute interval when running
// six-field (second-granularity) schedules.
// This function blocks; run it in a goroutine.
func (s *Scheduler) Start(ctx context.Context) error {
	s.tick(ctx)

	ticker := time.NewTicker(s.opts.TickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// Drain waits for all in-flight handlers to complete, up to timeout.
// Returns an error if timeout elapses before all handlers finish.
func (s *Scheduler) Drain(timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("scheduler/Drain: timed out after %s", timeout)
	}
}

// Jobs returns a snapshot of all current job records, safe for concurrent reads.
func (s *Scheduler) Jobs() []model.Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Job, 0, len(s.jobs))
	for _, sj := range s.jobs {
		out = append(out, sj.job)
	}
	return out
}

// Deregister removes a job from the scheduler. If the job is currently
// running, it is not interrupted; it simply will not be scheduled again.
// Returns an error if no job with the given name exists.
func (s *Scheduler) Deregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[name]; !ok {
		return fmt.Errorf("scheduler: no job named %q", name)
	}
	delete(s.jobs, name)
	return nil
}

// tick checks all registered jobs and dispatches those that are due.
func (s *Scheduler) tick(ctx context.Context) {
	now := time.Now().Unix()

	s.mu.RLock()
	var due []*scheduledJob
	for _, sj := range s.jobs {
		if sj.job.NextRun > 0 && sj.job.NextRun <= now && sj.job.Status != model.JobStatusRunning {
			due = append(due, sj)
		}
	}
	s.mu.RUnlock()

	for _, sj := range due {
		select {
		case s.sem <- struct{}{}:
			s.wg.Add(1)
			// Set Running before spawning the goroutine so a concurrent tick
			// cannot re-dispatch this job before the goroutine acquires the lock.
			s.mu.Lock()
			sj.job.Status = model.JobStatusRunning
			sj.job.LastRun = time.Now().Unix()
			s.mu.Unlock()
			go s.runJob(ctx, sj)
		default:
			// Concurrency cap reached; mark as skipped for this tick.
			s.mu.Lock()
			sj.job.Status = model.JobStatusSkipped
			s.mu.Unlock()
		}
	}
}

// runJob executes the handler for sj and updates job state.
func (s *Scheduler) runJob(ctx context.Context, sj *scheduledJob) {
	defer func() {
		<-s.sem
		s.wg.Done()
	}()

	jobSnapshot := sj.job
	err := sj.handler(ctx, jobSnapshot)

	// Capture end time after the handler so Next() always returns a future time,
	// even when the handler runs longer than the schedule interval.
	endTime := time.Now()

	s.mu.Lock()
	sj.job.RunCount++
	if err != nil {
		sj.job.Status = model.JobStatusError
		sj.job.LastError = err.Error()
	} else {
		sj.job.Status = model.JobStatusSuccess
		sj.job.LastError = ""
	}
	if sj.schedule.Once() {
		// One-shot jobs do not reschedule.
		sj.job.NextRun = 0
	} else {
		sj.job.NextRun = sj.schedule.Next(endTime).Unix()
	}
	s.mu.Unlock()

	if s.opts.StateDir != "" {
		// Best-effort persist after each job completes.
		_ = saveState(s.opts.StateDir, s.Jobs())
	}
}
