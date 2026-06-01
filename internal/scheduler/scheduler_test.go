package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

func TestScheduler_Register_EmptyName(t *testing.T) {
	s := New(Options{})
	err := s.Register("", "* * * * *", func(_ context.Context, _ model.Job) error { return nil })
	if err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestScheduler_Register_InvalidSchedule(t *testing.T) {
	s := New(Options{})
	err := s.Register("job", "not-a-cron", func(_ context.Context, _ model.Job) error { return nil })
	if err == nil {
		t.Error("expected error for invalid schedule, got nil")
	}
}

func TestScheduler_Jobs_Empty(t *testing.T) {
	s := New(Options{})
	jobs := s.Jobs()
	if jobs == nil {
		t.Error("Jobs() returned nil, want empty slice")
	}
	if len(jobs) != 0 {
		t.Errorf("Jobs() len = %d, want 0", len(jobs))
	}
}

func TestScheduler_Jobs_Populated(t *testing.T) {
	s := New(Options{})
	noop := func(_ context.Context, _ model.Job) error { return nil }
	if err := s.Register("job-a", "once", noop); err != nil {
		t.Fatalf("Register job-a: %v", err)
	}
	if err := s.Register("job-b", "0 * * * *", noop); err != nil {
		t.Fatalf("Register job-b: %v", err)
	}
	if n := len(s.Jobs()); n != 2 {
		t.Errorf("Jobs() len = %d, want 2", n)
	}
}

func TestScheduler_Drain_NoJobs(t *testing.T) {
	s := New(Options{})
	if err := s.Drain(time.Second); err != nil {
		t.Errorf("Drain with no jobs: %v", err)
	}
}

func TestScheduler_Register_Once_Fires(t *testing.T) {
	s := New(Options{})
	handled := make(chan model.Job, 1)

	if err := s.Register("once-job", "once", func(_ context.Context, j model.Job) error {
		handled <- j
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Start(ctx) }()
	defer cancel()

	select {
	case j := <-handled:
		if j.AgentName != "once-job" {
			t.Errorf("AgentName = %q, want once-job", j.AgentName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called within 5 seconds")
	}

	cancel()
	if err := s.Drain(time.Second); err != nil {
		t.Errorf("Drain: %v", err)
	}
}

func TestScheduler_Register_Once_JobStatusAfter(t *testing.T) {
	s := New(Options{})
	done := make(chan struct{})

	if err := s.Register("track-job", "once", func(_ context.Context, _ model.Job) error {
		close(done)
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Start(ctx) }()
	defer cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called")
	}

	if err := s.Drain(time.Second); err != nil {
		t.Errorf("Drain: %v", err)
	}

	for _, j := range s.Jobs() {
		if j.AgentName == "track-job" {
			if j.Status != model.JobStatusSuccess {
				t.Errorf("Status = %q, want success", j.Status)
			}
			if j.RunCount != 1 {
				t.Errorf("RunCount = %d, want 1", j.RunCount)
			}
			if j.NextRun != 0 {
				t.Errorf("NextRun = %d, want 0 (once job)", j.NextRun)
			}
			return
		}
	}
	t.Error("job 'track-job' not found in Jobs()")
}

func TestScheduler_ErrorHandler(t *testing.T) {
	s := New(Options{})
	done := make(chan struct{})

	if err := s.Register("err-job", "once", func(_ context.Context, _ model.Job) error {
		defer close(done)
		return context.DeadlineExceeded
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = s.Start(ctx) }()
	defer cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler not called")
	}

	if err := s.Drain(time.Second); err != nil {
		t.Errorf("Drain: %v", err)
	}

	for _, j := range s.Jobs() {
		if j.AgentName == "err-job" {
			if j.Status != model.JobStatusError {
				t.Errorf("Status = %q, want error", j.Status)
			}
			if j.LastError == "" {
				t.Error("LastError should not be empty after failed handler")
			}
			return
		}
	}
	t.Error("job 'err-job' not found")
}

func TestScheduler_Deregister_NotFound(t *testing.T) {
	s := New(Options{})
	if err := s.Deregister("missing"); err == nil {
		t.Error("expected error deregistering nonexistent job, got nil")
	}
}

func TestScheduler_Deregister_Removes(t *testing.T) {
	s := New(Options{})
	noop := func(_ context.Context, _ model.Job) error { return nil }
	if err := s.Register("job-a", "0 * * * *", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := s.Register("job-b", "0 * * * *", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if n := len(s.Jobs()); n != 2 {
		t.Fatalf("before deregister: Jobs() len = %d, want 2", n)
	}
	if err := s.Deregister("job-a"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	jobs := s.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("after deregister: Jobs() len = %d, want 1", len(jobs))
	}
	if jobs[0].AgentName != "job-b" {
		t.Errorf("remaining job AgentName = %q, want job-b", jobs[0].AgentName)
	}
	if err := s.Deregister("job-a"); err == nil {
		t.Error("expected error deregistering already-removed job, got nil")
	}
}
