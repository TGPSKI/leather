package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// RunOnce loads and executes a single agent definition, then exits.
// Usage: leather run [flags] <path-to-agent.agent.md>
func RunOnce(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("run", stderr)
	config.BindFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "leather run: requires a path to an *.agent.md file")
		return 2
	}

	path, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "leather run: %v\n", err)
		return 1
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather run: %v\n", err)
		return 1
	}
	// Auto-disable pretty mode when stdout is not an interactive terminal.
	// Prevents ANSI escape codes from appearing in redirected log files.
	if cfg.Pretty && !isTTY(stdout) {
		cfg.Pretty = false
	}

	logDest := io.Writer(stderr)
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(stderr, "leather run: open log file %s: %v\n", cfg.LogFile, err)
			return 1
		}
		defer f.Close()
		if cfg.Pretty {
			logDest = f
		} else {
			logDest = io.MultiWriter(stderr, f)
		}
	} else if cfg.Pretty {
		logDest = io.Discard
		fmt.Fprintln(stdout, "leather: structured logs discarded (--pretty mode). Pass --log-file <path> to capture.")
	}
	log := buildLogger(cfg, logDest)

	a, err := agent.LoadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "leather run: %v\n", err)
		return 1
	}
	if err := agent.ApplyLifecycleFile(path, &a); err != nil {
		fmt.Fprintf(stderr, "leather run: lifecycle: %v\n", err)
		return 1
	}
	a = resolveAgent(cfg, a)
	if a.Model == "" {
		fmt.Fprintf(stderr, "leather run: model must be set in the lifecycle file or config.yaml\n")
		return 1
	}
	if a.Name == "" {
		fmt.Fprintln(stderr, "leather run: agent name is required (set 'name:' in front matter)")
		return 1
	}

	toolReg, err := tool.Load(cfg.ToolDir)
	if err != nil {
		log.Warn("failed to load tool registry", "dir", cfg.ToolDir, "error", err)
		toolReg = tool.NewRegistry()
	}

	mcpServersFile := cfg.MCPServersFile
	if mcpServersFile == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			mcpServersFile = filepath.Join(home, ".leather", "mcp-servers.yaml")
		}
	}
	mcpConfigs, mcpLoadErr := mcp.LoadServers(mcpServersFile)
	if mcpLoadErr != nil {
		log.Warn("failed to load MCP servers", "file", mcpServersFile, "error", mcpLoadErr)
		mcpConfigs = nil
	}
	mcpReg := mcp.NewRegistry(mcpConfigs, log)
	if len(mcpConfigs) > 0 {
		startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if startErr := mcpReg.StartAll(startCtx); startErr != nil {
			log.Warn("some MCP servers failed to start", "error", startErr)
		}
		startCancel()
		log.Info("MCP servers started", "count", len(mcpConfigs))
		defer mcpReg.StopAll()
	}

	budget := resolveTokenBudget(cfg, a)
	if cfg.Pretty {
		fmt.Fprintf(stdout, "%s  %s  %s  %s\n\n",
			bold("leather run"),
			dim("agent="+a.Name),
			dim("model="+filepath.Base(a.Model)),
			dim("endpoint="+cfg.LLMEndpoint))
	}
	notifiers, notifyErrs := notify.BuildMap(cfg.NotifyBackends)
	for _, e := range notifyErrs {
		log.Warn("notify backend init failed", "error", e)
	}

	var toolLimiter *tool.HostLimiter
	if len(cfg.ToolRateLimits) > 0 {
		var limErr error
		toolLimiter, limErr = tool.NewHostLimiter(cfg.ToolRateLimits)
		if limErr != nil {
			log.Warn("tool rate limits: invalid config, rate limiting disabled", "error", limErr)
		}
	}

	r := &runner.Runner{
		Client:        session.NewHTTPClient(cfg.LLMEndpoint, cfg.LLMAPIKey, cfg.LLMTimeout),
		Registry:      toolReg,
		MCPRegistry:   mcpReg,
		Log:           log,
		MaxToolRounds: cfg.MaxToolRounds,
		Notifiers:     notifiers,
		ToolLimiter:   toolLimiter,
	}

	// Cancel the run context on SIGINT/SIGTERM/SIGHUP so in-flight LLM calls
	// are aborted and deferred cleanup (mcpReg.StopAll, log file close) runs
	// before the process exits.
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
	go func() {
		for {
			select {
			case <-sig:
				runCancel()
				return
			case <-runCtx.Done():
				return
			}
		}
	}()

	loops := cfg.Loop
	if loops < 1 {
		loops = 1
	}

	// Build the base parameter map from skill definitions (keys with defaults).
	// Empty-string values mean "prompt user". Re-collected each iteration when loops > 1.
	// Lifecycle parameters seed the map first; skill parameters only fill in absent keys.
	var baseVars map[string]string
	if len(a.Parameters) > 0 || len(a.Skills) > 0 {
		baseVars = make(map[string]string)
		// Lifecycle parameters take precedence — add them first.
		for k, v := range a.Parameters {
			baseVars[k] = v
		}
		// Skill parameters fill in any keys not already declared by the lifecycle.
		for _, sk := range toolReg.GetSkills(a.Skills) {
			for k, v := range sk.Parameters {
				if _, already := baseVars[k]; !already {
					baseVars[k] = v
				}
			}
		}
	}

	reader := bufio.NewReader(os.Stdin)

	for i := 0; i < loops; i++ {
		if runCtx.Err() != nil {
			break
		}
		if cfg.Pretty && loops > 1 {
			fmt.Fprintf(stdout, "\n%s\n", dim(fmt.Sprintf("── loop %d/%d ──", i+1, loops)))
		}

		configuredRunner, prettyPrinter := configurePrettyRunner(*r, stdout, cfg)

		// Re-prompt for parameters on every iteration.
		if len(baseVars) > 0 {
			vars := make(map[string]string, len(baseVars))
			for k, v := range baseVars {
				vars[k] = v
			}
			for k, v := range vars {
				if v == "" {
					fmt.Fprintf(stderr, "%s: ", k)
					line, _ := reader.ReadString('\n')
					vars[k] = strings.TrimSpace(line)
				}
			}
			configuredRunner.Vars = vars
		}

		rec, err := configuredRunner.Run(runCtx, a, budget)
		if err != nil {
			if prettyPrinter != nil {
				prettyPrinter.Stop()
			}
			if runCtx.Err() != nil {
				// Context cancelled by signal — clean shutdown, no error message.
				return 0
			}
			fmt.Fprintf(stderr, "leather run: %v\n", err)
			return 1
		}
		if prettyPrinter != nil {
			prettyPrinter.Render(a, rec)
		}
		if cfg.PersistRuns {
			dir := cfg.RunHistoryDir
			if dir == "" {
				dir = filepath.Join(cfg.StateDir, "runs")
			}
			if err := persistRunRecord(dir, rec, cfg.RunMaxBytes); err != nil {
				log.Warn("failed to persist run record", "agent", a.Name, "error", err)
			}
		}
	}
	return 0
}
