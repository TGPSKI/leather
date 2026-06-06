package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

const workflowUsage = `Usage: leather workflow <subcommand> [flags]

Subcommands:
  run    ingest a hide and drain all curing queues to completion (parallel)

Limitations: queue_pattern and collect_size curings are not supported.
Use "leather workflow <subcommand> --help" for per-subcommand flag details.
`

const workflowRunUsage = `Usage: leather workflow run [flags] [file]

Ingest a hide and run all configured curing workers until every queue is
quiescent. Workers for different curings run in parallel; fan-out from a
planner curing into executor curings is handled automatically.

An HTTP API server is started on --api-addr for the duration of the run so
that MCP tools in child processes can POST hides via LEATHER_INTAKE_URL.

Flags:
  --tannery    path to tannery.yaml (required)
  --curing     explicit curing name (use instead of route matching)
  --queue      explicit queue name (required with --curing)
  --source     source label for route matching (default: "cli")
  --kind       hide kind label (required for route matching)
  --settle     settle delay after all queues go empty (default: 1s)
  --timeout    total wall-clock deadline, 0 = none (default: 0)

Arguments:
  file         optional path to input file; reads stdin if omitted

Exit codes:
  0  queues drained, no DLQ items
  1  runtime error, DLQ items found, or timeout
  2  usage error (missing required flags, unresolved route)
`

// workflowLLMClient, when non-nil, overrides buildHTTPClient in RunWorkflowRun.
// Set in tests to inject a MockLLM without an HTTP server.
var workflowLLMClient session.LLMClient

// RunWorkflow implements the "leather workflow" subcommand dispatcher.
func RunWorkflow(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, workflowUsage)
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "run":
		return RunWorkflowRun(rest, stdout, stderr)
	case "--help", "-h":
		fmt.Fprint(stdout, workflowUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "leather workflow: unknown sub-command %q\n\n", sub)
		fmt.Fprint(stderr, workflowUsage)
		return 2
	}
}

