# logging

> Structured, component-scoped logging wrapping `log/slog`.

## Responsibility

`logging` provides a thin `Logger` wrapper around `log/slog` that attaches a
component name to every log entry. It supports text and JSON output formats,
per-component level control, and an injectable writer for test isolation.
All leather packages use `logging.Logger` rather than calling `slog` directly.

## Public API

### Types

| Symbol | Description |
|---|---|
| `Logger` | Component-scoped structured logger backed by `*slog.Logger`. |

### Functions

| Symbol | Signature | Description |
|---|---|---|
| `New` | `(component string, level model.LogLevel) *Logger` | Returns a text-format Logger writing to stderr. |
| `NewJSON` | `(component string, level model.LogLevel) *Logger` | Returns a JSON-format Logger writing to stderr. |
| `NewWithWriter` | `(component string, level model.LogLevel, w io.Writer, jsonFormat bool) *Logger` | Returns a Logger writing to w. Used for tests and alternate sinks (e.g. log files). |
| `(*Logger).Debug` | `(msg string, args ...any)` | Emits a debug-level log entry. |
| `(*Logger).Info` | `(msg string, args ...any)` | Emits an info-level log entry. |
| `(*Logger).Warn` | `(msg string, args ...any)` | Emits a warn-level log entry. |
| `(*Logger).Error` | `(msg string, args ...any)` | Emits an error-level log entry. |
| `(*Logger).With` | `(args ...any) *Logger` | Returns a new Logger with additional key-value attributes pre-attached. |

## Internal Design

`Logger` wraps `*slog.Logger` and adds a `component` attribute to every
record at construction time. The log level is resolved from `model.LogLevel`
to `slog.Level` once at construction — no per-call level parsing.

`NewWithWriter` is the primary constructor used in production code so that
`io.Writer` can be injected. `New` and `NewJSON` are convenience wrappers
that pass `os.Stderr`.

Log entries follow the pattern:
```
time=... level=INFO component=cli msg="starting serve" ...key=value
```

## Dependencies

- `internal/model` — for `model.LogLevel`

## Data Flow

```mermaid
flowchart LR
    caller["any internal package"] -->|"logging.New(component, level)"| Logger
    Logger -->|slog| output["stderr / file / test buffer"]
```

## Test Surface

- `logging_test.go` — tests Logger construction (text and JSON formats),
  level filtering, and `With` attribute chaining using `NewWithWriter` with
  a `bytes.Buffer`.

## Related Docs

- [docs/modules/model.md](model.md) — defines `LogLevel`
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md) — dependency graph
