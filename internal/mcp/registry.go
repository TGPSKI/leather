package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
)

// Registry manages a set of named MCP server clients.
// Servers are started lazily via StartAll and stopped via StopAll.
type Registry struct {
	configs []model.MCPServerConfig
	clients map[string]*Client
	log     *logging.Logger
}

// NewRegistry creates a Registry from a slice of server configs.
// Servers are not started until StartAll is called. log may be nil.
func NewRegistry(configs []model.MCPServerConfig, log *logging.Logger) *Registry {
	return &Registry{
		configs: configs,
		clients: make(map[string]*Client, len(configs)),
		log:     log,
	}
}

// StartAll attempts to start every configured server. Per-server failures are
// logged and skipped; StartAll returns an aggregate error only when zero
// servers started successfully. Returns nil when no servers are configured.
func (r *Registry) StartAll(ctx context.Context) error {
	var (
		started int
		errs    []string
	)
	for _, cfg := range r.configs {
		c, err := Start(ctx, cfg)
		if err != nil {
			if r.log != nil {
				r.log.Warn("mcp/StartAll: server failed to start", "name", cfg.Name, "error", err)
			}
			errs = append(errs, fmt.Sprintf("%s: %v", cfg.Name, err))
			continue
		}
		r.clients[cfg.Name] = c
		started++
	}
	if started == 0 && len(errs) > 0 {
		return fmt.Errorf("mcp/StartAll: no servers started: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Get returns the Client for the named server. Returns false when the server
// is not configured or has not been started yet.
func (r *Registry) Get(name string) (*Client, bool) {
	c, ok := r.clients[name]
	return c, ok
}

// StopAll terminates all running server processes. Errors are best-effort.
func (r *Registry) StopAll() {
	for _, c := range r.clients {
		_ = c.Close()
	}
}

// ToolSchema returns the JSON Schema for toolName as reported by the named MCP server.
// Returns nil when the server is not running or the schema was not fetched.
func (r *Registry) ToolSchema(server, tool string) map[string]any {
	if c, ok := r.clients[server]; ok {
		return c.ToolSchema(tool)
	}
	return nil
}
