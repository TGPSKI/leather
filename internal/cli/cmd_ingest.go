package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/TGPSKI/leather/internal/config"
	"github.com/TGPSKI/leather/internal/curing"
	"github.com/TGPSKI/leather/internal/hide"
	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/queue"
)

// RunIngest implements the "leather ingest" subcommand.
// Returns exit code: 0 success, 1 error, 2 usage error.
func RunIngest(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ingest", stderr)
	// Bind core config flags (registers --config, --state-dir, --tannery, etc.) so that
	// config.Load(fs) can find the queue directory and tannery path at runtime.
	config.BindFlags(fs)
	kind := fs.String("kind", "", "hide kind label (required)")
	source := fs.String("source", "cli", "source label")
	curingName := fs.String("curing", "", "explicit curing name (optional)")
	queueName := fs.String("queue", "", "explicit queue name (requires --curing)")
	dryRun := fs.Bool("dry-run", false, "print what would be created without writing to disk")
	if !parseFlags(fs, args) {
		return 2
	}

	// Load main config for queue dir (state-dir) and tannery path.
	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: load config: %v\n", err)
		return 1
	}

	if cfg.TanneryFile == "" {
		fmt.Fprintln(stderr, "leather ingest: --tannery is required")
		return 2
	}
	if *kind == "" {
		fmt.Fprintln(stderr, "leather ingest: --kind is required")
		return 2
	}

	// Load tannery config for hide dir and routes.
	tannCfg, err := config.LoadTannery(cfg.TanneryFile)
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: load tannery: %v\n", err)
		return 1
	}

	// Resolve input: file argument or stdin.
	var content []byte
	if rest := fs.Args(); len(rest) > 0 {
		content, err = os.ReadFile(rest[0])
		if err != nil {
			fmt.Fprintf(stderr, "leather ingest: read file: %v\n", err)
			return 1
		}
	} else {
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "leather ingest: read stdin: %v\n", err)
			return 1
		}
	}

	// Routing goes through the same rule POST /intake uses, so the two surfaces
	// agree on what a given parameter set means and an unroutable one fails
	// here rather than exiting 0 with the routing lines quietly absent (#75).
	curingDefs, err := curing.LoadDir(tannCfg.CuringDir)
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: load curings: %v\n", err)
		return 1
	}
	routing, err := curing.ResolveRouting(curing.RoutingRequest{
		Curing:    *curingName,
		Queue:     *queueName,
		Source:    *source,
		EventType: *kind,
		Router:    curing.NewRouter(tannCfg.Routes),
		Defs:      curingDefs,
		Queues:    tannCfg.Queues,
	})
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: %v\n", err)
		return 1
	}

	if *dryRun {
		fmt.Fprintf(stdout, "[dry-run] hide_id   (not created)\n")
		fmt.Fprintf(stdout, "[dry-run] kind      %s\n", *kind)
		fmt.Fprintf(stdout, "[dry-run] source    %s\n", *source)
		if routing.Routed() {
			fmt.Fprintf(stdout, "[dry-run] curing    %s\n", routing.Curing)
			fmt.Fprintf(stdout, "[dry-run] queue     %s\n", routing.QueueFor("<hide_id>"))
		} else {
			fmt.Fprintf(stdout, "[dry-run] routing   none (hide stored, nothing enqueued)\n")
		}
		return 0
	}

	// A running serve holds every queue it polls in memory and rewrites the
	// backing file from that snapshot, so an item appended here would never be
	// seen and would be overwritten by the serve's next save. Refuse rather
	// than accept work that quietly disappears (issue #76).
	if routing.Routed() {
		lockPath := filepath.Join(cfg.StateDir, "leather.lock")
		if running, pid := serveIsRunning(lockPath); running {
			holder := ""
			if pid > 0 {
				holder = fmt.Sprintf(" (pid %d)", pid)
			}
			fmt.Fprintf(stderr, "leather ingest: a serve is running against this state dir%s — its queues are held in memory, so an item appended here would be invisible to it and overwritten by its next write.\n", holder)
			fmt.Fprintf(stderr, "  post to that serve instead:  curl -X POST '<api_addr>/intake?kind=%s&curing=%s' --data-binary @<file>\n", *kind, routing.Curing)
			fmt.Fprintf(stderr, "  or stop the serve, ingest, then start it again.\n")
			return 1
		}
	}

	// Write hide.
	hs := hide.NewStore(tannCfg.HideDir)
	entry, err := hs.Put(*kind, *source, content, nil)
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: write hide: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "hide_id   %s\n", entry.ID)

	if !routing.Routed() {
		// Say so out loud. The absence of the curing/queue lines was previously
		// the only tell that nothing downstream would ever see this hide.
		fmt.Fprintf(stdout, "routing   none (hide stored, nothing enqueued)\n")
		return 0
	}

	// Enqueue curing work item using the serve-compatible queue dir.
	destQueue := routing.QueueFor(entry.ID)
	qmgr := queue.NewManager(filepath.Join(cfg.StateDir, "queues"))
	item := model.QueueItem{
		ID:         queue.GenerateItemID(),
		CuringName: routing.Curing,
		HideID:     entry.ID,
		HideKind:   *kind,
		EnqueuedAt: time.Now().Unix(),
		Payload:    map[string]any{"hide_id": entry.ID, "curing": routing.Curing},
	}
	if err := qmgr.Enqueue(destQueue, item); err != nil {
		fmt.Fprintf(stderr, "leather ingest: enqueue: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "curing    %s\n", routing.Curing)
	fmt.Fprintf(stdout, "queue     %s\n", destQueue)
	return 0
}
