package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/scheduler"
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
	fmt.Fprintf(stdout, "max tokens: %d  reserve: %d  threshold: %.0f%%\n",
		cfg.MaxTokens, cfg.CompletionReserve, cfg.SummarizeThreshold*100)

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
	for _, j := range jobs {
		fmt.Fprintln(stdout, formatJob(j))
	}
	return 0
}

// formatJob renders a single job record as a human-readable line.
func formatJob(j model.Job) string {
	last := "never"
	if j.LastRun > 0 {
		last = time.Unix(j.LastRun, 0).Format("2006-01-02 15:04:05")
	}
	next := "n/a"
	if j.NextRun > 0 {
		next = time.Unix(j.NextRun, 0).Format("2006-01-02 15:04:05")
	}
	return fmt.Sprintf("%-24s  %-8s  last=%-19s  next=%-19s  runs=%d",
		j.AgentName, string(j.Status), last, next, j.RunCount)
}
