# schema

> Flat-schema validation for config, agent, skill, worker, and MCP YAML surfaces.

## Responsibility

`schema` provides lightweight validation for the flat scalar and list portions
of leather definition files. It catches missing required fields, bad enums,
invalid durations, malformed cron expressions, and similar shape errors before
the deeper package-specific parsers run. Nested blocks remain owned by the
package that actually interprets them; `schema` is intentionally shallow.

## Public API

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `FieldType` | `type FieldType uint8` | Enum describing the expected scalar format for one YAML field. |
| `Field` | `type Field struct { ... }` | Validation rule for one field, including required/list/enum/range metadata. |
| `Schema` | `type Schema map[string]Field` | Flat schema definition for one file type. |
| `Violation` | `type Violation struct { Field, Message string }` | One validation failure. |
| `ValidateFlat` | `func ValidateFlat(vals map[string]string, lists map[string][]string, s Schema) []Violation` | Apply a flat schema to already-parsed scalar and list maps. |
| `ValidateAgentFrontmatter` | `func ValidateAgentFrontmatter(src string) []Violation` | Validate the YAML block from `*.agent.md` front matter. |
| `ValidateLifecycleYAML` | `func ValidateLifecycleYAML(src string) []Violation` | Validate flat lifecycle fields. |
| `ValidateConfigYAML` | `func ValidateConfigYAML(src string) []Violation` | Validate flat `config.yaml` fields. |
| `ValidateSkillYAML` | `func ValidateSkillYAML(src string) []Violation` | Validate top-level skill metadata. |
| `ValidateWorkerYAML` | `func ValidateWorkerYAML(src string) []Violation` | Validate worker scalar fields. |
| `ValidateMCPServersYAML` | `func ValidateMCPServersYAML(src string) []Violation` | Validate each `servers:` item in `mcp-servers.yaml`. |

## Internal Design

`schema` delegates YAML tokenization to `config.ParseBlock`, then validates the
resulting scalar and list maps with `ValidateFlat`. That keeps the package
stdlib-only while avoiding a second YAML parser implementation.

Each file type has a static schema in `defs.go`. These schemas intentionally
cover only the flat surface. Nested sections such as `cache:`, `output:`,
`hooks:`, `parameters:`, and worker output blocks are left to the owning
package parsers.

`TypeCron` validation uses a lazily compiled regex guarded by `sync.Once` and
accepts five- or six-field cron expressions plus the special `once` value.
`ValidateMCPServersYAML` uses a dedicated splitter because `mcp-servers.yaml`
is a list of flat objects rather than a single flat map.

## Dependencies

| Package | Why |
|---|---|
| `internal/config` | Reuses `ParseBlock` for flat YAML extraction. |

## Data Flow

```mermaid
flowchart LR
    SRC[YAML source] --> PB[config.ParseBlock]
    PB --> VF[ValidateFlat]
    VF --> VIOLS[[]Violation]
    VF --> OK[no violations]
```

## Test Surface

`internal/schema/schema_test.go` covers required-field checks, list-required
fields, scalar type validation, integer bounds, cron handling, wrapper
validators for agent/lifecycle/skill/worker documents, and optional-field
absence semantics.

## Related Docs

- [docs/modules/config.md](config.md)
- [docs/modules/agent.md](agent.md)
- [docs/modules/cli.md](cli.md)
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md)