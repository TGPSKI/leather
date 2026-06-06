package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/queue"
)

// RunDLQ is the entry point for the "leather dlq" sub-command.
// It dispatches to inspect or requeue based on the first positional argument.
func RunDLQ(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, dlqUsage)
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "inspect":
		return runDLQInspect(rest, stdout, stderr)
	case "requeue":
		return runDLQRequeue(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "leather dlq: unknown sub-command %q\n\n", sub)
		fmt.Fprint(stderr, dlqUsage)
		return 2
	}
}

const dlqUsage = `leather dlq — inspect and requeue outbound dead-letter queue items

Usage:
  leather dlq inspect  [--queue outbound-dlq] [flags]
  leather dlq requeue  <item-id> [--queue outbound-dlq] [--work-queue <name>] [flags]

Sub-commands:
  inspect   list items currently in the DLQ
  requeue   move a DLQ item back to a work queue for re-processing

Use "leather dlq <sub-command> --help" for flag details.
`

// runDLQInspect lists items in the named DLQ queue.
func runDLQInspect(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dlq inspect", stderr)
	config.BindFlags(fs)
	queueName := fs.String("queue", "outbound-dlq", "DLQ queue name to inspect")
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather dlq inspect: %v\n", err)
		return 1
	}

	queueDir := filepath.Join(cfg.StateDir, "queues")
	mgr := queue.NewManager(queueDir)
	q, err := mgr.Get(*queueName)
	if err != nil {
		fmt.Fprintf(stderr, "leather dlq inspect: open queue %q: %v\n", *queueName, err)
		return 1
	}

	items := q.Scan()
	if len(items) == 0 {
		fmt.Fprintf(stdout, "leather dlq inspect: queue %q is empty\n", *queueName)
		return 0
	}

	fmt.Fprintf(stdout, "%-26s  %-20s  %-20s  %-6s  %-30s  %s\n",
		"ID", "tool", "agent", "attempt", "enqueued_at", "error")
	fmt.Fprintln(stdout, strings.Repeat("-", 120))
	for _, item := range items {
		ts := time.Unix(item.EnqueuedAt, 0).Format("2006-01-02 15:04:05")
		tool := item.ToolName
		if tool == "" {
			if t, ok := item.Payload["tool"].(string); ok {
				tool = t
			}
		}
		errStr := ""
		if e, ok := item.Payload["error"].(string); ok {
			errStr = e
		}
		if len(errStr) > 60 {
			errStr = errStr[:60] + "…"
		}
		attempt := item.AttemptCount
		if a, ok := item.Payload["attempt"].(float64); ok && attempt == 0 {
			attempt = int(a)
		}
		fmt.Fprintf(stdout, "%-26s  %-20s  %-20s  %-6d  %-30s  %s\n",
			item.ID, truncate(tool, 20), truncate(item.AgentName, 20), attempt, ts, errStr)
	}
	return 0
}

// runDLQRequeue moves a named item from the DLQ to a work queue.
// Usage: leather dlq requeue [flags] <item-id>
// The item-id must be the last argument (after all flags).
func runDLQRequeue(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("dlq requeue", stderr)
	config.BindFlags(fs)
	queueName := fs.String("queue", "outbound-dlq", "DLQ queue name to read from")
	workQueue := fs.String("work-queue", "", "destination work queue; defaults to <queue> with -dlq suffix removed")
	if !parseFlags(fs, args) {
		return 2
	}

	positional := fs.Args()
	if len(positional) == 0 {
		fmt.Fprintf(stderr, "leather dlq requeue: missing item-id argument\n")
		fmt.Fprint(stderr, "Usage: leather dlq requeue [flags] <item-id>\n")
		return 2
	}
	itemID := positional[0]

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather dlq requeue: %v\n", err)
		return 1
	}

	dest := *workQueue
	if dest == "" {
		dest = strings.TrimSuffix(*queueName, "-dlq")
		if dest == *queueName {
			// queue name didn't end in -dlq; use it as-is (caller must specify --work-queue)
			fmt.Fprintf(stderr, "leather dlq requeue: queue %q does not end in '-dlq'; use --work-queue to specify destination\n", *queueName)
			return 2
		}
	}

	queueDir := filepath.Join(cfg.StateDir, "queues")
	mgr := queue.NewManager(queueDir)

	dlqQ, err := mgr.Get(*queueName)
	if err != nil {
		fmt.Fprintf(stderr, "leather dlq requeue: open queue %q: %v\n", *queueName, err)
		return 1
	}

	items := dlqQ.Scan()
	found := false
	for _, item := range items {
		if item.ID != itemID {
			continue
		}
		found = true
		// Reset attempt count so the item gets a fresh retry budget.
		item.AttemptCount = 0

		// Dequeue from DLQ.
		removed, deqErr := dlqQ.DequeueByIDs([]string{itemID})
		if deqErr != nil || len(removed) == 0 {
			fmt.Fprintf(stderr, "leather dlq requeue: dequeue item %q: %v\n", itemID, deqErr)
			return 1
		}

		// Enqueue to work queue.
		if enqErr := mgr.Enqueue(dest, item); enqErr != nil {
			fmt.Fprintf(stderr, "leather dlq requeue: enqueue to %q: %v\n", dest, enqErr)
			return 1
		}
		fmt.Fprintf(stdout, "requeued %s → %s\n", itemID, dest)
		return 0
	}

	if !found {
		fmt.Fprintf(stderr, "leather dlq requeue: item %q not found in queue %q\n", itemID, *queueName)
		return 1
	}
	return 0
}

// truncate shortens s to max runes, appending "…" when truncated.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
