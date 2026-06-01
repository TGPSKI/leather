package worker

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// Supervisor manages a set of polling workers, one goroutine per worker.
// Create with NewSupervisor; call Start to launch all workers.
type Supervisor struct {
	workers []*HTTPPollWorker
	wg      sync.WaitGroup
}

// NewSupervisor constructs a Supervisor from a set of WorkerDefinitions.
// Workers that fail to initialise are logged as warnings and skipped;
// the remaining workers are started normally.
func NewSupervisor(defs []model.WorkerDefinition, mgr *queue.Manager, log *logging.Logger) *Supervisor {
	s := &Supervisor{}
	for _, def := range defs {
		w, err := newHTTPPollWorker(def, mgr, log)
		if err != nil {
			log.Warn("worker init failed, skipping", "worker", def.Name, "error", err)
			continue
		}
		s.workers = append(s.workers, w)
	}
	return s
}

// Start launches one goroutine per worker. It returns immediately; the
// goroutines run until ctx is cancelled. Call Drain to wait for all workers
// to finish after the context is cancelled.
func (s *Supervisor) Start(ctx context.Context) {
	for _, w := range s.workers {
		s.wg.Add(1)
		go func(w *HTTPPollWorker) {
			defer s.wg.Done()
			w.Run(ctx)
		}(w)
	}
}

// Drain blocks until all worker goroutines have exited.
// It is safe to call Drain even if Start was never called.
func (s *Supervisor) Drain() {
	s.wg.Wait()
}

// WorkerStatus is a snapshot of a single worker's runtime state.
type WorkerStatus struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Interval   string `json:"interval"`
	Queue      string `json:"queue"`
	LastPollAt int64  `json:"last_poll_at"` // Unix timestamp; 0 = never polled
	QueueLen   int    `json:"queue_len"`    // -1 if unavailable
}

// Workers returns a snapshot of all workers managed by the supervisor.
func (s *Supervisor) Workers() []WorkerStatus {
	out := make([]WorkerStatus, 0, len(s.workers))
	for _, w := range s.workers {
		qLen := -1
		if q, err := w.mgr.Get(w.def.Output.Queue); err == nil {
			qLen = q.Len()
		}
		out = append(out, WorkerStatus{
			Name:       w.def.Name,
			Type:       w.def.Type,
			Interval:   w.def.Interval.String(),
			Queue:      w.def.Output.Queue,
			LastPollAt: atomic.LoadInt64(&w.lastPollAt),
			QueueLen:   qLen,
		})
	}
	return out
}
