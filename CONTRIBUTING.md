# Contributing to leather

Thank you for your interest in contributing to `leather`! This project follows
strict architectural constraints to remain a lightweight, auditable agent
orchestrator.

## Architectural Constraints

- **Standard Library Only**: `go.mod` must have zero external dependencies.
- **Fail-Closed**: Configuration and validation errors must trigger immediate
  explicit failures rather than degrading behavior.
- **Single Binary**: The product is delivered as the `leather` binary.

## Development Setup

Requires Go 1.22+.

```bash
# Build the main binary
make build

# Build the companion shell tool
make build-shell-mcp

# Run all tests
make test

# Run lint checks
make lint
```

## Pull Request Process

1. **Fork and Branch**: Create a feature branch from `main`.
2. **Tests**: Ensure `make test` and `make lint` pass. All new code must be
   covered by tests.
3. **Stdlib Check**: Verify no new dependencies were added to `go.mod`.
4. **Documentation**: Update the `docs/` or `examples/` if the change adds
   new user-facing features.
5. **Atomic Commits**: Use descriptive, atomic commit messages.

## Community

By participating in this project, you agree to abide by our
Code of Conduct.

## Questions?

If you have questions about the implementation or architecture, please refer
to `docs/ARCHITECTURE.md` and `docs/GLOSSARY.md` before opening an issue.