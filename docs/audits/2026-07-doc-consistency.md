# leather — documentation consistency audit

Scope: `docs/GUIDE.md`, `schemas/*.schema.yaml`, `internal/schema/defs.go`, the runtime (`internal/model`, `internal/config`, `internal/cli`), `cmd/shell-mcp`, and `examples/`. Every finding below is backed by file:line and mapped to an agent-ready issue.

## The shape of the drift

There are **four representations** of the same contracts, and they disagree:

1. **Runtime / `internal/schema/defs.go`** — what `leather validate` and the loaders actually enforce. *Source of truth.*
2. **`schemas/*.schema.yaml`** — hand-maintained JSON schemas for editor (yaml.schemas) diagnostics. *Drifts from (1).*
3. **`docs/GUIDE.md`** — prose + examples. *Drifts from (1) and from the shell-mcp binary.*
4. **`cmd/shell-mcp`** — the shell-tools.json format. *The GUIDE describes a different, nonexistent format for it.*

The highest-leverage fixes make (2) generated from (1) and make the GUIDE’s code blocks CI-tested against the real binaries — after that, most individual doc bugs cannot recur.

Drift concentrates in three clusters: **(a) tools/shell-mcp/tannery prose** (stale shell-tools format, naming, phantom flags), **(b) the hand-maintained JSON schemas** vs `defs.go`, and **(c) the HTTP API surface docs** (observability/replay/UI) describing endpoints and a /metrics format the server doesn't serve. The serve/config/architecture prose and the shipped UI code are largely accurate.

## Verified clean (spot-checked this audit)

Drift is concentrated in the tools/shell-mcp/tannery prose and the hand-maintained JSON schemas. These corners were checked and found consistent with the code:

- `.subagents/AGENTS-SERVE.md` — the `--flag` / `LEATHER_*` / default table matches code (`--default-toolsets`, `--completion-reserve`=1024, `--llm-timeout`, `--llm-endpoint`, etc.).
- `docs/ARCHITECTURE.md` — every `internal/<pkg>` path referenced exists.
- `docs/integrations/` (rpi-hailo.md) — no stale format/flag references.
- Config defaults in `schemas/config-1.schema.yaml` spot-checked against `internal/config/defaults.go` (completion_reserve, summarize_threshold, llm_timeout) — aligned.

## Findings → issues

