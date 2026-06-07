package curing

import (
	"context"
	"sync"
	"time"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// Supervisor manages a fleet of Workers across all configured curings.
// One Worker is created per CuringDefinition; concurrency is controlled per-queue
// via QueueConcurrencyConfig.Concurrency (the semaphore on each Worker).
type Supervisor struct {
	workers []*Worker
	wg      sync.WaitGroup
}

// NewSupervisor constructs one Worker per CuringDefinition.
// concMap maps queue name to QueueConcurrencyConfig. Missing queue entries default to
// concurrency=1, max_attempts=3. deps is shared (by pointer) across all workers.
func NewSupervisor(
	defs []model.CuringDefinition,
	agents map[string]model.Agent,
	concMap map[string]model.QueueConcurrencyConfig,
	hideStore *hide.Store,
	artStore *artifact.Store,
	deps *RunnerDeps,
	qmgr *queue.Manager,
	router *Router,
	log *logging.Logger,
) (*Supervisor, error) {
	s := &Supervisor{}
	for _, def := range defs {
		conc := 1
		poll := time.Second
		if cfg, ok := concMap[def.Queue]; ok {
			if cfg.Concurrency > 0 {
				conc = cfg.Concurrency
			}
			if cfg.PollInterval > 0 {
				poll = cfg.PollInterval
			}
		}
		w, err := NewWorker(def, agents, conc, poll, hideStore, artStore, deps, qmgr, router, log)
		if err != nil {
			return nil, err
		}
		s.workers = append(s.workers, w)
	}
	return s, nil
}

// Start launches one goroutine per Worker. Goroutines are tracked by the
// internal WaitGroup and exit when ctx is cancelled.
func (s *Supervisor) Start(ctx context.Context) {
	for _, w := range s.workers {
		s.wg.Add(1)
		go func(w *Worker) {
			defer s.wg.Done()
			w.Run(ctx)
		}(w)
	}
}

// TotalActive returns the sum of in-flight item handlers across all workers.
// A non-zero value means workers are still processing items even if all queues
// report depth 0 (items are dequeued before processing begins).
func (s *Supervisor) TotalActive() int {
	total := 0
	for _, w := range s.workers {
		total += w.ActiveCount()
	}
	return total
}

// Drain waits for all worker Run loops to exit AND for any in-flight item
// handlers spawned by those loops to finish. Call after ctx is cancelled
// during graceful shutdown.
//
// Returns when both conditions are met. There is no internal timeout; callers
// that need a bounded shutdown should run Drain in a goroutine and select on
// their own deadline.
func (s *Supervisor) Drain() {
	s.wg.Wait()
	for _, w := range s.workers {
		w.WaitInflight()
	}
}