// RunWorkflowRun implements "leather workflow run".
// Returns exit code: 0 success, 1 error, 2 usage error.
func RunWorkflowRun(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("workflow run", stderr)
	config.BindFlags(fs)
	curingName := fs.String("curing", "", "explicit curing name")
	queueName := fs.String("queue", "", "explicit queue name (required with --curing)")
	source := fs.String("source", "cli", "source label for route matching")
	kind := fs.String("kind", "", "hide kind label (required for route matching)")
	settle := fs.Duration("settle", time.Second, "settle delay after all queues go empty")
	timeout := fs.Duration("timeout", 0, "total wall-clock deadline (0 = none)")
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: load config: %v\n", err)
		return 1
	}
	if cfg.TanneryFile == "" {
		fmt.Fprintln(stderr, "leather workflow run: --tannery is required")
		return 2
	}

	tannCfg, err := config.LoadTannery(cfg.TanneryFile)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: load tannery: %v\n", err)
		return 1
	}

	curingDefs, err := curing.LoadDir(tannCfg.CuringDir)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: load curing defs: %v\n", err)
		return 1
	}
	if err := config.ValidateTannery(tannCfg, curingDefs); err != nil {
		fmt.Fprintf(stderr, "leather workflow run: validate tannery: %v\n", err)
		return 1
	}

	// Resolve routing: explicit flags take priority, then auto-route.
	resolvedCuring := *curingName
	resolvedQueue := *queueName
	if resolvedCuring == "" {
		if *kind == "" {
			fmt.Fprintln(stderr, "leather workflow run: --kind is required when --curing is not set")
			return 2
		}
		router := curing.NewRouter(tannCfg.Routes)
		route, ok := router.Match(*source, *kind)
		if !ok {
			fmt.Fprintf(stderr, "leather workflow run: no route found for source=%q kind=%q\n", *source, *kind)
			return 2
		}
		resolvedCuring = route.Curing
		resolvedQueue = route.Queue
	} else if resolvedQueue == "" {
		fmt.Fprintln(stderr, "leather workflow run: --queue is required when --curing is set")
		return 2
	}

	// ctx controls both the supervisor poll loops and the quiescence wait.
	// Worker goroutines use context.Background() internally so cancelling ctx
	// stops the Run() poll loops without aborting in-flight LLM calls.
	ctx := context.Background()
	var cancel context.CancelFunc
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Acquire single-process lock on the hide directory (same lock as serve).
	lockPath := filepath.Join(tannCfg.HideDir, "leather.lock")
	lf, err := acquireProcessLock(lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: %v\n", err)
		return 1
	}
	defer releaseProcessLock(lf)

	hideStore := hide.NewStore(tannCfg.HideDir)
	artStore := artifact.NewStore(tannCfg.ArtifactDir)
	qmgr := queue.NewManager(filepath.Join(cfg.StateDir, "queues"))
	log := buildLogger(cfg, stderr)

	agents, agentErrs := agent.LoadDir(cfg.AgentDir)
	for _, e := range agentErrs {
		log.Warn("workflow run: agent load error", "error", e)
	}
	agentsMap := agentsByName(agents)

	var llmClient session.LLMClient
	if workflowLLMClient != nil {
		llmClient = workflowLLMClient
	} else {
		llmClient = buildHTTPClient(cfg)
	}

	toolReg, err := tool.Load(cfg.ToolDir)
	if err != nil {
		log.Warn("workflow run: failed to load tool registry", "dir", cfg.ToolDir, "error", err)
		toolReg = tool.NewRegistry()
	}

	// Load and start MCP servers (same fallback path as RunServe).
	mcpServersFile := cfg.MCPServersFile
	if mcpServersFile == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			mcpServersFile = filepath.Join(home, ".leather", "mcp-servers.yaml")
		}
	}
	mcpConfigs, mcpLoadErr := mcp.LoadServers(mcpServersFile)
	if mcpLoadErr != nil {
		log.Warn("workflow run: failed to load MCP servers", "file", mcpServersFile, "error", mcpLoadErr)
		mcpConfigs = nil
	}
	mcpReg := mcp.NewRegistry(mcpConfigs, log)
	if len(mcpConfigs) > 0 {
		startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if startErr := mcpReg.StartAll(startCtx); startErr != nil {
			log.Warn("workflow run: some MCP servers failed to start", "error", startErr)
		}
		startCancel()
		defer mcpReg.StopAll()
	}

	runnerDeps := &curing.RunnerDeps{
		Client:        llmClient,
		ToolReg:       toolReg,
		Log:           log,
		MaxToolRounds: cfg.MaxToolRounds,
		QueueMgr:      qmgr,
		MCPReg:        mcpReg,
		Budget:        resolveTokenBudget(cfg, model.Agent{}),
	}

	sup, err := curing.NewSupervisor(
		curingDefs, agentsMap, tannCfg.Queues,
		hideStore, artStore, runnerDeps,
		qmgr, curing.NewRouter(tannCfg.Routes), log,
	)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: new supervisor: %v\n", err)
		return 1
	}

	log.Info("workflow run: starting supervisor", "curings", len(curingDefs), "agents", len(agentsMap))
	sup.Start(ctx)

	// Start the API server on cfg.APIAddr, mirroring leather serve.
	// This exposes /intake so MCP tool subprocesses can POST hides+items
	// into this process's qmgr via HTTP, avoiding cross-process file I/O.
	// LEATHER_INTAKE_URL is set in the environment so shell-mcp inherits it.
	td := &tanneryDeps{
		hideStore:    hideStore,
		artStore:     artStore,
		curingDefs:   curingDefs,
		curingRouter: curing.NewRouter(tannCfg.Routes),
		tannCfg:      tannCfg,
	}
	deps := apiDeps{
		cfg:      cfg,
		log:      log,
		queueMgr: qmgr,
		tannery:  td,
	}
	apiSrv := startAPIServer(deps)
	defer apiSrv.Shutdown(context.Background()) //nolint:errcheck
	intakeURL := "http://" + cfg.APIAddr + "/intake"
	os.Setenv("LEATHER_INTAKE_URL", intakeURL)
	log.Info("workflow run: intake URL", "url", intakeURL)

	// Read content from file argument or stdin.
	var content []byte
	if rest := fs.Args(); len(rest) > 0 {
		content, err = os.ReadFile(rest[0])
		if err != nil {
			fmt.Fprintf(stderr, "leather workflow run: read file: %v\n", err)
			return 1
		}
	} else {
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "leather workflow run: read stdin: %v\n", err)
			return 1
		}
	}

	// Write the initial hide and enqueue the first item.
	entry, err := hideStore.Put(*kind, *source, content, nil)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: write hide: %v\n", err)
		return 1
	}
	drainStart := time.Now()

	item := model.QueueItem{
		ID:         queue.GenerateItemID(),
		CuringName: resolvedCuring,
		HideID:     entry.ID,
		HideKind:   entry.Kind,
		EnqueuedAt: time.Now().Unix(),
		Payload:    map[string]any{"hide_id": entry.ID, "curing": resolvedCuring},
	}
	if err := qmgr.Enqueue(resolvedQueue, item); err != nil {
		fmt.Fprintf(stderr, "leather workflow run: enqueue: %v\n", err)
		return 1
	}

	// Wait for all queues to reach simultaneous quiescence.
	if err := waitForQuiescence(ctx, qmgr, tannCfg.Queues, *settle); err != nil {
		cancel()
		sup.Drain()
		if ctx.Err() != nil {
			fmt.Fprintf(stderr, "leather workflow run: timed out after %v\n", *timeout)
		} else {
			fmt.Fprintf(stderr, "leather workflow run: %v\n", err)
		}
		return 1
	}

	// Stop the Run() poll loops; Drain() waits for in-flight goroutines.
	cancel()
	sup.Drain()

	if dlqs := checkDLQs(qmgr, tannCfg.Queues); len(dlqs) > 0 {
		fmt.Fprintf(stderr, "leather workflow run: items in DLQ: %s\n", strings.Join(dlqs, ", "))
		return 1
	}

	arts, err := artStore.ListByCuring(resolvedCuring)
	if err != nil {
		fmt.Fprintf(stderr, "leather workflow run: list artifacts: %v\n", err)
		return 1
	}
	var runArts []model.Artifact
	for _, a := range arts {
		if a.CreatedAt >= drainStart.Unix() {
			runArts = append(runArts, a)
		}
	}

	fmt.Fprintf(stdout, "hide_id    %s\n", entry.ID)
	fmt.Fprintf(stdout, "curing     %s\n", resolvedCuring)
	fmt.Fprintf(stdout, "queue      %s\n", resolvedQueue)
	fmt.Fprintf(stdout, "artifacts  %d\n", len(runArts))
	for _, a := range runArts {
		fmt.Fprintf(stdout, "\n--- artifact %s ---\n", a.ID)
		body := a.Content
		const maxBytes = 4096
		if len(body) > maxBytes {
			body = body[:maxBytes] + "\n[truncated]"
		}
		fmt.Fprintln(stdout, body)
	}
	return 0
}

// waitForQuiescence polls all tracked queues until they are simultaneously
// empty, then waits settle before confirming. Returns ctx.Err() on timeout.
func waitForQuiescence(
	ctx context.Context,
	qmgr *queue.Manager,
	queues map[string]model.QueueConcurrencyConfig,
	settle time.Duration,
) error {
	allEmpty := func() bool {
		for name := range queues {
			if qmgr.Depth(name) > 0 {
				return false
			}
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !allEmpty() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}

		// Queues are empty — wait the settling delay for any fan-out enqueues.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(settle):
		}

		if allEmpty() {
			return nil
		}
		// Queues refilled during settle; loop to keep polling.
	}
}

// checkDLQs returns a formatted list of non-empty DLQ queues.
func checkDLQs(qmgr *queue.Manager, queues map[string]model.QueueConcurrencyConfig) []string {
	var result []string
	for name := range queues {
		dlq := name + "-dlq"
		if d := qmgr.Depth(dlq); d > 0 {
			result = append(result, fmt.Sprintf("%s (%d)", dlq, d))
		}
	}
	return result
}