| # | Priority | Sev | Area | Finding | Issue |
|---|----------|-----|------|---------|-------|
| 1 | P1 | high | GUIDE.md ↔ shell-mcp binary | shell-tools.json section documents an `exec.*` format shell-mcp cannot parse | `issues/01-guide-shell-tools-format.md` |
| 2 | P1 | high | schemas/*.schema.yaml ↔ internal/schema/defs.go | JSON schemas drift from authoritative `internal/schema/defs.go` (toolsets, persist_runs_detail, max_repeats) | `issues/02-schema-codegen-drift.md` |
| 3 | P2 | med | GUIDE.md ↔ skill-1.schema.yaml ↔ examples | tool-name casing says kebab-case; schema pattern and all examples use snake_case | `issues/03-tool-name-case.md` |
| 4 | P2 | med | docs/ (missing) | no central environment-variable reference (LEATHER_DEMO_MODE, git-tool vars, SHELL_MCP_CONFIG, model vars) | `issues/04-env-var-reference.md` |
| 5 | P2 | med | GUIDE.md config reference | config wiring keys (mcp_servers_file, llm_endpoint, llm_api_key) are undocumented | `issues/05-document-config-wiring-keys.md` |
| 6 | P3 | low-med | GUIDE.md command + tannery reference | command reference + tannery gaps (`workflow run`, queue_pattern, webhook-secret coupling) | `issues/06-guide-command-and-tannery-gaps.md` |
| 7 | P2 | med | schemas/ + internal/cli/cmd_validate.go | add a shell-tools.json schema and cover it in `leather validate` | `issues/07-shell-tools-schema-and-validate.md` |
| 8 | P2 | med | internal/session + serve/workflow | offline LLM fixture (record/replay) so examples self-test without a live model | `issues/08-offline-llm-fixture.md` |
| 9 | P3 | low | examples/ tooling | scaffolder + documented convention for adding an example | `issues/09-example-scaffold.md` |
| 10 | P3 | low-med | .subagents/AGENTS-SHELL-MCP.md + AGENTS-OPERATIONS.md ↔ code | phantom flags/env documented that don't exist (`--no-shell`/`SHELL_MCP_NO_SHELL`, `LEATHER_LOG_DIR`) | `issues/10-phantom-flags-and-env.md` |
| 11 | P2 | med | docs/ + .subagents/ + GUIDE | three overlapping prose doc sets with no canonical hierarchy — the root cause of the drift | `issues/11-doc-source-of-truth-hierarchy.md` |
| 12 | P2 | med | .subagents/AGENTS-{OBSERVABILITY,REPLAY,UI,PERFORMANCE,OPERATIONS}.md ↔ internal/cli/cmd_serve.go | HTTP API docs describe endpoints/format the server doesn't serve (/metrics format, /status/*, /runs, /replay/*) | `issues/12-http-api-doc-drift.md` |

## Detail

### 1. docs(GUIDE): shell-tools.json section documents an `exec.*` format shell-mcp cannot parse
*P1 · high · effort M · GUIDE.md ↔ shell-mcp binary*

Two docs describe shell-tools.json with an `exec.argv` / `exec.shell` object. The shipped `shell-mcp` has no such fields — it reads `command` / `args` / `required` / `timeout_seconds` / `output_cap_bytes`. Worse, both docs ALSO contain the correct command/args form elsewhere: this is a half-finished migration (code + examples + docs/TEMPLATES.md + docs/modules/tool.md were updated; these two prose docs were not). Anyone/any agent copying the stale blocks writes a file shell-mcp can't use.

**Evidence**
- docs/GUIDE.md:332-373 and 791-815 — `exec.argv`/`exec.shell` tools + the § “`exec.argv` vs `exec.shell`” table (355-360). The SAME file also uses the correct `command`/`args` form elsewhere → intra-file contradiction.
- .subagents/AGENTS-SHELL-MCP.md:79-118 — `exec.argv` examples PLUS a spec table (line 118) declaring `exec` a REQUIRED field, `exactly one of argv:|shell:`. Also internally contains the command/args form.
- cmd/shell-mcp/main.go:37-60 — `toolDef` = command/args/required/patterns/defaults/optional/output_cap_bytes/timeout_seconds. No exec/argv/shell; `grep -rn 'json:\"exec\"|Argv' --include=*.go` → zero matches.
- Canonical-correct surfaces already exist: all 13 examples/*/shell-tools.json, docs/TEMPLATES.md, docs/modules/tool.md, and CHANGELOG.md:100-104 (which added `required` to the command/args form).

