// Package leather is a stdlib-only Go binary that runs declarative agents
// locally — scheduled jobs, one-shot runs, webhook-driven workflows, tool
// calling, and auditable outputs — without a Python stack, hosted control
// plane, or external dependency pile.
//
// This package is intentionally empty. The runtime lives in the leather
// binary at github.com/tgpski/leather/cmd/leather and the shell-manifest
// MCP companion at github.com/tgpski/leather/cmd/shell-mcp. Implementation
// packages live under internal/ and are not part of the public Go API.
//
// # Install
//
//	go install github.com/tgpski/leather/cmd/leather@latest
//	go install github.com/tgpski/leather/cmd/shell-mcp@latest
//
// # Pre-built binaries
//
// Tagged releases publish linux/darwin × amd64/arm64 tarballs at
// https://github.com/tgpski/leather/releases.
//
// # Documentation
//
// See the project README and docs/ tree on GitHub for full usage, the
// agent definition format, the curing/tannery workflow model, and the
// HTTP API surface: https://github.com/tgpski/leather.
package leather
