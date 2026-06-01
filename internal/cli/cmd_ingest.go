package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
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

	// Routing: explicit curing+queue, then auto-route (source, ""), then store-only.
	resolvedCuring := *curingName
	resolvedQueue := *queueName
	if resolvedCuring == "" {
		router := curing.NewRouter(tannCfg.Routes)
		if route, ok := router.Match(*source, *kind); ok {
			resolvedCuring = route.Curing
			resolvedQueue = route.Queue
		}
	}

	if *dryRun {
		fmt.Fprintf(stdout, "[dry-run] hide_id   (not created)\n")
		fmt.Fprintf(stdout, "[dry-run] kind      %s\n", *kind)
		fmt.Fprintf(stdout, "[dry-run] source    %s\n", *source)
		if resolvedCuring != "" {
			fmt.Fprintf(stdout, "[dry-run] curing    %s\n", resolvedCuring)
			fmt.Fprintf(stdout, "[dry-run] queue     %s\n", resolvedQueue)
		}
		return 0
	}

	// Write hide.
	hs := hide.NewStore(tannCfg.HideDir)
	entry, err := hs.Put(*kind, *source, content, nil)
	if err != nil {
		fmt.Fprintf(stderr, "leather ingest: write hide: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "hide_id   %s\n", entry.ID)

	if resolvedCuring == "" || resolvedQueue == "" {
		return 0
	}

	// Enqueue curing work item using the serve-compatible queue dir.
	qmgr := queue.NewManager(filepath.Join(cfg.StateDir, "queues"))
	item := model.QueueItem{
		ID:         queue.GenerateItemID(),
		CuringName: resolvedCuring,
		HideID:     entry.ID,
		HideKind:   *kind,
		EnqueuedAt: time.Now().Unix(),
		Payload:    map[string]any{"hide_id": entry.ID, "curing": resolvedCuring},
	}
	if err := qmgr.Enqueue(resolvedQueue, item); err != nil {
		fmt.Fprintf(stderr, "leather ingest: enqueue: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "curing    %s\n", resolvedCuring)
	fmt.Fprintf(stdout, "queue     %s\n", resolvedQueue)
	return 0
}