**Fix**
- Rewrite BOTH docs/GUIDE.md (defs + vs-table + worked example ~791-815) AND .subagents/AGENTS-SHELL-MCP.md (examples + the exec-required spec table) to the real schema: `command`, `args` (`-c … -- {{arg}}` idiom), top-level `required: […]`, `timeout_seconds`, `output_cap_bytes`, optional `patterns`/`defaults`/`optional`.
- Remove the `exec.argv` vs `exec.shell` guidance entirely; replace with the `command:bash + args:[-c, script, --, {{a}}]` pattern.
- Add a CI doctest that extracts every ```json block under a shell-tools heading in ALL docs and asserts it json.Unmarshal(DisallowUnknownFields)s into `toolDef`, so no doc can carry the stale form again.

### 2. schemas: JSON schemas drift from authoritative `internal/schema/defs.go` (toolsets, persist_runs_detail, max_repeats)
*P1 · high · effort L · schemas/*.schema.yaml ↔ internal/schema/defs.go*

`leather validate` uses the flat validators in internal/schema/defs.go (the runtime source of truth). The `schemas/*.schema.yaml` files are a *separate*, hand-maintained representation for editor (yaml.schemas) diagnostics. They have drifted: valid, runtime-supported fields are missing from the JSON schemas and get flagged as errors in editors. This is the root cause behind several one-off doc bugs.

**Evidence**
- cmd_validate.go:95,43,122,… — validate calls `schema.ValidateAgentFrontmatter/ValidateConfigYAML/ValidateSkillYAML` (defs.go), NOT the JSON schemas.
- toolsets: live — defs.go:46 (`"toolsets": {IsList:true}`), model.go:205-400 (Toolset, Toolsets, TurnToolsets). Missing from schemas/agent-1.schema.yaml (which is additionalProperties:false), even though schemas/toolset-1.schema.yaml advertises “referenced by agents via toolsets: [name]”.
- persist_runs_detail: live — defs.go:111 (enum none|tools), config.go:370, model.go:607-667. Missing from schemas/config-1.schema.yaml.
- max_repeats: live — model.go:90, tool/registry.go:636-644 (per-tool, min -1). Documented in GUIDE:514-527. Missing from schemas/skill-1.schema.yaml.
- Schemas even disagree with each OTHER: schemas/lifecycle-1.schema.yaml lists toolsets/skills/tool_rounds, but schemas/agent-1.schema.yaml (also an agent definition, for *.agent.md frontmatter) omits toolsets — same field, two agent-def schemas, different answers.

**Fix**
- Pick one source of truth. Preferred: generate schemas/*.schema.yaml from internal/schema/defs.go (+ the enum/pattern metadata) via `go generate`, so the JSON schemas can never diverge.
- If codegen is out of scope now, as an interim: add the three missing fields to their JSON schemas (agent-1: toolsets; config-1: persist_runs_detail + persist_runs_tool_cap; skill-1: per-tool max_repeats), and audit the full defs.go field set against each schema for any other omissions.
- Add a CI test asserting parity: every field in defs.go field-tables appears in the corresponding schemas/*.schema.yaml and vice-versa. Fail the build on drift.

### 3. docs(GUIDE): tool-name casing says kebab-case; schema pattern and all examples use snake_case
*P2 · med · effort S · GUIDE.md ↔ skill-1.schema.yaml ↔ examples*

The GUIDE tells authors to name tools in kebab-case. The skill JSON-schema tool-name pattern is `^[a-z][a-z0-9_]*$` (snake_case; hyphens rejected) and every example uses snake_case. A reader following the GUIDE picks a name the schema flags and that mismatches the examples.

**Evidence**
- docs/GUIDE.md:378-381 — “Tool names use `kebab-case`.”
- schemas/skill-1.schema.yaml — tools[].name pattern `^[a-z][a-z0-9_]*$` (underscore, no hyphen). Note: skill *name* pattern DOES allow hyphens (`^[a-z][a-z0-9-]*$`) — the asymmetry is the trap.
- examples/10-ci-gate/shell-tools.json — `get_pr_files`, `post_pr_comment`, `add_pr_label` (snake_case), matching the skill mcp.tool references.

**Fix**
- Decide the canonical convention (snake_case matches schema + examples + OpenAI tool-call norms — recommend snake_case) and correct GUIDE:378-381.
- Call out the asymmetry explicitly: skill NAME may contain hyphens; TOOL name (and mcp.tool reference) must be snake_case.
- Optionally: if runtime does not actually enforce the charset (registry.go only checks duplicates), decide whether to enforce it in defs.go so validate catches it, or relax the JSON-schema pattern — but pick one and make docs + schema + runtime agree.

### 4. docs: no central environment-variable reference (LEATHER_DEMO_MODE, git-tool vars, SHELL_MCP_CONFIG, model vars)
*P2 · med · effort S · docs/ (missing)*

There is no single env-var reference. The repo-wide `LEATHER_DEMO_MODE` dry/live convention lives only in a shell helper; several example-critical vars are documented nowhere; and the model/endpoint vars are scattered. Authors of new tools/examples must reverse-engineer each one from source.

**Evidence**
- LEATHER_DEMO_MODE (default `dry`, `=live` for side effects): examples/scripts/preflight.sh:20-25 (`lth_demo_mode`) and every side-effect tool (`if [ "${LEATHER_DEMO_MODE:-dry}" = live ]`). `grep -rln LEATHER_DEMO_MODE docs/` → 0 hits.
- LEATHER_GIT_SIGNING_KEY and LEATHER_GIT_DIFF_LINES: used by example 13 (scripts + Makefile + README) — 0 hits in the central doc set.
- SHELL_MCP_CONFIG: the shell-mcp config-resolution env (cmd/shell-mcp/main.go:8-10,318) — not in GUIDE.
- LEATHER_MODEL / LEATHER_LLM_ENDPOINT: referenced across code + examples but with no single documented home.

**Fix**
- Add a docs/CONVENTIONS.md (or GUIDE section) with an env-var table: name, default, scope (build/serve/tool/example), and effect. Include LEATHER_DEMO_MODE with the `if [ "${LEATHER_DEMO_MODE:-dry}" = live ]` idiom and the `dry-mode: would …` message convention.
- Cover LEATHER_MODEL, LEATHER_LLM_ENDPOINT, LEATHER_GIT_SIGNING_KEY, LEATHER_GIT_DIFF_LINES, SHELL_MCP_CONFIG, GITHUB_WEBHOOK_SECRET, GH_TOKEN, LEATHER_INTAKE_URL.
- Cross-link from the shell-tools section and examples/README.

### 5. docs(GUIDE): config wiring keys (mcp_servers_file, llm_endpoint, llm_api_key) are undocumented
*P2 · med · effort S · GUIDE.md config reference*

`mcp_servers_file` is how `serve`/`workflow run` locate MCP servers (and thus tools) from config, yet it appears nowhere in the GUIDE. `llm_endpoint`/`llm_api_key` are likewise absent. Authors can wire agents and tools correctly and still get a tool-less run because the config never points at the servers.

**Evidence**
- schemas/config-1.schema.yaml — defines mcp_servers_file, llm_endpoint, llm_api_key.
- cmd_workflow.go:211-217 — `mcpServersFile := cfg.MCPServersFile` (falls back to ~/.leather/mcp-servers.yaml); this is the load path for `workflow run`.
- `grep -c mcp_servers_file docs/GUIDE.md` → 0; llm_endpoint → 0; llm_api_key → 0.

**Fix**
- Document mcp_servers_file in the config reference: purpose, default fallback (~/.leather/mcp-servers.yaml), and that `workflow run` reads it from config (not a flag).
- Document llm_endpoint / llm_api_key and their env overrides (LEATHER_LLM_ENDPOINT / model via LEATHER_MODEL).
- Add a one-line note in the workflow/serve sections: ‘tools come from mcp_servers_file; without it, agents run tool-less.’

### 6. docs(GUIDE): command reference + tannery gaps (`workflow run`, queue_pattern, webhook-secret coupling)
*P3 · low-med · effort S · GUIDE.md command + tannery reference*

Several real, load-bearing behaviors are missing or mis-stated in the GUIDE reference tables, each of which forces trial-and-error.

**Evidence**
- docs/GUIDE.md:1265 — command table lists `leather workflow`; the actual subcommand is `leather workflow run` (internal/cli/cmd_workflow.go:72 `case "run"`). Examples use `leather workflow run` (examples/13-git-workflow-commit/scripts/run-demo.sh:103).
- queue_pattern is a real tannery route feature (schemas/tannery-1.schema.yaml; used in examples/10-ci-gate/tannery.yaml) but `grep -c queue_pattern docs/GUIDE.md` → 0.
- `workflow run` calls LoadTannery, which fails if a declared webhook `secret: {{env:GITHUB_WEBHOOK_SECRET}}` is unset — even though workflow-run never serves the webhook. Undocumented coupling (observed: `workflow run` aborts with ‘webhook \"github\" secret: environment variable \"GITHUB_WEBHOOK_SECRET\" is unset’).

**Fix**
- Fix the command table: `leather workflow run` (and note it reads one hide from stdin, drains queues to quiescence; flags --config/--tannery/--curing/--queue/--kind/--source/--settle/--timeout).
- Document queue_pattern (per-event single-use input queues, `{{hide_id}}` templating) in the tannery routes section.
- Document that `workflow run` still validates tannery webhook secrets; either set the env var or omit the webhook block for pure workflow-run configs. (Optionally file a follow-up to lazily validate webhook secrets only when the listener starts.)

### 7. feat(validate): add a shell-tools.json schema and cover it in `leather validate`
*P2 · med · effort M · schemas/ + internal/cli/cmd_validate.go*

shell-tools.json is the most format-fiddly, hand-edited artifact, and it is the only one with no schema and no `leather validate` coverage. A malformed tools file passes `leather validate` and only fails at runtime inside shell-mcp.

**Evidence**
- cmd_validate.go phases cover: config, *.agent.md/*.lifecycle.yaml, *.skill.yaml, *.toolset.yaml, *.worker.yaml, mcp-servers.yaml, tannery.yaml, *.curing.yaml — shell-tools.json is absent.
- schemas/ has agent/config/curing/lifecycle/mcp-server/skill/tannery/toolset/worker/common — no shell-tools schema.
- Format authority is cmd/shell-mcp/main.go:37-60 (`toolDef`).

**Fix**
- Add schemas/shell-tools-1.schema.yaml mirroring `toolDef` (name; command; args; required[]; patterns{}; defaults{}; optional; timeout_seconds; output_cap_bytes), additionalProperties:false.
- Wire a validate phase: discover *shell-tools*.json referenced by mcp-servers.yaml (or a `--shell-tools` flag) and validate against the schema; report violations in the same format as other phases.
- Add the schema to the VS Code yaml.schemas guidance and to any doctest from the GUIDE shell-tools fix.

### 8. feat(serve/workflow): offline LLM fixture (record/replay) so examples self-test without a live model
*P2 · med · effort L · internal/session + serve/workflow*

MockLLM exists but is only reachable from `test-agent`; serve and `workflow run` always construct an HTTP client against LLM_ENDPOINT. There is no supported way to run a full pipeline end-to-end without a live model, so example smoke tests and the ability to prove wiring in CI require a running model (or a hand-rolled scripted endpoint).

**Evidence**
- internal/session/mock_llm.go — MockLLM (with ToolCallSequence) exists but is test-only.
- cmd_serve.go:1315 / buildHTTPClient:2701-2703 — serve/workflow build `session.NewHTTPClient(cfg.LLMEndpoint…)` unconditionally; cmd_test_agent.go:96 is the only place `model = mock` is wired.
- Consequence: validating the analyze→match→label pipeline end-to-end required standing up a scripted OpenAI-compatible server by hand.

**Fix**
- Add a fixture mode to serve/workflow: e.g. `--llm-fixture responses.jsonl` (or model: fixture:<file>) that replays per-call responses, including tool_calls, via the existing LLMClient interface.
- Support record mode (`--llm-record out.jsonl`) that captures a live run for later replay.
- Convert one multi-agent example (e.g. 06 or 10) to a fixture-backed `make <NN>-smoke` that runs in CI with no model.

### 9. dx(examples): scaffolder + documented convention for adding an example
*P3 · low · effort M · examples/ tooling*

Adding an example is tribal knowledge: pick the next free NN index, hand-register `NN:` and `NN-live:` targets in examples/Makefile, copy scripts/pretty.sh, and source ../scripts/preflight.sh. Nothing documents this, so each new example is copy-three-siblings-and-guess.

**Evidence**
- examples/Makefile:13 — the number list and NN/NN-live targets are all hand-maintained per example (e.g. lines 164-170 for 10).
- Each example ships its own scripts/pretty.sh copy and sources examples/scripts/preflight.sh; the pairing is undocumented.
- No examples/CONTRIBUTING or `make new-example`.

**Fix**
- Add `make new-example NAME=<slug>` that allocates the next index, scaffolds the standard tree (config/tannery/agents/curings/tools/scripts), copies pretty.sh, wires the NN/NN-live Makefile targets, and drops a README skeleton.
- Or, minimally: add examples/README section documenting the required files, the NN/NN-live target pattern, and the pretty.sh/preflight.sh contract.

### 10. docs: phantom flags/env documented that don't exist (`--no-shell`/`SHELL_MCP_NO_SHELL`, `LEATHER_LOG_DIR`)
*P3 · low-med · effort S · .subagents/AGENTS-SHELL-MCP.md + AGENTS-OPERATIONS.md ↔ code*

Two module docs describe flags/env the binaries don't read. The shell-mcp one is security-relevant (it implies a hardening mode that isn't there); the ops one sends readers to set a variable that has no effect.

**Evidence**
- .subagents/AGENTS-SHELL-MCP.md:43,62 — `--no-shell (argv-only) mode`, `SHELL_MCP_NO_SHELL=1`, “only argv-form entries are exposed.” cmd/shell-mcp/main.go has only `SHELL_MCP_CONFIG` (lines 8-10,318); no `no-shell` flag/env exists.
- .subagents/AGENTS-OPERATIONS.md — “leather respects `LEATHER_STATE_DIR`, `LEATHER_LOG_DIR`…”. STATE_DIR is real (config.go:50,148 `envString("STATE_DIR", …)`); there is NO `LOG_DIR` flag or env — logging uses `--log-file`/`LEATHER_LOG_FILE`. `LEATHER_LOG_DIR` is a phantom.
- .subagents/AGENTS-OBSERVABILITY.md:146 — documents a `--metrics-public` flag (auth bypass for /metrics). No such flag exists: `grep -rn metrics-public internal/` → 0.

**Fix**
- shell-mcp: decide truth — remove the `--no-shell`/`SHELL_MCP_NO_SHELL`/argv-only claims from AGENTS-SHELL-MCP.md, or implement them in cmd/shell-mcp. If removing, note that every manifest entry is exposed as-is (untrusted manifests must be vetted upstream).
- ops: replace `LEATHER_LOG_DIR` with the real `LEATHER_LOG_FILE` (and `LEATHER_STATE_DIR`) in AGENTS-OPERATIONS.md; grep the doc set for any other `LEATHER_*_DIR`/flag not present in config.go.
- observability: remove `--metrics-public` (or implement it); today /metrics follows the same API-auth rules as other endpoints.

### 11. docs(architecture): three overlapping prose doc sets with no canonical hierarchy — the root cause of the drift
*P2 · med · effort M · docs/ + .subagents/ + GUIDE*

The same contracts are described in THREE independent prose corpora with no declared source-of-truth: docs/GUIDE.md (tutorial), docs/modules/*.md (27 hand-written module docs), and .subagents/AGENTS-*.md (18 module agent docs). Facts like the shell-tools format and `toolsets` are stated 3–4 times and drift independently — which is WHY this audit exists. Plus the JSON schemas (4th) and the code (5th).

**Evidence**
- Prose sets: docs/GUIDE.md; docs/modules/ (27 files: agent.md tool.md mcp.md config.md …, hand-written — no codegen marker); .subagents/ (18 AGENTS-*.md).
- Concrete divergence on ONE fact (shell-tools format): docs/modules/tool.md + docs/TEMPLATES.md correct; docs/GUIDE.md + .subagents/AGENTS-SHELL-MCP.md carry the stale exec.* form (see shell-tools-format issue).
- docs/modules/ is linked from README.md but not from GUIDE; no page states which corpus is authoritative for a given topic.
- Deferred-vocabulary leakage: docs/GLOSSARY.md:77-98 defines a “target vocabulary” marked **Deferred to v0.2** (Operation, IntakeWorker, CuringWorkItem, CuringRun). These have no code referent (0 hits each) yet leak into prose — e.g. AGENTS.md:98 presents `Operation` as live vocabulary; a reader grepping the code for it finds nothing.

**Fix**
- Declare and document a hierarchy at the top of docs/ (e.g. CONTRIBUTING or docs/README): code > generated schemas > exactly ONE prose home per topic; GUIDE is tutorial and links out rather than restating field-level contracts.
- For each overlapping topic (tools/skills/toolsets, shell-mcp, tannery, config), pick one canonical doc and replace the duplicated field-level content in the others with a cross-link.
- Reconcile GLOSSARY target vocabulary: either mark leaked terms (Operation, etc.) clearly as forthcoming everywhere they appear, or defer their use until the v0.2 code rename lands. A reader should never meet a term with no code referent presented as current.
- Evaluate generating docs/modules/*.md from godoc/gomarkdoc so they can't drift from code; if kept hand-written, add them to any doc-doctest coverage.

### 12. docs: HTTP API docs describe endpoints/format the server doesn't serve (/metrics format, /status/*, /runs, /replay/*)
*P2 · med · effort M · .subagents/AGENTS-{OBSERVABILITY,REPLAY,UI,PERFORMANCE,OPERATIONS}.md ↔ internal/cli/cmd_serve.go*

The observability/replay/UI module docs describe a larger, more RESTful HTTP API than `serve` registers. Monitoring and UI integrations built from these docs break. Notably the shipped UI CODE is correct — it is the docs that drift — so this is a docs-only fix (plus deciding which 'planned' endpoints to keep).

**Evidence**
- /metrics format: cmd_serve.go:1946-1964 writes JSON via `httpx.WriteJSON` (`metricsResponse`: agents{}, tool_retry_total, tool_backoff_total, tool_rate_limit_wait_total, outbound_dlq_depth). AGENTS-OBSERVABILITY.md:146,155,163 says “Prometheus-style text exposition”, “text/plain”, “text-only; no JSON variant”, “Stable metric names (Prometheus exposition)” — a Prometheus scrape gets JSON and fails.
- /status sub-routes: AGENTS-OBSERVABILITY.md:144-145 documents `GET /status/agents` and `GET /status/queues`. Only `/status` is registered (cmd_serve.go:1887). Per-agent data lives in /metrics (agents map); per-queue in /queues.
- /runs: AGENTS-UI.md:110-111 (api.js `listRuns()→/runs`, `getRun()→/runs/{id}`), AGENTS-PERFORMANCE.md:46, AGENTS-OPERATIONS.md:296 all cite `/runs`. It is registered nowhere; the shipped UI (ui/js/leather-api.js:19,21) actually calls `/jobs` and `/history`. (AGENTS-UI also names the file `api.js`; it ships as `leather-api.js`.)
- /replay/*: AGENTS-REPLAY.md:111-117 tables 7 endpoints (GET /replay/runs, GET /replay/runs/{id}, POST /replay/run/{id}, POST /replay/run/{id}/from/{turn}, GET /replay/diff, GET /replay/export/{id}, DELETE /replay/runs/{id}). Only `/replay/control` is registered (cmd_serve.go:2026). Some are hinted ‘planned’/‘when API auth lands’ but the table reads as current.

**Fix**
- AGENTS-OBSERVABILITY: correct /metrics to JSON (document the metricsResponse fields); drop ‘Prometheus text / no JSON variant’ — or file a separate feature to add a real Prometheus exposition. Remove /status/agents and /status/queues (repoint to /metrics + /queues).
- Replace all `/runs` endpoint references with `/jobs` and `/history` in AGENTS-UI/PERFORMANCE/OPERATIONS; fix the `api.js` → `leather-api.js` filename.
- AGENTS-REPLAY: mark every endpoint beyond `/replay/control` as **Planned** (or implement them); make the table's status column explicit.
- Add a doc-test: every HTTP path documented as current (fenced or in an endpoint table) must be registered in the serve mux, else tagged Planned.

