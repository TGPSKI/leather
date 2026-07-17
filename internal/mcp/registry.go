package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/TGPSKI/leather/internal/logging"
	"github.com/TGPSKI/leather/internal/model"
)

// Registry manages a set of named MCP server clients.
// Servers are started lazily via StartAll and stopped via StopAll.
type Registry struct {
	configs []model.MCPServerConfig
	// mu guards clients. It also serializes Recreate so concurrent callers that
	// hit a poisoned client don't each spawn a competing restart of the same
	// server process.
	mu      sync.Mutex
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
		r.mu.Lock()
		r.clients[cfg.Name] = c
		r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[name]
	return c, ok
}

// Recreate closes the poisoned client for name and starts a fresh one, swapping
// it into the registry. stale is the client the caller found poisoned; if the
// current client is no longer stale (another caller already recreated it), the
// existing fresh client is returned without restarting. Serialized via r.mu so
// concurrent callers do not race multiple restarts of the same server.
func (r *Registry) Recreate(ctx context.Context, name string, stale *Client) (*Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cur, ok := r.clients[name]
	if !ok {
		return nil, fmt.Errorf("mcp/Recreate: server %q not found in MCP registry", name)
	}
	// Another caller already recreated the client for this server; reuse it.
	if cur != stale {
		return cur, nil
	}

	var cfg model.MCPServerConfig
	found := false
	for _, c := range r.configs {
		if c.Name == name {
			cfg = c
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("mcp/Recreate: no config for server %q", name)
	}

	_ = cur.Close()
	fresh, err := Start(ctx, cfg)
	if err != nil {
		// Leave the poisoned client in place; a later call will retry recreation.
		return nil, fmt.Errorf("mcp/Recreate %s: %w", name, err)
	}
	r.clients[name] = fresh
	if r.log != nil {
		r.log.Warn("mcp/Recreate: recreated poisoned client", "name", name)
	}
	return fresh, nil
}

// StopAll terminates all running server processes. Errors are best-effort.
func (r *Registry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.clients {
		_ = c.Close()
	}
}

// ToolSchema returns the JSON Schema for toolName as reported by the named MCP server.
// Returns nil when the server is not running or the schema was not fetched.
func (r *Registry) ToolSchema(server, tool string) map[string]any {
	r.mu.Lock()
	c, ok := r.clients[server]
	r.mu.Unlock()
	if ok {
		return c.ToolSchema(tool)
	}
	return nil
}
