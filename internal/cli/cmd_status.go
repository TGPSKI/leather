package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/TGPSKI/leather/internal/config"
	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/scheduler"
)

// RunStatus prints scheduler state and token budget information.
func RunStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("status", stderr)
	config.BindFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather status: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "agent dir:  %s\n", cfg.AgentDir)
	fmt.Fprintf(stdout, "state dir:  %s\n", cfg.StateDir)
	fmt.Fprintf(stdout, "endpoint:   %s\n", cfg.LLMEndpoint)
	fmt.Fprintf(stdout, "max tokens: %d  completion reserve: %d  reasoning reserve: %d  threshold: %.0f%%\n",
		cfg.MaxTokens, cfg.CompletionReserve, cfg.ReasoningReserve, cfg.SummarizeThreshold*100)

	// Every job field below comes off disk, so without this line a scheduler
	// that died an hour ago prints exactly what a healthy idle one prints —
	// including a next= it will never honour (issue #77). `serve` holds this
	// lock for its lifetime and the kernel drops it on exit, so acquiring it
	// is proof no serve is running.
	running, pid := serveIsRunning(filepath.Join(cfg.StateDir, "leather.lock"))
	switch {
	case running && pid > 0:
		fmt.Fprintf(stdout, "serve:      running (pid %d)\n", pid)
	case running:
		fmt.Fprintf(stdout, "serve:      running\n")
	default:
		fmt.Fprintf(stdout, "serve:      not running — schedules below are the last state on disk, not a live plan\n")
	}

	jobs, err := scheduler.LoadState(cfg.StateDir)
	if err != nil {
		fmt.Fprintf(stderr, "leather status: load state: %v\n", err)
		return 1
	}

	if len(jobs) == 0 {
		fmt.Fprintln(stdout, "\n(no persisted job records found — run 'leather serve' first)")
		return 0
	}

	fmt.Fprintln(stdout)
	now := time.Now()
	for _, j := range jobs {
		fmt.Fprintln(stdout, formatJob(j, now))
	}
	return 0
}

// formatJob renders a single job record as a human-readable line.
// A next= already in the past is marked stale: a live scheduler advances it, so
// a fire time that has come and gone is a dead-scheduler tell on its own.
func formatJob(j model.Job, now time.Time) string {
	last := "never"
	if j.LastRun > 0 {
		last = time.Unix(j.LastRun, 0).Format("2006-01-02 15:04:05")
	}
	next := "n/a"
	if j.NextRun > 0 {
		next = time.Unix(j.NextRun, 0).Format("2006-01-02 15:04:05")
		if time.Unix(j.NextRun, 0).Before(now) {
			next += " (stale)"
		}
	}
	return fmt.Sprintf("%-24s  %-8s  last=%-19s  next=%-27s  runs=%d",
		j.AgentName, string(j.Status), last, next, j.RunCount)
}
