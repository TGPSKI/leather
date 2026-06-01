// Package mcp implements an MCP (Model Context Protocol) client over the
// stdio transport using JSON-RPC 2.0. It manages server process lifecycle
// and provides a Registry for named MCP server clients.
package mcp

import (
	"fmt"
	"os"
	"strings"

	"github.com/tgpski/leather/internal/model"
)

// LoadServers reads an mcp-servers.yaml file and returns the parsed server
// configs. Returns an empty slice (not an error) if path does not exist.
func LoadServers(path string) ([]model.MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mcp/LoadServers: read %s: %w", path, err)
	}
	return parseServersYAML(string(data))
}

// parseServersYAML parses an mcp-servers.yaml document into server configs.
//
// Expected format:
//
//	servers:
//	  - name: skeptic
//	    command: skeptic mcp
//	    transport: stdio
//
// The top-level "servers:" key holds a YAML list of server objects.
func parseServersYAML(src string) ([]model.MCPServerConfig, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	var configs []model.MCPServerConfig
	var cur *model.MCPServerConfig
	var curEnv *model.MCPEnvVar
	inServers := false
	inEnv := false
	serverBase := -1 // indent of server list items; detected from first "- " seen

	flushEnv := func() {
		if curEnv != nil && cur != nil {
			cur.Env = append(cur.Env, *curEnv)
			curEnv = nil
		}
	}
	flushServer := func() {
		flushEnv()
		if cur != nil {
			configs = append(configs, *cur)
			cur = nil
		}
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		ind := countIndent(rawLine)

		// Top-level "servers:" key — only when the line is NOT a list item.
		if ind == 0 && !strings.HasPrefix(trimmed, "- ") {
			key, _ := splitKV(trimmed)
			if key == "servers" {
				inServers = true
			} else {
				inServers = false
			}
			continue
		}

		if !inServers {
			continue
		}

		if strings.HasPrefix(trimmed, "- ") {
			if serverBase == -1 {
				serverBase = ind // detect indent of first server item
			}

			if ind == serverBase {
				// New server list item.
				flushServer()
				inEnv = false
				cur = &model.MCPServerConfig{Transport: "stdio"}
				rest := strings.TrimPrefix(trimmed, "- ")
				if rest != "" {
					key, val := splitKV(rest)
					applyServerField(cur, key, val)
				}
				continue
			}

			// Env var list item inside env: block.
			if inEnv && ind == serverBase+4 {
				flushEnv()
				curEnv = &model.MCPEnvVar{}
				rest := strings.TrimPrefix(trimmed, "- ")
				if rest != "" {
					key, val := splitKV(rest)
					applyEnvField(curEnv, key, val)
				}
				continue
			}
		}

		// Env var continuation fields.
		if inEnv && curEnv != nil && ind >= serverBase+6 {
			key, val := splitKV(trimmed)
			applyEnvField(curEnv, key, val)
			continue
		}

		// Server continuation fields.
		if cur != nil {
			key, val := splitKV(trimmed)
			if key == "env" && val == "" {
				inEnv = true
				continue
			}
			inEnv = false
			applyServerField(cur, key, val)
		}
	}
	flushServer()

	for i, cfg := range configs {
		if cfg.Name == "" {
			return nil, fmt.Errorf("mcp/LoadServers: server at index %d missing required field: name", i)
		}
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp/LoadServers: server %q missing required field: command", cfg.Name)
		}
	}
	return configs, nil
}

func applyServerField(cfg *model.MCPServerConfig, key, val string) {
	switch key {
	case "name":
		cfg.Name = unquote(val)
	case "command":
		cfg.Command = unquote(val)
	case "transport":
		cfg.Transport = unquote(val)
	}
}

func applyEnvField(e *model.MCPEnvVar, key, val string) {
	switch key {
	case "name":
		e.Name = unquote(val)
	case "pass":
		e.Pass = unquote(val)
	case "env":
		e.Env = unquote(val)
	}
}

// countIndent returns the number of leading spaces in s.
func countIndent(s string) int {
	for i, c := range s {
		if c != ' ' {
			return i
		}
	}
	return len(s)
}

// splitKV splits a "key: value" string into key and value parts.
func splitKV(s string) (string, string) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return strings.TrimSpace(s), ""
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:])
}

// unquote removes surrounding single or double quotes from a YAML scalar value.
func unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
