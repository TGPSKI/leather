package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/schema"
)

// RunValidate parses and validates all agent, skill, and worker definition files.
// Exits 0 if all files are valid, 1 if any have errors, 2 on usage errors.
//
// Output format:
//
//	ok:     <name>  (<path>)           — file parsed and schema-clean
//	error:  <file>: <message>          — parse or semantic error
//	schema: <file>:  field "<name>": <msg>  — schema violation
func RunValidate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("validate", stderr)
	config.BindFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather validate: %v\n", err)
		return 1
	}

	exitCode := 0
	totalFiles := 0

	// --- Phase 0: config file ---

	if cfg.ConfigFile != "" {
		if data, err := os.ReadFile(cfg.ConfigFile); err == nil {
			viols := schema.ValidateConfigYAML(string(data))
			name := filepath.Base(cfg.ConfigFile)
			if len(viols) > 0 {
				for _, v := range viols {
					fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", name, v.Field, v.Message)
				}
				exitCode = 1
			} else {
				fmt.Fprintf(stdout, "ok:     %s  (%s)\n", name, cfg.ConfigFile)
			}
			totalFiles++
		}
	}

	// --- Phase 1: agent.md and lifecycle.yaml files ---

	agents, loadErrs := agent.LoadDir(cfg.AgentDir)
	for _, e := range loadErrs {
		fmt.Fprintf(stderr, "error:  %v\n", e)
		exitCode = 1
	}
	for _, a := range agents {
		resolved := resolveAgent(cfg, a)
		if resolved.Model == "" {
			fmt.Fprintf(stderr, "error:  agent %q: field \"model\": required (set model: in lifecycle or config.yaml)\n", a.Name)
			exitCode = 1
			continue
		}
		fmt.Fprintf(stdout, "ok:     %s  (%s)\n", a.Name, a.SourcePath)
	}
	totalFiles += len(agents)

	// Schema-validate *.agent.md frontmatter and *.lifecycle.yaml files.
	if entries, err := os.ReadDir(cfg.AgentDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(cfg.AgentDir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				// T5.12: surface read failures instead of silently skipping.
				fmt.Fprintf(stderr, "error:  %s: read failed: %v\n", e.Name(), err)
				exitCode = 1
				continue
			}
			src := string(data)
			var viols []schema.Violation
			switch {
			case strings.HasSuffix(e.Name(), ".agent.md"):
				yamlBlock := extractFrontMatterYAML(src)
				if yamlBlock != "" {
					viols = schema.ValidateAgentFrontmatter(yamlBlock)
				}
			case strings.HasSuffix(e.Name(), ".lifecycle.yaml"):
				viols = schema.ValidateLifecycleYAML(src)
			}
			for _, v := range viols {
				fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", e.Name(), v.Field, v.Message)
				exitCode = 1
			}
		}
	}

	// --- Phase 2: *.skill.yaml files ---

	if cfg.ToolDir != "" {
		if entries, err := os.ReadDir(cfg.ToolDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".skill.yaml") {
					continue
				}
				path := filepath.Join(cfg.ToolDir, e.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					fmt.Fprintf(stderr, "error:  %s: %v\n", e.Name(), err)
					exitCode = 1
					continue
				}
				viols := schema.ValidateSkillYAML(string(data))
				if len(viols) > 0 {
					for _, v := range viols {
						fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", e.Name(), v.Field, v.Message)
					}
					exitCode = 1
				} else {
					fmt.Fprintf(stdout, "ok:     %s  (%s)\n", e.Name(), path)
				}
				totalFiles++
			}
		}
		// T5.11: also validate *.toolset.yaml files in the same directory.
		if entries, err := os.ReadDir(cfg.ToolDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".toolset.yaml") {
					continue
				}
				path := filepath.Join(cfg.ToolDir, e.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					fmt.Fprintf(stderr, "error:  %s: %v\n", e.Name(), err)
					exitCode = 1
					continue
				}
				viols := schema.ValidateToolsetYAML(string(data))
				if len(viols) > 0 {
					for _, v := range viols {
						fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", e.Name(), v.Field, v.Message)
					}
					exitCode = 1
				} else {
					fmt.Fprintf(stdout, "ok:     %s  (%s)\n", e.Name(), path)
				}
				totalFiles++
			}
		}
	}

	// --- Phase 3: *.worker.yaml files ---

	if cfg.WorkerDir != "" {
		if entries, err := os.ReadDir(cfg.WorkerDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".worker.yaml") {
					continue
				}
				path := filepath.Join(cfg.WorkerDir, e.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					fmt.Fprintf(stderr, "error:  %s: %v\n", e.Name(), err)
					exitCode = 1
					continue
				}
				viols := schema.ValidateWorkerYAML(string(data))
				if len(viols) > 0 {
					for _, v := range viols {
						fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", e.Name(), v.Field, v.Message)
					}
					exitCode = 1
				} else {
					fmt.Fprintf(stdout, "ok:     %s  (%s)\n", e.Name(), path)
				}
				totalFiles++
			}
		}
	}

	// --- Phase 4: mcp-servers.yaml ---

	mcpFile := cfg.MCPServersFile
	if mcpFile == "" {
		if home, err := os.UserHomeDir(); err == nil {
			mcpFile = filepath.Join(home, ".leather", "mcp-servers.yaml")
		}
	}
	if mcpFile != "" {
		if data, err := os.ReadFile(mcpFile); err == nil {
			viols := schema.ValidateMCPServersYAML(string(data))
			name := filepath.Base(mcpFile)
			if len(viols) > 0 {
				for _, v := range viols {
					fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", name, v.Field, v.Message)
				}
				exitCode = 1
			} else {
				fmt.Fprintf(stdout, "ok:     %s  (%s)\n", name, mcpFile)
			}
			totalFiles++
		}
	}

	// --- Phase 5: tannery.yaml + *.curing.yaml files ---

	if cfg.TanneryFile != "" {
		if data, err := os.ReadFile(cfg.TanneryFile); err == nil {
			viols := schema.ValidateTanneryYAML(string(data))
			name := filepath.Base(cfg.TanneryFile)
			if len(viols) > 0 {
				for _, v := range viols {
					fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", name, v.Field, v.Message)
				}
				exitCode = 1
			} else {
				fmt.Fprintf(stdout, "ok:     %s  (%s)\n", name, cfg.TanneryFile)
			}
			totalFiles++

			// Validate *.curing.yaml files under the tannery's curing_dir.
			tan, _ := config.LoadTannery(cfg.TanneryFile)
			if tan.CuringDir != "" {
				if entries, err := os.ReadDir(tan.CuringDir); err == nil {
					for _, e := range entries {
						if e.IsDir() || !strings.HasSuffix(e.Name(), ".curing.yaml") {
							continue
						}
						path := filepath.Join(tan.CuringDir, e.Name())
						data, err := os.ReadFile(path)
						if err != nil {
							fmt.Fprintf(stderr, "error:  %s: %v\n", e.Name(), err)
							exitCode = 1
							continue
						}
						viols := schema.ValidateCuringYAML(string(data))
						if len(viols) > 0 {
							for _, v := range viols {
								fmt.Fprintf(stderr, "schema: %s:  field %q: %s\n", e.Name(), v.Field, v.Message)
							}
							exitCode = 1
						} else {
							fmt.Fprintf(stdout, "ok:     %s  (%s)\n", e.Name(), path)
						}
						totalFiles++
					}
				}
			}
		}
	}

	// --- Summary ---

	if exitCode == 0 {
		fmt.Fprintf(stdout, "\nvalidated %d file(s) — no errors\n", totalFiles)
	} else {
		fmt.Fprintf(stdout, "\n%d file(s) checked\n", totalFiles)
	}
	return exitCode
}

// extractFrontMatterYAML returns the YAML content between the leading --- delimiters of src.
// Returns empty string when no valid front matter block is found.
func extractFrontMatterYAML(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	const open = "---\n"
	if !strings.HasPrefix(src, open) {
		return ""
	}
	rest := src[len(open):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
