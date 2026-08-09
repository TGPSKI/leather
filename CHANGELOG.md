# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.3] — 2026-08-09

Three defects of one kind, all found by operating the reference tannery: a
configuration or prompt mistake that produced no error, only a run that looked
successful. Two are fixed by failing closed at load; the third replaces a prompt
sentence with a mechanism.

### Added

- **`require_tool:` — a turn that must act cannot end on text**
  ([#84](https://github.com/TGPSKI/leather/issues/84)).
  A turn whose job is to dispatch, file, or publish ends the moment the model
  produces text, and producing text instead of acting is the failure mode:
  observed, a fan-out agent composed five dispatch blocks as its reply, called
  nothing, and recorded success while zero downstream work was queued. The only
  defense was a sentence in the prompt, re-breakable by anyone who rewrote it.
  A turn header may now name required tools; a text response arriving with none
  of them called is refused, the model is told to make the call, and the turn
  continues. If the turn's tool rounds run out first the run fails, naming the
  turn and the tool. Any one named tool satisfies the requirement, and a call
  that fails counts — the requirement is that the model attempted the act. A
  requirement no tool in that turn's scope can satisfy is rejected by
  `leather validate` and again at run start, before the first LLM call.

### Fixed

- **An unrecognized turn header key is an error, not prompt text**
  ([#82](https://github.com/TGPSKI/leather/issues/82)).
  The turn parser recognized four keys and silently treated any other
  declaration-shaped line as the first line of the prompt. `tool_rounds: 12` set
  no budget and shipped to the model as prose; a typo'd `toolset:` left the turn
  inheriting the previous turn's scope, which is the case where a turn written
  to drop a write tool keeps it. Both produced a clean run and no warning. A
  header-position line naming an unrecognized key now fails the load, naming the
  file, the turn, and the five recognized keys. The header also ends at the
  first blank line, so a prompt that genuinely opens with `word:` has somewhere
  to put it. Measured against every agent under `examples/` and every agent
  block in `docs/`: no existing turn changes meaning.
- **Lifecycle `output: type: queue` produced items no curing could consume**
  ([#83](https://github.com/TGPSKI/leather/issues/83)).
  The route enqueued an item carrying only a payload. A curing's first act is to
  load its item's hide, so every item this route produced hit "hide missing" and
  dead-lettered on first touch — there was no input for which it worked, and the
  schema accepted it, `leather validate` passed it, and the failure surfaced only
  as a growing DLQ. The route now writes the response to the hide store and
  enqueues a reference to it, matching what curing-to-curing chaining already
  did. Where no hide store is available the route warns and enqueues nothing
  rather than producing an item guaranteed to dead-letter.

### Documentation

- `docs/GUIDE.md` gains the full turn-header key table, the `require_tool`
  contract, a shape-selection table for picking between cron, curing, fan-out
  and fan-in, five authoring anti-patterns drawn from live failures, and two
  corrections: `hide_types` does not filter chained artifacts (a chained hide
  carries the upstream curing's name as its kind), and `page_size_bytes` smaller
  than the document silently drops the tail an end-of-document guard depends on.

## [0.5.2] — 2026-08-09

### Added

- **[`docs/REFERENCE-TANNERY.md`](docs/REFERENCE-TANNERY.md) — a study of a
  running leather deployment.** Every numbered example runs from a fresh clone
  with one `make` target, so none of them shows a whole system.
  [`TGPSKI/catnip/tannery`](https://github.com/TGPSKI/catnip/tree/2033b0d9c8d5739995e2aec4087130babcce699d/tannery)
  — six agents, four queues, a 41-minute job, unattended — is read in place and
  pinned at `2033b0d` rather than vendored; it needs `catnip`, a populated
  store and a served model, so a copy under `examples/` could not run.
  Covers stages joined by queues rather than clock arithmetic, per-turn scopes
  that replace rather than extend, tool-sourced verdicts, and a three-layer
  timeout stack. Carries run records from 2026-08-07 against a 35B MoE (~3B
  active, NVFP4, one GPU): the analyst stage went from a monolith at 237s
  median that never completed a recorded cycle to 43s median, 26 runs for 26,
  and no fix in that day's failure ledger required a bigger model. Also carries
  the A/B in which a blind frontier control — single-shot, 64k tokens, 7.5
  minutes — produced a better document than the ~750k-token pipeline, and the
  split of that gap into tool surface and synthesis ceiling.

Every fix here closes a silent failure: a configuration or routing mistake that
produced no error, only a run that looked successful. A tool reference that
resolved to nothing left an agent whose prompt still named the tools, so it
narrated calls it never made. An ingest that could not route stored a hide and
exited 0. A dead scheduler kept advertising its next fire time. leather now
fails closed on all three.

### Fixed

- **A tool reference that resolves to nothing is now fatal, not skipped**
  ([#71](https://github.com/TGPSKI/leather/issues/71),
  [#72](https://github.com/TGPSKI/leather/issues/72)).
  A failed tool-registry load was logged at WARN and the run continued with no
  tools registered at all. Because skill headers put tool names and purposes
  into the system prompt, the agent had been told in detail about tools the
  tool-calling API never received — and the most probable completion is to
  role-play them. The measured example: 72 prompt tokens on a turn declaring 14
  tools, followed by 2208 completion tokens of fabricated `[TOOL CALL]` /
  `[RESPONSE]` pairs with invented field names and a made-up hash. A scheduled
  agent doing this at 05:07 writes a confident, entirely invented artifact, and
  the only signal is one WARN line.
  `leather run`, `leather serve`, and `leather workflow run` now exit non-zero
  when the registry fails to load, and a run fails before the first LLM call
  when an agent — or any of its per-turn scopes — names a skill, toolset, or
  tool the registry did not load. The message names the near miss, because a
  `*.skill.yaml` listed under `toolsets:` is one edit from correct and reads as
  correct.
- **`leather validate` loads the registry the runtime loads**
  ([#71](https://github.com/TGPSKI/leather/issues/71)).
  Two skills in one `tool_dir` declaring the same tool name broke the whole
  registry while every file stayed individually well-formed, so per-file
  validation reported no errors on a configuration that could not execute a
  single tool. Validate now performs a whole-registry load and resolves each
  agent's scope against it, catching duplicate tool names, toolsets naming
  tools nobody defines, dangling `skills:`/`toolsets:` references, and
  `type: mcp` tools whose `mcp.server` is not configured in `mcp-servers.yaml`.
- **Flow-style `mcp:` and `http:` mappings parse**
  ([#74](https://github.com/TGPSKI/leather/issues/74)).
  `mcp: { server: shells, tool: report_write }` parsed to an empty server and
  tool. The tool registered, was offered to the model, was selected, and died at
  dispatch with `server "" not found` — as a tool *result* the model could
  narrate around, after it had already committed to the call. Both YAML mapping
  forms are now accepted wherever a nested map is, and a `type: mcp` tool
  missing either half of its dispatch address fails the load instead of failing
  mid-run. Found in a tannery whose fourteen tools all used flow style: every
  one undispatchable, validate clean.
- **A toolset written as tool definitions is rejected**
  ([#73](https://github.com/TGPSKI/leather/issues/73)).
  `docs/GUIDE.md` showed a toolset containing full tool definitions; the parser
  reads bare names. Following the guide registered a tool literally named
  `name: list-tags`, the toolset resolved to zero real tools, and no error was
  raised. The GUIDE now shows the name-list form and says explicitly that a
  toolset *references* tools some `*.skill.yaml` defines, and a `- ` entry
  containing a colon — which is always this mistake — fails the load naming the
  file and the required form. A toolset that lists no tools is also an error.
- **The two intake surfaces route by one rule**
  ([#75](https://github.com/TGPSKI/leather/issues/75)).
  `queue=` alone enqueued over HTTP but not from the CLI; `curing=` alone
  routed on neither, though the named curing already declares its queue. The
  unrouted cases printed success-shaped output whose only tell was two absent
  lines. Now, on both surfaces: a named curing routes to its declared queue, a
  named queue routes and picks up the curing that consumes it, and naming
  neither lets the route table decide. A parameter set that names a destination
  which cannot route — an unknown curing, a queue the tannery does not declare —
  is an error, and a hide-only ingest says `routing   none` rather than leaving
  it to be inferred. A `queue_pattern` route reaching `/intake` now expands and
  enqueues instead of silently resolving to an empty queue name.
- **CLI ingest refuses to write queues a live serve owns**
  ([#76](https://github.com/TGPSKI/leather/issues/76)).
  A running serve holds each queue it polls in memory and rewrites the backing
  file from that snapshot, so an item appended by `leather ingest` was never
  seen and was overwritten by the serve's next write. The empty-queue-plus-idle-
  worker state looks identical to "nothing was ever enqueued", which is #75's
  failure shape — the two stacked bugs took three reproductions to separate.
  `leather ingest` now probes the state-dir lock before enqueuing and refuses
  with the holder's PID and the supported alternative. Hide-only ingests touch
  no queue and are still allowed.
- **`leather status` reports whether a scheduler is running**
  ([#77](https://github.com/TGPSKI/leather/issues/77)).
  Every field it printed came off disk, so a serve killed an hour ago produced
  output identical to a healthy idle one — including a `next=` it would never
  honour, which for a daily agent stays plausible for a day. Status now probes
  the state-dir lock (acquiring it is proof of absence; releasing it
  immediately, because status must never hold it) and reports
  `serve: running (pid N)` or `serve: not running`. `serve` writes its PID into
  the lock file, which was 0 bytes, so the holder can be named instead of
  pattern-matched. A `next=` already in the past renders as `(stale)`.

### Changed

- **`.subagents/README.md` no longer mirrors the root routing table**
  ([#54](https://github.com/TGPSKI/leather/issues/54)). The mirror had drifted
  from `AGENTS.md`, which is the one failure a duplicated index reliably
  produces. The README now points at the canonical table and keeps only what is
  its own: the per-guide conventions.
- **`14-sig-triage`: `eval/scripts/tui/` is vendored from its canonical public
  upstream, [pane](https://github.com/TGPSKI/pane), now at
  [v0.2.0](https://github.com/TGPSKI/pane/releases/tag/v0.2.0) (`9e80e94`).**
  The copy is byte-identical to upstream and its provenance header now names a
  released tag rather than an untagged mid-history commit, so what the eval
  viewers run is pinned to something a reader can fetch by version; pane's
  `tools/check-vendor.sh` verifies the byte-identity claim rather than merely
  asserting it.
  Reaching the eval viewers: `bar_chart` gains aggregation binning, outlier
  clipping and top-row label collision handling; `interact.py` (with
  `RowCursor`) and `grids.py` join the vendor set; `grids.diverging_bars()`
  draws signed values either side of a zero line, for series where the sign is
  the finding rather than the magnitude. Fixed along the way: a bar carrying
  both a peak marker and a value label had the label written over the marker,
  so a chart whose legend promised "▲ marks this day" drew no ▲ at all. The
  marker is now kept and the value placed beside it, or dropped when there is
  no room.

## [0.5.1] — 2026-08-04

### Added

- **`15-trust-repair` pre-pilot scaffold** — arms, runner, and oracle wiring
  for the trust-boundary-repair workload (decision of record for the second
  workload; full corpus and confirmatory runs remain gated).

### Changed

- **`14-sig-triage`: the ablation findings are now pre-registered and
  replicated.** Six contrasts were frozen at main commit `96cc418` before any
  confirmatory cell ran; the battery executed 11 arms × 3 draws (5× on the one
  contrast that hit the registered boundary trigger), and **five of six**
  survive Holm–Bonferroni at α=0.05. Published figures move accordingly, and
  every movement is against the author's interest: decomposition depth from
  −9.2 to **−5.2**, retrieval payload from +6.4 to **+3.0**, and retrieval
  payload further from RESOLVED to **UNRESOLVED** under Amendment 2. The
  headline span widens to **59.6→81.6%** because a registered S1 draw is now
  the lowest archived cell. New rendered page: `eval/results/CONFIRMATORY.md`.
- **`14-sig-triage`: the confirmatory estimator is now issue-clustered.**
  Amendment 2 replaces pooled McNemar as the primary test. Pooling concatenated
  repeated measurements of the same 250 issues and treated them as independent
  trials, which overstates significance; the primary is now a sign-flip
  permutation test over whole issues. Effect sizes are unchanged — only the
  uncertainty was wrong. Cost: contrast 2 (retrieval payload) drops to
  unresolved. Pooled McNemar remains in the output, labelled descriptive.
- **`14-sig-triage`: "a bad harness scores below no harness at all" is
  retired.** Across replication the fresh-session scheme (61.3%, 5 draws) and
  the bare model (61.9%, 4 draws) are statistically level. The claim is now
  that a bad harness *ties* the bare model while paying for an extra stage and
  250 tool calls — which is the stronger statement anyway.

### Fixed

- **`temperature: 0` is finally reachable from a single setting** (#56) —
  greedy decode required setting temperature in *both* `config.yaml` and every
  agent's frontmatter, because the frontmatter parser defaulted to `0.7` (which
  shadowed config) while `resolveAgent` used `0` as its unset sentinel (which
  made an explicit agent-side `0` read as "unset"). Set-ness is now tracked
  through frontmatter and lifecycle YAML (`TemperatureSet`, mirroring
  `EnabledSet`), the built-in `0.7` lives in exactly one place
  (`config.DefaultTemperature`), and the documented priority *lifecycle >
  frontmatter > config.yaml > built-in* actually holds. The agent-config debug
  line logs `temperature_source=agent|config` so this class of shadowing is
  visible, and `leather doctor` gained a `temperature` row.
- **`leather doctor` no longer fabricates config problems** (#31) — source
  attribution used a `value != default` heuristic, so a value explicitly set
  equal to the built-in default was labelled `default`, and the catch-all
  `config/env/flag` never said which layer won. `config.Load` now records true
  per-key provenance and doctor reports it: `yaml`, `env`, `flag`, or
  `default`. Also fixed en route: `LEATHER_PERSIST_RUNS_DETAIL` and
  `LEATHER_PERSIST_RUNS_TOOL_CAP` were only read at startup seeding, so a YAML
  value silently beat those two env vars, contradicting the documented order.
- **The documented config precedence was wrong, not the code** (#33) —
  `docs/modules/config.md` claimed YAML overrides environment variables; the
  code has always applied env over YAML. The doc now states the real order —
  *flags > env > YAML > built-in* — plus the config-file resolution chain, and
  a test pins env-over-YAML for `llm_endpoint`, the field from the original
  wrong-endpoint incident.
- **Falling back past a project-local `config.yaml` is now loud** (#30) —
  invoking leather bare in a directory containing `config.yaml` silently read
  `~/.leather/config.yaml` instead, which once connected an eval to the wrong
  LLM endpoint. `Load` now prints a one-line stderr notice naming both files
  and the fix (`--config=./config.yaml` or `LEATHER_CONFIG`). Cwd
  auto-discovery remains deliberately absent.
- **`config.Load` tests are hermetic** (#38) — the test suite read whatever
  sat in the developer's real `~/.leather/config.yaml` (the cross-PR #34–#36
  phantom failure). Home-dir resolution is now injectable and the
  default-asserting tests isolate themselves.
- **`14-sig-triage`: the battery's resume guard destroyed three finished
  cells.** Completeness required ≥225 *answered* rows, but S1's mechanism is row
  loss (~220 answered every draw), so a second invocation judged its finished
  cells incomplete and re-ran them over their own archives. Completeness is now
  "250 rows + a manifest"; quality remains `verify-run.sh`'s job. Only S1 was
  affected and its contrast still resolves — see
  `eval/results/INCIDENT-s1-overwrite.md` for the two draw-sets and why the
  correction is explicitly not conservative.

## [0.5.0] — 2026-07-29 "alligator"

### Fixed

- **One slow MCP tool call bricked the whole scheduler until a manual restart** —
  a tool call that consumed the run budget and expired mid-read poisoned the
  shared MCP client (`internal/mcp`), and nothing ever recovered it: the
  registry held one client per server for the process lifetime, so every
  subsequent tool call from every agent short-circuited with
  `client poisoned by prior read timeout` (a 7.5-hour outage in production).
  `Registry` now serializes an on-demand recreate path (`Registry.Recreate`);
  on `mcp.ErrPoisoned` the executor recreates the client once and retries, so
  a poisoned transport self-heals on the next call instead of requiring a
  `leather serve` restart.

- **Failed MCP tool calls were silently treated as successes** — `cmd/shell-mcp`
  returned execution failures as ordinary `error: ...` text with no `isError`
  flag, so `internal/mcp` couldn't distinguish a failed call from a real
  result. The failure survived three layers as plain text and reached the
  runner's dedupe map as a success, permanently blocking retries of a call
  that never actually happened — the root cause of a production incident
  where IP ban deployments were silently dropped for 6+ hours. `shell-mcp`
  now sets `isError: true` on execution failure; `internal/mcp.Call` returns
  a typed `*ToolError`; the executor stops retrying deterministic tool
  errors instead of burning its retry budget; and the runner's dedupe map is
  never populated by a failed call.
- **Duplicate tool calls asserted unverifiable success** — the dedupe map
  stored only a boolean, so a repeated identical call was answered with
  "already completed successfully earlier in this run" and no result
  content, letting the model narrate success it never observed. Duplicate
  calls now replay the cached `model.ToolResult` with a
  `[replay: identical call completed earlier this run; result repeated
  below]` prefix. Zero-argument write tools could also never re-run within
  a run regardless of intervening world-state changes; `ToolDefinition`
  gains a per-tool `max_repeats` (skill yaml), with `-1` disabling dedupe
  entirely.

- **An out-of-scope tool call was run-fatal** — a tool call outside the
  current turn's scope (hallucinated, prompt-injected, or recalled from the
  system prompt after a context clear) was correctly never executed, but the
  rejection failed the entire run, and the temperature-0 retry reproduced it
  deterministically — one recoverable model mistake became a dead work item.
  On the sig-triage eval a 4B model re-called a system-prompt-mentioned tool
  on a `tools: []` turn in 435/471 decide rounds, dead-lettering 214/250
  issues; a 35B never attempted the call on the identical config, which is
  why the failure mode was invisible until a tool-happy small model probed
  it. The executor now answers such calls with a tool-result error
  (`tool X is not available on this turn`), still never executing them; a
  model that keeps calling anyway is bounded by max tool rounds, and refused
  calls never populate the dedupe cache.

- **`shell-mcp` emitted invalid JSON Schema for zero-argument tools** — a
  tool declaring no arguments serialized its schema as `"required": null`
  instead of `"required": []`. Invisible under `tool_choice: auto`, but any
  backend that compiles the schema into a grammar rejects every call with
  `400 Grammar error: Expected array for 'required', got null`. The field
  now always marshals as an array.

- **Editor JSON schemas flagged valid, runtime-supported fields** — the
  hand-maintained `schemas/*.schema.yaml` had drifted from the runtime
  validators in `internal/schema/defs.go`: `toolsets` was missing from
  `agent-1` (while present in `lifecycle-1`), `persist_runs_detail` /
  `persist_runs_tool_cap` from `config-1`, and per-tool `max_repeats` from
  `skill-1`, so editors marked working configs as errors. The missing fields
  are now declared; full codegen from `defs.go` remains an open follow-up.

- **`shell-mcp` per-tool limits were silently ignored** — per-tool
  `timeout_seconds` and `output_cap_bytes` in shell-tools config were parsed
  but never applied, so every tool ran at the global defaults (30 s /
  4000 bytes), silently truncating large results mid-output. Both are now
  enforced, and a call missing a required argument returns a proper tool
  error instead of executing the command with literal `{{placeholder}}`
  argv.

- **`go install` binaries self-identified as `dev (none)`** (#49, #50) — the
  version stamp came only from the Makefile's `-ldflags`, which plain
  `go install github.com/TGPSKI/leather/cmd/{leather,shell-mcp}@<tag>` never
  applies (the installs themselves work as of the v0.4.1 module-path fix;
  verified against the module proxy). Both binaries now fall back to the
  embedded Go build info — the module version for `@tag` installs, the VCS
  revision for plain `go build` from a checkout. `shell-mcp --version` (also
  `-v`/`version`) now prints it too, instead of failing with
  `read config --version: no such file` because the argument was taken for a
  config path.

- **`queue_input:` in agent frontmatter was accepted but silently ignored** —
  both `AgentFrontmatterSchema` and `agent-1.schema.yaml` advertised the
  field ("a paired lifecycle takes precedence"), and `leather validate`
  passed it, but only the lifecycle parser actually read it, so a
  frontmatter-only agent never drained its queue and nothing said why. The
  frontmatter parser now honors it; lifecycle still wins when both are set.

### Added

- **Per-tool-call timeout** — a single run-level timeout previously governed
  every LLM call *and* every tool call, so one slow tool (e.g. an 89-repo
  fetch) would consume the entire run budget and expire the shared MCP
  transport read mid-call. New `tool_timeout` config (global, default `600s`;
  env `LEATHER_TOOL_TIMEOUT`; flag `--tool-timeout`), overridable per agent via
  front-matter / lifecycle `tool_timeout:`, wraps each tool call in its own
  child deadline. A tool that exceeds it fails a single call cleanly
  (`tool X exceeded tool_timeout …`) while the run continues; the run-level
  `timeout` remains the outer budget. `0` disables the per-tool deadline.

- **Structured tool-call traces in run records** — `.state/runs/*.jsonl`
  previously recorded only turn-level text, making tool-call forensics
  (exact argv, result content, error, timing) unrecoverable without source
  archaeology. `Turn` gains an opt-in `tool_calls` field (`ToolTrace`: name,
  redacted args, capped content, error, replay flag, duration) controlled by
  new `persist_runs_detail: none|tools` config (default `none`, byte-identical
  legacy output) and `persist_runs_tool_cap` (default 2048 bytes/field). Args
  are redacted via `internal/secret` before persisting.

- **Per-turn context clearing — `clear: true` on a turn section** — leather
  had per-turn *tool* scoping (`tools:` / `skills:` / `toolsets:` per turn)
  but no per-turn *context* bounding: `Session.Reset` had zero production
  callers, so context only ever grew, and every turn inherited everything —
  including the model's own intermediate speculation (measured on the
  sig-triage eval: a three-turn agent grew 1206 → 2828 prompt tokens across
  its turns and lost ~5 points to its two-turn sibling on paired per-issue
  comparison, replicated 3× under the registered battery; the first single
  draw said ~9). A turn may now
  declare `clear: true` alongside `tools:`/`skills:`/`toolsets:`: the
  conversation is reset before that turn's prompt is added; the system
  message and turn variables survive, because skill `extract:` captures live
  outside the session. That pairing is the point — distil a large tool
  result into `{{key}}`, then discard the raw blob. The alternative
  (splitting into two curings for a fresh session) was measured too and
  costs information: the handoff discarded the correct answer on 9.6% of
  issues; per-turn clear keeps one agent reasoning across the boundary.

- **`examples/14-sig-triage`** — assign a Kubernetes SIG to unlabeled issues
  with a small local model, with the accuracy claims measured rather than
  asserted: the same frozen 4B model scores 62.4%–81.6% on a 250-issue gold
  corpus depending only on the runtime design around it. The example ships
  the eval harness that measured it (tiered corpus, ablation-arm registry,
  paired per-issue verdicts, evidence archives with replayable analysis)
  alongside the winning curing design; method and verdicts are documented in
  `docs/LEP-0006-group-evals.md` and `docs/LEP-0008-conditional-routing.md`.

- **doclint** — a documentation-consistency gate (`scripts/doclint`, CI
  workflow `doclint.yml`) that cross-checks doc claims against the source,
  with per-line `doclint:allow` escapes and a file-level
  `doclint:disable-file` directive for documents that quote drift by design
  (audit reports); a missing docs root fails closed. Env vars leather *sets*
  for child processes (`os.Setenv`, e.g. `LEATHER_INTAKE_URL`) count as
  referents. The full doc set — subagent guides, `docs/GUIDE.md`, schemas —
  was reconciled against the code to bring the gate to zero violations:
  phantom flags/env/endpoints removed or marked planned, the stale
  `exec.*` shell-tools form replaced with the real `command`/`args` schema,
  tool-name casing corrected to snake_case, `/metrics` documented as the
  JSON it actually returns, and UI docs repointed from `/runs` to the real
  `/jobs` + `/history`.

- **Offline LLM fixture: `llm_record` / `llm_fixture`** — there was no
  supported way to run a full pipeline end-to-end without a live model:
  `MockLLM` was reachable only from `test-agent`, so proving wiring in CI
  meant hand-rolling a scripted OpenAI-compatible server. `--llm-record
  capture.jsonl` now wraps the live client and captures every completion
  (including tool calls) to JSONL; `--llm-fixture capture.jsonl` replays it
  instead of calling a model — one recorded completion per call, in order,
  failing loudly with the call index and last message when a run diverges
  from its recording. serve, run, and workflow-run share one client per
  process so replay order spans jobs. `make 06-smoke` is the working proof:
  the full ingest → triage → summarize → artifact pipeline of example 06,
  modelless, failing the target if no artifact is produced.

- **`leather validate` covers `shell-tools.json`** — the most format-fiddly
  hand-edited artifact was the only one with no schema and no validate
  coverage; a malformed tools file passed `leather validate` and failed only
  at runtime inside shell-mcp, as a silently tool-less agent.
  `schema.ValidateShellToolsJSON` now validates every `*.json` referenced
  from an `mcp-servers.yaml` command line: required fields, the removed
  `exec.*`/`argv` forms, unknown fields, RE2 pattern compilation, snake_case
  and duplicate names. A matching editor schema ships as
  `schemas/shell-tools-1.schema.yaml`. First catch: example 09 declared
  `"timeout": N`, a field shell-mcp silently ignores — its tools had been
  running at the 30 s default since they were written (now
  `timeout_seconds`).

- **Schema ↔ runtime parity is now enforced** — a test
  (`internal/schema/parity_test.go`) asserts every runtime validator field
  in `defs.go` appears in the corresponding `schemas/*.schema.yaml` and
  vice-versa (nested-only blocks declared explicitly), so the editor-schema
  drift class cannot recur; the YAML schemas stay hand-written because their
  descriptions carry operator guidance codegen would flatten. First catches:
  agent frontmatter accepted `toolsets`, `tool_timeout`, and `thinking`
  while both `defs.go` and `agent-1.schema.yaml` omitted them.

- **`make new-example NAME=<slug>`** — adding an example was tribal
  knowledge (pick the next index, hand-register Makefile targets, copy
  `pretty.sh`, source `preflight.sh`). The scaffolder allocates the index,
  creates the standard tree, appends the `NN`/`NN-live` targets, and prints
  the two hand-written registrations; the convention itself is now
  documented in `examples/README.md`.

- **`docs/CONVENTIONS.md`** — central environment-variable reference (name,
  default, scope, effect) covering the binary's load-bearing vars, shell-mcp,
  and the example-shell contract (`LEATHER_DEMO_MODE` and the dry-mode
  idiom, example 13's git vars, webhook/GitHub tokens). The GUIDE config
  reference now also documents `llm_endpoint`/`llm_api_key`,
  `mcp_servers_file` (without it, agents run tool-less with no error),
  `queue_pattern` routes, and the `workflow run` webhook-secret coupling.

## [0.4.1] — 2026-07-05

### Fixed

- **Module path casing prevented installation** — `go.mod` declared the module
  path as `github.com/tgpski/leather` (lowercase), but the canonical repository
  is `github.com/TGPSKI/leather`. Because Go module paths are case-sensitive,
  `go install github.com/TGPSKI/leather@...` failed with a version-constraints
  conflict. The module path and all imports now use the repository casing so
  installs resolve correctly.

## [0.4.0] — 2026-07-05 "vegan leather"

### Fixed

- **Curing-driven agents never inherited the global default model** —
  `curing/worker.go`'s runner path (webhook → queue → curing worker,
  used by `leather serve`'s tannery integration and `leather workflow run`)
  never applied `cfg.Model` for agents with no `model:` front-matter field,
  unlike the scheduler and `leather run` paths. Every such agent was sent to
  the LLM client with an empty model name. `agentsByName` now resolves each
  agent through the same `resolveAgent` defaulting used elsewhere before
  handing the map to `curing.NewSupervisor`.
- **Prefix-scan curings silently locked at `concurrency: 1`** — curings using
  `queue_prefix` (one single-use queue per event, e.g. via a route's
  `queue_pattern`) have no static `Queue` name, so `NewSupervisor`'s
  `concMap[def.Queue]` lookup never matched any `queues:` entry in
  `tannery.yaml` and silently defaulted to `concurrency: 1` regardless of
  what was configured — serializing what `queue_pattern` is specifically
  meant to parallelize. `NewSupervisor` now falls back to `concMap[def.QueuePrefix]`
  when `Queue` is empty, so a `queues:` entry keyed by the prefix name (e.g.
  `pr-meta: {concurrency: 8}`) takes effect. `examples/11-high-volume-ci`'s
  `tannery.yaml` now declares real concurrency for its four prefix-scan
  curings, surfaced by #28's `examples-all` validation on `ego-killer`.
- **`shell-tools.json` tool schemas missing `required`, so models never learn
  which arguments to supply** — `cmd/shell-mcp`'s `tools/list` handler builds
  each tool's `inputSchema.properties` only from its `required`/`defaults`
  fields; a tool with neither declares an empty schema. Examples 10, 11, and
  12's `shell-tools.json` declared no `required` field on any tool, so the
  model never learned it needed to supply e.g. `pr_number`/`repo`, and every
  `{{pr_number}}`/`{{repo}}` placeholder in the tool's shell command passed
  through unsubstituted. Example 10's dry-mode fallback happened to mask
  this (an unsubstituted placeholder just produces a nonexistent filename,
  falling back to a default fixture); example 11's dry-mode fallback does
  shell arithmetic on the same value and failed loudly (`arithmetic syntax
  error`) on every single call. All three files now declare `required`
  matching their `{{key}}` placeholders.

- **System-prompt-only agents rejected by strict backends** (#41) — agents
  with no `prompt:`/`prompts:` configured (pure system-prompt + scheduled
  tool use) sent completion requests with zero user-role messages, which
  strict OpenAI-compatible backends reject with `400: "No user query found
  in messages"`. `runner.Run` now sends a placeholder user message
  (`"Proceed."`) for these agents instead of skipping the user turn entirely.
- **`examples/scripts/examples-summary.sh` path bug** — `EX_DIR` doubled the
  `examples/` path segment (`.../examples/examples`), so `make summary`
  silently reported an empty, all-zero rollup instead of erroring or finding
  any example state.
- **Curing collect fan-in (`collect_size`) hung forever and leaked state when
  one leg permanently DLQ'd** (#44) — `runCollectFromQueue`/`runCollect`
  polled `len(items) < CollectSize` forever with no timeout, TTL, or
  staleness check. If any one of a fan-in group's expected legs exhausted
  its own `max_attempts` and DLQ'd, the group could never reach
  `CollectSize` again: the downstream agent never fired, and the
  already-collected items (plus their underlying hides) leaked on disk
  indefinitely with no operator-visible signal. Found while load-testing
  `examples/11-high-volume-ci` against a backend serialized to one request
  at a time — decisions plateaued below the target webhook count with
  nothing in the logs explaining why. Added `CuringDefinition.CollectTimeoutSeconds`
  (default 900s; `0` preserves the old wait-forever behavior): a partial
  collect group that exceeds this age is now evicted to `<queue>-dlq` and
  emits a new `TanneryEvent{Kind: "stale"}`, rendered in `leather serve
  --pretty` and forwarded to the devtools event bus.
- **Curing worker corrupted prompts and lost work under concurrency** (#45) —
  four fixes found load-testing a fan-out/fan-in pipeline at concurrency 8+:
  `process()`/`handleCollected()` mutated a shared `*model.Agent` when
  injecting hide content into the prompt, garbling prompts across concurrent
  runs (both now clone the agent config first); `handleItemFromQueue` deleted
  the queue entry even on failure, permanently losing the item on any
  transient error (failures now re-enqueue up to `max_attempts`, then route
  to `<queue>-dlq`); a failed fan-in `handleCollected` call silently dropped
  the entire already-dequeued collect group with no retry, no DLQ, and leaked
  source hides (`requeueOrDLQGroup` now applies the per-item retry/DLQ
  convention to whole groups); and a fan-in curing's own artifact recorded
  `hide_kind` as whichever input leg was collected first instead of its own
  name.
- **HTTP client timeout raced the run's context deadline** —
  `http.Client{Timeout: ...}` in `internal/session/http_client.go` fired
  independently of the context deadline set by `runner.Run` / the curing's
  `timeout_seconds`, producing spurious "context deadline exceeded" errors
  well before the real timeout under load. The context deadline is now the
  single source of truth for request timeouts.
- **Reasoning-model flakiness in the tool-call loop** — confirmed via direct
  replay against the LLM endpoint outside leather (Qwen3 via vLLM,
  `--tool-call-parser qwen3_xml`, ~25% reproduction under load; prompt
  instructions had zero effect): the model would occasionally re-issue an
  already-succeeded tool call verbatim instead of progressing, spinning until
  max tool rounds was hit — and separately would sometimes stop naturally
  (`finish_reason: "stop"`) with zero output tokens instead of the expected
  answer, propagating downstream as blank fields. A tool call with the same
  name and arguments that already succeeded this run is no longer re-executed
  (scoped to non-hide tools; hide pagination legitimately repeats calls), and
  the existing length-truncation self-healing retry now also retries once,
  bare, on an empty natural stop.
- **Answer text emitted alongside tool calls was dropped from the final
  answer** — the same reasoning-model flakiness family: the model sometimes
  emits the head of its final answer in the same completion as a tool call,
  then continues from where it stopped after the tool result; the runner
  recorded only the last round's content, silently head-truncating the
  artifact (observed as 6/40 fan-out legs losing their identifier header in
  a burst load test, cascading into blank or placeholder-text reports
  downstream). Fragments emitted in non-hide tool-call rounds are now banked
  and spliced ahead of the final round's text, and the bare empty-stop retry
  is skipped when banked fragments already carry the answer.
- **16-bit ID suffixes collided under burst load, cross-wiring fan-in groups
  and deleting shared hides** — `ids.TimestampHex` drew its uniqueness suffix
  from `mathrand.Int31n(0x10000)`: 65,536 values per `(prefix, minute)` bucket,
  ~1% birthday-collision odds at ~40 IDs/minute. In a 100-webhook burst, two
  concurrent `pr-context` legs drew the same hide ID; `hide.Store.Put`'s
  `os.MkdirAll` silently merged them, one PR's decision group collected and
  (on success) deleted the shared hide, and the other PR's group found its
  leg's hide missing and DLQ'd after retries — or, with the opposite race
  order, would have silently used the wrong PR's analysis content. The suffix
  is now 32 bits (`%08x`, ~1-in-a-million odds at hundreds of IDs per bucket),
  and `Put` creates the hide directory exclusively (`os.Mkdir`), regenerating
  the ID on collision instead of overwriting an existing hide — a colliding
  ID can no longer destroy another group's data even if one is drawn.

### Added

- **`leather completion`** — new subcommand that prints a static shell
  completion script for `bash`, `zsh`, or `fish` (`leather completion <shell>`).
  Each script mirrors the `Run` dispatch table so every subcommand and its
  flags complete at the prompt; source it from your shell profile (e.g.
  `source <(leather completion zsh)`).
- **Reasoning-aware `completion_reserve`** — fixes reasoning-model completions
  (Qwen3-class, thinking enabled) getting cut off mid-thought before any
  answer content exists, because a single flat `completion_reserve` reserved
  too few tokens for both the `<think>` trace and the answer.
  - Per-agent `completion_reserve` override, declarable via `completion_reserve:`
    in `*.agent.md` front matter or `*.lifecycle.yaml`, mirroring the existing
    `max_tokens` override.
  - New `reasoning_reserve` config field splits the budget: `completion_reserve`
    now means answer content only, `reasoning_reserve` covers the `<think>`
    trace, and `max_tokens` sent to the model is their sum. Global-only,
    defaults to `0` (fully backward compatible).
  - Self-healing retry: a completion truncated (`finish_reason: "length"`)
    before producing any content or tool calls is retried once with a doubled
    reserve, bounded by remaining context.
  - Leather-internal model-aware defaults: agents targeting a known reasoning
    model (Qwen3, QwQ, DeepSeek-R1) get a larger `completion_reserve`
    automatically unless a per-agent override is set explicitly.
- **`thinking:` agent front-matter field** — `thinking: false` in `*.agent.md`
  sets `Agent.DisableThinking`, sending
  `chat_template_kwargs.enable_thinking=false` with each request to disable a
  reasoning model's hidden `<think>` trace per agent. The zero value leaves
  model default behavior untouched. Disabling thinking was the most effective
  fix for both tool-call-loop flakiness modes above, and combined with
  right-sizing `completion_reserve` measured a 5.2x speedup (323s → 62s) on
  `examples/11-high-volume-ci`'s 40-webhook burst load test with
  equal-or-better correctness.
- **shell-mcp per-argument `patterns` validation** — optional per-tool
  `patterns` map in `shell-tools.json` (argument key → RE2 regexp) validated
  before the command runs and advertised in the tool's `inputSchema`. A
  missing argument validates as the empty string, so anchored patterns also
  reject absent values — catching a flaky model passing blanks or literal
  prompt-template placeholders like `<number>` instead of real values.
  `examples/11-high-volume-ci` uses it to pattern-constrain `pr_number` and
  `repo` on all four GitHub tools.
- **Full-system load-test profiler** — `scripts/profile/profile-run.sh` wraps any
  command (e.g. `make 11`) with 1-2 Hz samplers for host CPU/memory/disk
  (vmstat + sysstat when installed), kernel pressure-stall information,
  per-process attribution (pidstat), GPU telemetry (nvidia-smi), CPU/board
  temperatures, and vLLM's `/metrics` (running/waiting requests, KV-cache
  usage, token throughput, prefix-cache hit rate), then prints an avg/peak
  summary via `scripts/profile/profile-summary.py`. Used to produce the 100-webhook
  profile documented in `examples/11-high-volume-ci/README.md`: ~500 LLM
  jobs orchestrated for ~6% of one host's CPU and zero measurable IO
  pressure, GPU-bound end to end.

### Removed

- **`leather chat`** — the interactive chat subcommand is removed with no
  deprecation cycle. Use `leather run` for one-shot agent execution; chat's
  flags (`--system`, `--agent`, `--dev`) were local to the command and are
  removed with it.
- **`ROADMAP.md`** — the standalone roadmap file and its references (README,
  `release-prep` skill) are removed; deferred-item tracking now lives in
  GitHub issues.

## [0.3.0] — 2026-06-07

### Added

- **`leather workflow run`** — bounded one-shot tannery workflow execution from
  the CLI. The command ingests a hide from a file or stdin, resolves a curing
  by route or explicit `--curing`/`--queue`, starts the needed curing workers,
  drains queues to quiescence, and exits with clear status codes for success,
  runtime failure, timeout, or DLQ items.
- **Outbound tool resilience** — per-tool retry configuration, transient error
  classification, exponential backoff with optional `Retry-After` handling,
  per-host rate limits, outbound DLQ routing, `leather dlq inspect/requeue`,
  and tool retry/backoff/rate-limit/DLQ metrics.
- **Queue poll interval configuration** — `poll_interval` is now parsed from
  queue concurrency config and applied by workers, with a 1s default and faster
  intervals in examples/tests where short runs matter.
- **Log file support** — `--log-file` / `LEATHER_LOG_FILE` writes structured
  logs to a file and falls back cleanly to stderr when file setup fails.
- **Concurrent git workflow example** — `examples/13-git-workflow-commit`
  demonstrates `leather workflow run` with a planner curing that fans out
  per-file signed git commit tasks to executor curings.

### Changed

- Git workflow agents, skills, and shell tools now collect fuller per-file
  diffs, use more concrete commit-message rules, avoid duplicate enqueue
  calls, and allow additional tool rounds for reliable metadata extraction.
- Raspberry Pi / Hailo examples use the dedicated `rpi-01`–`rpi-03` namespace
  so mainline example numbering can continue independently.

### Fixed

- `leather workflow run` now waits for both empty queues and in-flight worker
  handlers before declaring quiescence, preventing skipped Phase 1 commits and
  stale Phase 2 queue processing.
- `leather workflow run` sets `LEATHER_INTAKE_URL` before MCP servers start so
  child shell-MCP tools inherit the correct intake endpoint.
- Workflow help text, usage wiring, shell-tool diff caps, and git commit
  command handling were corrected to avoid confusing output and silent
  failures.

## [0.2.0] — 2026-06-05 "weathered"

### Added

- **Shared stdlib leaf utilities** (`internal/fileutil`, `internal/jsonstore`,
  `internal/ids`, `internal/yamlx`) — four zero-dependency leaf packages that
  consolidate helpers previously duplicated across the codebase (issue #3,
  phase 1 of the ROADMAP "Shared library extraction" track):
  - `fileutil`: `Exists`, `AtomicWriteFile`, `AtomicWriteFileFunc` — atomic
    temp-rename writes with automatic parent-dir creation and cleanup on failure.
  - `jsonstore`: `Save` / `Load` — marshal+atomic-write and read+unmarshal with
    a `(found bool, err error)` return so a missing file is `(false, nil)`.
  - `ids`: `TimestampHex(prefix)` — `<prefix>_<YYYYMMDD_HHMM>_<hex>` IDs used
    by artifacts, queue items, and hides; `RandHex(n)` — crypto-random hex for
    bearer tokens.
  - `yamlx`: `ParseBlock`, `ParseFlat`, `StripQuotes`, `SplitKV` — the
    stdlib-only flat-YAML parser moved out of `internal/config` and available
    to all packages without import cycles.
- All duplicated inline copies replaced: `internal/scheduler`, `internal/cache`,
  `internal/queue`, `internal/artifact`, `internal/hide`, `internal/cli`
  migrated onto the new packages. `internal/config/yaml.go` deleted; its YAML
  tests moved to `internal/yamlx`.
- **`internal/httpx`** — `WriteJSON(w, status, v)` and `WriteError(w, status, msg)`:
  shared HTTP response helpers extracted from `internal/cli`. Eliminates 25+
  inline `w.Header().Set("Content-Type", "application/json")` +
  `json.NewEncoder(w).Encode(…)` clusters across `cmd_serve.go`,
  `api_tannery.go`, and `api_devtools.go` (issue #17, phase 2).
- **`yamlx.ParseFlatLines`** — like `ParseFlat` but also returns a
  `map[string]int` of field name → 1-indexed source line number, enabling
  `file:line` prefixes in schema violation output (issue #17, phase 2).
- **`schema.Violation.Line`** — new `Line int` field (0 = unknown) populated
  by `ValidateFlat` when line data is available. `leather validate` now emits
  `schema: file:N:  field "…": …` for config/skill/toolset/worker YAML files.
- **`leather snapshot save / restore`** — built-in point-in-time backup and
  restore for runtime state (issue #6). `save` archives `queues/`, `runs/`,
  and `cache/` (plus tannery `hide_dir/` and `artifact_dir/` when configured)
  into a `tar.gz` file, skipping transient files (`leather.lock`,
  `devtools.token`). `restore` extracts into the configured state directory
  with a non-empty-dir guard (`--force` to override). Both commands verify
  that `leather serve` is not running before proceeding.
- **DevTools `queue.run` event** — when the scheduler dequeues an item and
  begins a direct agent run, a `queue.run` event is emitted on the DevTools
  bus with queue name, item ID, hide ID, attempt count, and payload key names
  (values are never exposed). Each subsequent runner event is causally linked
  to the `queue.run` event via `AppendCause`, making the queue→agent lineage
  visible in the DevTools DAG view (issue #11).
- **`leather attach`** — new subcommand that joins a running `serve` instance
  and streams pretty-printed DevTools events to the terminal (issue #19).
  Reads the DevTools token from the state directory, connects to the
  `/api/devtools/events` SSE endpoint, and renders each event with
  color-coded kind labels, entity references, and payload key-value pairs.
  Supports `--filter` to scope output by event kind or source, and
  `--no-reconnect` to exit on stream close instead of reconnecting with
  exponential backoff.

## [0.1.3] - 2026-06-05

### Added

- `leather doctor` subcommand: prints effective configuration with source
  attribution (`default` vs. `config/env/flag`) for every key. Secret-bearing
  values (`llm_api_key`) are redacted to a 4-char prefix + mask so operators
  can confirm which credential is loaded without exposing the full token.
- `leather init` subcommand: scaffolds a new project directory with a
  `.env`, `config.yaml`, example `agents/my-agent.agent.md`,
  `agents/my-agent.lifecycle.yaml`, and a `Makefile`.
  - `--dir <path>` selects the target directory (created if absent; defaults
    to `~/.leather`).
  - `.env` pre-populates `LEATHER_LLM_ENDPOINT`, `LEATHER_MODEL`,
    `LEATHER_LLM_API_KEY`, `LEATHER_LOG_LEVEL`, and `LEATHER_AGENT_DIR`
    with comments for `source .env` / direnv usage.
  - Fails closed on existing files — any collision is reported with a hint to
    use `--overwrite`.
  - `--overwrite` replaces existing files.
  - Schema-validates the scaffolded `config.yaml` and lifecycle file before
    reporting success.
- **Qwen/Hermes text tool call fallback**: models that emit
  `<tool_call>{json}</tool_call>` blocks in the content channel instead of
  the API `tool_calls` array now parse and execute correctly. Truncated
  trailing blocks (finish_reason=length) are silently dropped so the run
  continues on the next round.
- **RPi examples rpi-01–rpi-03** — Raspberry Pi 5 + AI HAT+ 2 (Hailo-10H) examples
  validated on live hardware against `qwen3:1.7b` (renamed from 13–15 to give the
  RPi track its own stable namespace):
  - `rpi-01-hailo-endpoint-canary`: endpoint sanity check.
  - `rpi-02-hailo-local-status-digest`: shell evidence collection → scheduled
    digest without tannery.
  - `rpi-03-hailo-local-status-ingest`: evidence → hide → curing → artifact.
- `docs/integrations/rpi-hailo.md` integration guide for Raspberry Pi 5 +
  Hailo-10H.
- `make install` target and `LEATHER_RPI_*` env vars in the examples Makefile.
- GitHub issue template for agent work items.
- **Agent Skills** `release-prep` and `release-tag` in `.agents/skills/`:
  - `release-prep` — auto-detects the next semver from git history
    (PATCH/MINOR/MAJOR categorisation), inserts a CHANGELOG section, updates
    docs, and commits + pushes to `main`.
  - `release-tag` — runs four pre-flight gates (clean tree, in sync with
    origin, CHANGELOG has the version, tag does not already exist), then
    creates and pushes an annotated tag to trigger the automated release
    pipeline.
- `.claude/skills/` symlinks pointing to `.agents/skills/` so Claude Code
  discovers project skills without duplicating files.
- `make link-skills` target recreates those symlinks for contributors cloning
  fresh.

### Changed

- Tool call limit raised from 16 to 100 in `internal/schema/defs.go`, removing
  a ceiling that caused batch agents to hit mid-run limits on large workloads.

## [0.1.2] - 2026-06-01

### Changed

- Replaced `LICENSE` with canonical GPL-3.0 SPDX text for `pkg.go.dev` license
  detection.

## [0.1.1] - 2026-06-01

### Added

- `doc.go` package documentation for `pkg.go.dev` landing page.

## [0.1.0] - 2026-05-31

First public release.

### Added

#### Core runtime

- Single-binary CLI (`leather`) with subcommands `serve`, `run`, `chat`,
  `validate`, `test-agent`, `status`, `ingest`, `replay`, `version`,
  `help`.
- Agent definition format: Markdown body with optional YAML front matter
  and a sibling `*.lifecycle.yaml` for schedule, model overrides, and
  per-turn parameters. Lifecycle-only and front-matter-only flows both
  supported; `applyLifecycle` is a non-destructive merge that preserves
  front-matter for fields the lifecycle does not explicitly set.
- Session context management with token-budget tracking against any
  OpenAI-compatible endpoint (local vLLM, OpenAI cloud, etc.), including
  summarisation and truncation strategies before model limits are hit.
- Multi-turn tool-calling with deterministic abort gating and a
  per-turn parameter scope.
- Deterministic runtime variables: tool results can extract values that
  later turns substitute via `{{key}}` templating. Templates supported:
  `{{env:VAR}}`, `{{key}}`, `{{.field}}`, `{{hide_id}}`.
- Buffered "hides" intercept oversized tool output so the agent reads
  scoped cuts/pages instead of saturating the context window.
- Companion `shell-mcp` binary: a Model Context Protocol server that
  exposes a manifest-defined catalog of local shell commands as MCP
  tools, with positional-arg templating and `--no-shell` parsing-only
  mode for CI.

#### Tools, skills, toolsets

- Native stdio-based MCP client. Allowlists per server. Subprocess
  hygiene: `setpgid`, stderr forwarding, `SIGTERM` → `SIGKILL` on the
  process group at shutdown. Decoder is poisoned on read timeout so
  subsequent `Call` invocations return `ErrPoisoned` instead of reading
  garbage off a desynchronised stream.
- Per-skill `required_env` allowlist for `{{env:VAR}}` expansion in
  tool arguments — env-var exfiltration through tool arg templates is
  blocked at the skill boundary.
- Shell, HTTP, and MCP tool definitions resolvable via tools, toolsets,
  or skill manifests with deterministic precedence rules. `*.toolset.yaml`
  files validated by `leather validate`.

#### Tannery (event-driven curing service)

- Event-driven curing pipeline: ingest a hide, route it through one or
  more agents, produce an artifact with full lineage.
- Persistent file-backed hide store with safe-path anchoring (no
  traversal out of the configured root).
- Artifact store with `curing` + `hide_id` lineage fields and parent-dir
  creation on file output routes.
- Webhook intake worker with body-size caps (5 MiB default, 50 MiB hard
  limit), mandatory secret validation (fail-closed on unset env), and
  fan-out idempotency keyed on `X-GitHub-Delivery` (`EnqueueIfAbsent` +
  hide rollback on enqueue failure).
- HTTP poll worker with `Retry-After` honouring (seconds or HTTP-date,
  capped at 5 minutes) for `429` / `503` responses.

#### Scheduler & queues

- Cron-style scheduler with bounded concurrency (`--max-concurrent-jobs`),
  graceful shutdown that drains in-flight work before cancelling, and
  SIGHUP-triggered re-registration when agent files change on disk
  (sha256-hash diff).
- Per-job emit serialisation in the curing worker by default;
  `EventFnConcurrent` opt-out for callers that need concurrent event
  delivery.
- File-backed JSONL FIFO queues with retry counters and per-queue DLQ.
- Single-use ephemeral queues for high-concurrency fan-in / fan-out
  patterns. `queue_pattern` → `queue_prefix` linkage validated at config
  load with a clear error on mismatch.
- HTTP API for `/queues/<name>`, `/queues/<name>-dlq`, and
  `/queues/<name>/requeue` (multi-status 207 on partial requeue
  failures with explicit `failed[]` list).

#### Observability & operations

- `/healthz` reports state-dir writability and LLM-endpoint
  configuration; returns 503 + JSON body when degraded.
- `/metrics` (Prometheus-style text format) and `/status` endpoints.
- DevTools UI at `/devtools` with per-launch hex auth token written to
  `<state-dir>/devtools.token` (`0600`), Bearer-middleware enforced.
- Welcome card with token input + Retry on first-connect failure
  (no-token, network error, 401/403, 503, loading timeout).
- Flow view in DevTools renders curings as pipelines; SSE event stream
  with CR/LF-sanitised `event:` fields.
- Replay subsystem: capture sessions, replay them later via
  `leather replay` (translates to `serve --api --replay` /
  `--replay-live`) for deterministic debugging and demos.
- Single-process lock per `--state-dir` via `flock`; the second process
  exits with code 2 and a clear remediation message.
- Pretty-mode CLI output with auto-disable when stdout is not a TTY,
  Tannery event icons (`→` webhook, `↑` enqueue, `↓` dequeue), inline
  agent responses, and explicit log-discard warning in pretty mode.
- Startup banner enumerates loaded agents with per-agent
  `schedule=…` / `queue=… (consumer)` / `disabled` rows.

#### Configuration

- Stdlib-only YAML loader for `config.yaml`, `tannery.yaml`,
  `*.lifecycle.yaml`, `*.agent.md` front matter, `*.toolset.yaml`, and
  MCP-server manifests.
- Schema validation via `leather validate <dir>` with `version:` field
  reserved on every top-level type for forward compatibility.
- Every flag has a matching `LEATHER_*` env var; flag wins on conflict.
- Schema files under `schemas/` describing every supported document.

#### Examples (12)

End-to-end runnable examples under `examples/`, each with its own
`Makefile` target, README, and `.env.example`:

1. `01-hello-mock` — smoke test against the mock LLM.
2. `02-scheduled-agent` — periodic cron-driven agent.
3. `03-shell-skill` — local tool execution via shell-mcp.
4. `04-tannery-ingest` — ingest a file as a hide, run a curing.
5. `05-tannery-webhook` — receive a GitHub webhook, route to a curing.
6. `06-multi-agent-curing` — two-agent pipeline producing an artifact.
7. `07-external-routing` — outbound notification (Telegram).
8. `08-dead-letter-queue` — DLQ inspection and requeue workflow.
9. `09-land-tracker` — long-running state aggregation.
10. `10-ci-gate` — parallel webhook fan-out for PR checks.
11. `11-high-volume-ci` — single-use queue pattern for high-throughput CI.
12. `12-spa-maintenance` — scheduled multi-step maintenance pipeline.

A `make examples-all` target runs every example end-to-end with a
per-target reliability/summary script.

#### Documentation

- `README.md` with a `Which mode do I want?` decision table,
  Raspberry Pi / small-server sizing guidance, install-verification
  snippet, and an explicit `Not in v0.1` section.
- `docs/GLOSSARY.md` as the authoritative vocabulary reference.
- `docs/ARCHITECTURE.md` with package layout and Mermaid diagrams of the
  Tannery pipeline.
- `docs/OPERATIONS.md` covering state-dir layout, systemd unit,
  `/healthz` + `/metrics` shape, DLQ workflow, DevTools auth, upgrade
  procedure, and troubleshooting table.
- `docs/TEMPLATES.md` single-table reference for `{{env:VAR}}`,
  `{{key}}`, `{{.field}}`, `{{hide_id}}`.
- `docs/GUIDE.md` end-to-end author guide.
- `AGENTS.md` + 17 per-domain `.subagents/AGENTS-*.md` guides for
  AI-coding-agent contribution flow.
- `SECURITY.md` with v0.1 threat model and known limits.
- `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `LICENSE` (GPL v3).

#### CI / release

- `.github/workflows/ci.yml`: SHA-pinned actions, `make check` +
  `make test-race` + `golangci-lint` + integration tests on every push
  and PR; cross-platform `full-scope` matrix (linux/arm64, macos/arm64)
  on `main` push or `full-test` label.
- `.github/workflows/release.yml`: triggered by `v*` tag push, builds
  `leather` + `shell-mcp` for linux/amd64, linux/arm64, darwin/amd64,
  darwin/arm64 with SHA-256 checksums; publishes a GitHub Release with
  notes extracted from this file.
- `.github/ruleset-*.json` declarative branch and tag protection
  rulesets (signed commits, required reviews, immutable release tags).

### Security

- Path-traversal anchoring (`internal/safepath`) applied to hide,
  artifact, queue, cache, and tool `OutputFile` writers.
- Outbound HTTP tool client uses a 30 s timeout.
- HTTP server uses 5/60/120/120 s read/write/idle/handler timeouts.
- Telegram bot tokens scrubbed from `*url.Error` strings before logging.
- DevTools demo bundle gated behind `?demo=1`.
- Non-loopback API bind emits a startup warning.
- SSE `event:` field CR/LF sanitisation to block injection through
  event names.
- Webhook handler validates secrets at startup (fails closed) and uses
  `EnqueueIfAbsent` with idempotency keys to prevent duplicate fan-out
  on partial writes.
- Tannery init wrapped in a success-guard so partial-init failure cannot
  leave a stale lock behind.

### Fixed (selected from release-readiness cycle)

Representative highlights from the multi-phase review sweep:

- `{{hide_id}}` template variable now reads the artifact's `HideID`
  rather than a stale curing-level value.
- Shutdown ordering: scheduler and workers are drained before context
  cancellation; two data races in `cmd_serve.go` closed.
- Metrics summaries snapshot under RLock and iterate outside the lock.
- DLQ requeue propagates per-item errors via `failed[]` and returns
  HTTP 207 on partial failure.
- Lifecycle parser preserves nested-block indentation; mistyped silent
  lifecycles no longer flatten parameter maps.
- Per-run timeout in the runner now covers the whole round loop, not a
  single turn.
- Response cache key includes user prompts (`\x01`-joined) so identical
  system prompts with different inputs no longer collide.
- Bus subscribers can be cleanly removed via `SubscribeWithCloser` with
  an idempotent `sync.Once` closer; publishers do I/O outside the mutex.
- `chat` REPL uses a 1 MiB scanner buffer and per-call SIGINT
  cancellation so Ctrl-C aborts an in-flight call without killing the
  loop.
- `/cache/stats` memoised with a 1000-entry cap and 10 s TTL.
- MCP `tools/list` schema fetching fixed for block-style frontmatter
  lists.

### Known limitations (post-v0.1 roadmap)

Intentionally out of scope for v0.1.0; tracked for v0.2:

- Shared library extraction (`internal/{fileutil,jsonstore,ids,yamlx,httpx,template,synx}`).
- `leather doctor` and `leather init` scaffolding subcommands.
- Backup/restore tooling beyond `tar -czf state-dir`.
- LLM-side prompt-injection mitigation in the summariser (hide buffering
  already isolates untrusted bulk output).
- `seedSeen` persistence in the HTTP poll worker.
- Embedded UI assets via `embed.FS` (UI currently shipped from `ui/`).
- DevTools event-model expansion for queue-input agent lineage.
- Outbound HTTP tool resilience (uniform rate-limiting, retry/backoff,
  outbound-failure DLQ for tool calls hitting external APIs).
- Windows support (Makefile assumes POSIX tools).

See [ROADMAP.md](ROADMAP.md) for the full deferred-item list with
rationales and proposed shapes.

[Unreleased]: https://github.com/TGPSKI/leather/compare/v0.5.3...HEAD
[0.5.3]: https://github.com/TGPSKI/leather/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/TGPSKI/leather/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/TGPSKI/leather/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/TGPSKI/leather/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/TGPSKI/leather/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/TGPSKI/leather/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/TGPSKI/leather/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/TGPSKI/leather/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/TGPSKI/leather/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/TGPSKI/leather/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/TGPSKI/leather/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/TGPSKI/leather/releases/tag/v0.1.0
