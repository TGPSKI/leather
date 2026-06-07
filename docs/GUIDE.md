# Leather Implementation Guide

Practical recipes and style rules for building agents, curings, and tannery
pipelines with leather. Examples 01–08 in `examples/` are the canonical
reference for every pattern described here.

---

## Contents

1. [Core concepts](#1-core-concepts)
2. [Config — `config.yaml`](#2-config--configyaml)
3. [Agents — `*.agent.md`](#3-agents--agentmd)
4. [Lifecycle — `*.lifecycle.yaml`](#4-lifecycle--lifecycleyaml)
5. [Shell tools — `shell-tools.json`](#5-shell-tools--shell-toolsjson)
6. [MCP servers — `mcp-servers.yaml`](#6-mcp-servers--mcp-serversyaml)
7. [Skills — `*.skill.yaml`](#7-skills--skillyaml)
8. [Curings — `*.curing.yaml`](#8-curings--curingyaml)
9. [Tannery — `tannery.yaml`](#9-tannery--tanneryyaml)
10. [Recipes](#10-recipes)
    - [Scheduled agent](#recipe-scheduled-agent)
    - [Shell-tool agent](#recipe-shell-tool-agent)
    - [Webhook → single curing](#recipe-webhook--single-curing)
    - [Two-agent pipeline](#recipe-two-agent-pipeline)
    - [Event routing (multi-route)](#recipe-event-routing-multi-route)
    - [Fan-out / fan-in](#recipe-fan-out--fan-in)
    - [Dead-letter queue](#recipe-dead-letter-queue)
    - [Notify on artifact](#recipe-notify-on-artifact)

---

## 1. Core concepts

| Term | What it is |
|---|---|
| **Agent** | One scoped model worker. Defined in `*.agent.md`. |
| **Curing** | Named pipeline: queue → agent → artifact (→ next queue). |
| **Hide** | Raw input bytes for a curing run (webhook payload, ingested file). |
| **Cut** | A paged slice of a hide, one context-window's worth. |
| **Tannery** | Long-running workspace container: webhooks, routes, queues. |
| **Skill** | Tools + a short prompt fragment. Tells the agent *how* to use the tools. |
| **Toolset** | Named tool bundle. No prompt fragment. |
| **Queue** | Durable on-disk FIFO. Workers poll; items retry up to `max_attempts`. |
| **DLQ** | Dead-letter queue. Items land here after exhausting `max_attempts`. |

Leather is **single-binary, stdlib-only Go**. No external dependencies,
no Docker required. All state (hides, artifacts, queues, run history) is
on local disk under `state_dir`.

---

## 2. Config — `config.yaml`

Every workspace starts with a `config.yaml`. This is the only required file
for `leather serve`.

```yaml
# --- Paths ---
agent_dir: agents          # where *.agent.md files live
tool_dir:  tools           # where *.skill.yaml / *.toolset.yaml files live
state_dir: .state          # hides, artifacts, queues, run history

# --- Model ---
model: /path/to/model      # model name or path; override: LEATHER_MODEL=
llm_timeout: 120s          # per-call timeout; override: LEATHER_LLM_TIMEOUT=
max_tokens: 8192           # context window budget
completion_reserve: 1024   # tokens reserved for model output
summarize_threshold: 0.85  # compress history at 85% of max_tokens

# --- Runtime ---
log_level: info            # debug | info | warn | error
scheduler_tick: 1s         # how often the scheduler checks for due jobs
max_concurrent_jobs: 2     # max simultaneous agent calls

# --- HTTP API (required for tannery and queue inspection) ---
api: true
api_addr: 127.0.0.1:7749
persist_runs: true         # write run history JSONL to state_dir/runs/

# --- Optional: notification backends ---
notify:
  backends:
    - name: telegram-ops
      type: telegram
      chat_id: "123456789"
      token:
        pass: telegram/bot_token      # resolved from pass(1) secret store
```

**Every flag has a matching env var.** `--model` → `LEATHER_MODEL`, `--llm-endpoint`
→ `LEATHER_LLM_ENDPOINT`. Flags win when both are set.

### Minimal config (no tannery, no notify)

```yaml
agent_dir: agents
state_dir: .state
log_level: info
model: my-model
api: true
api_addr: 127.0.0.1:7749
scheduler_tick: 1s
max_concurrent_jobs: 2
persist_runs: true
```

---

## 3. Agents — `*.agent.md`

The agent file is the system prompt plus identity metadata. Nothing else.

### Format

```markdown
---
name: agent-name
skills: [skill-name]     # optional
tool_rounds: 2           # optional; needed when agent calls tools
timeout: 120s            # optional
---

System prompt goes here.
```

### The leather agent style

Agents 01–08 establish the style. Three rules cover most cases:

**Rule 1 — One job per agent.**
Each agent does one thing. If a task has two phases, use two agents in a
pipeline (see [two-agent pipeline recipe](#recipe-two-agent-pipeline)).

**Rule 2 — Short system prompt.**
Role sentence + output specification. No numbered STEP lists. No
multi-paragraph instructions that enumerate sub-tasks. The agent's job should
be describable in one sentence followed by an output format.

**Rule 3 — Specify the output format exactly.**
Tell the agent what to write and how. Use fixed fields, bullet counts, or
structured lines. Vague prompts produce vague output; specific formats produce
parseable output that downstream agents can rely on.

### Prompt anatomy

```
[One sentence: what you receive and what your job is.]

[Output format — be exact:]
FIELD_A: <description>
FIELD_B: <description>

[Optional: one or two constraints, e.g. "No extra text." or "Under 30 words."]
```

### Good agent — `event.agent.md` (example 05)

```markdown
---
name: event
---

You receive cuts of a JSON event payload. Produce one short paragraph
(max 3 sentences) explaining, in plain English, what happened and what an
on-call operator should do about it. Do not quote the JSON; summarize.
```

### Good agent — `triage.agent.md` (example 06)

```markdown
---
name: triage
---

You are a pull-request triage agent. Leather will deliver PR thread content in
paged hide cuts when the input is large. While pages are still being delivered,
respond only with the requested page facts. After Leather explicitly says all
pages have been read, produce your triage note.

Produce a compact structured note with exactly these fields, one per line, no
extra commentary:

```
INTENT: <one sentence: what the PR is trying to do>
RISK:   <low|medium|high> — <one short reason>
AREAS:  <comma-separated touched subsystems>
FLAGS:  <comma-separated concerns>
```

If any field is unclear, write `unknown`. Keep total output under 200 words.
```

### Tool-using agent

```markdown
---
name: pr-diff
skills: [github-read]
tool_rounds: 2
---

You receive a GitHub pull_request webhook payload. Extract PR_NUMBER and REPO,
call get_pr_diff, then write:

LOGIC_CHANGES:
  <one line per meaningful change>
SIGNALS: <comma-separated: model-weights, api-surface, docs-only, ...>

The diff may be truncated; reason from what is visible. No extra text.
```

The agent knows to call the tool because the skill's `system_prompt_append`
tells it which tools are available and what they do. The agent body just says
*call it and report*. No numbered steps.

### Multi-turn agent (per-turn tool scoping)

Use `---` to separate turns. Each turn may narrow the tool scope:

```markdown
---
name: release-agent
toolsets: [release-read]
---

You are a release assistant.

---
toolsets: [release-write]
Tag the release now.

---
toolsets: [release-verify]
Confirm the tag is present on origin.
```

Per-turn declarations **replace** (not extend) the base scope for that turn.
Use this to restrict risky operations (write tools off for read turns, etc.).

### Anti-patterns

| Anti-pattern | Fix |
|---|---|
| `STEP 1 — do X. STEP 2 — do Y.` | Remove the numbering. Describe the job in one sentence + output format. |
| 20-line system prompt | Trim to role + format. Move operational params to lifecycle file. |
| Two unrelated tasks in one agent | Split into two agents, connect via output queue. |
| Asking the agent to "decide what to do" without constraints | Enumerate the decision tree explicitly (see `decision.agent.md`, example 10). |

---

## 4. Lifecycle — `*.lifecycle.yaml`

Operational parameters that rotate independently of the system prompt.

```yaml
agent: agent-name
schedule: "0 9 * * *"    # cron; or "once" for one-shot
model: my-model
max_tokens: 4096
timeout: 120s
temperature: 0.7
enabled: true

# Optional: prompt override (injected as the user turn)
prompt: |
  Summarize today's tasks in three bullets.

# Optional: parameterized prompt
parameters:
  report_date: "{{now}}"
  team: platform

# Optional: tool config
skills: [extra-skill]
tool_rounds: 3

# Optional: queue input (agent runs once per dequeued item)
queue_input: my-queue
queue_batch_size: 1
queue_max_attempts: 3

# Optional: output routing
output:
  queue: next-queue-name
  notify: [telegram-ops]

# Optional: response cache
cache:
  ttl: 3600
  key: "daily-{{parameters.report_date}}"
```

**Use the lifecycle file for:** schedule, model, temperature, `prompt`,
`parameters`, output routing, cache.

**Use the agent file for:** the system prompt (the *what*). Keep it stable.

### Multiple instances from one file

```yaml
agent: heartbeat
instances:
  - name: morning
    schedule: "0 9 * * 1-5"
    model: fast-model
  - name: evening
    schedule: "0 18 * * 1-5"
    model: fast-model
    prompt: End-of-day summary.
```

---

## 5. Shell tools — `shell-tools.json`

`shell-tools.json` is the manifest consumed by the `shell-mcp` binary. Each
entry becomes one callable tool.

```json
{
  "tools": [
    {
      "name": "git-log",
      "description": "Recent git history. Returns last 20 commits, one per line.",
      "args": [
        { "name": "ref", "type": "string", "required": false, "default": "HEAD",
          "description": "Branch, tag, or SHA." }
      ],
      "exec": {
        "argv": ["git", "log", "--oneline", "-n", "20", "{{ref}}"]
      },
      "cwd": "{{env.PROJECT_ROOT}}",
      "timeout": "5s"
    },
    {
      "name": "get-pr-diff",
      "description": "Fetch unified diff for a GitHub PR (first 4000 bytes).",
      "args": [
        { "name": "pr_number", "type": "int",    "required": true },
        { "name": "repo",      "type": "string", "required": true,
          "description": "owner/repo" }
      ],
      "exec": {
        "shell": "gh pr diff {{pr_number}} -R {{repo|shq}} | head -c 4000"
      },
      "timeout": "30s"
    }
  ]
}
```

### `exec.argv` vs `exec.shell`

| Form | When to use |
|---|---|
| `exec.argv` | Single command, no pipes, no shell expansion needed. Safest. |
| `exec.shell` | Need pipes, `&&`, subshells, or shell builtins. Args are shell-quoted automatically (`{{name}}` → `{{name\|shq}}`). |

**Prefer `exec.argv`.** Never add a Python/Node interpreter just to chain shell
commands — use `bash -c` with positional args instead:

```json
"exec": {
  "argv": ["bash", "-c", "cd \"$1\" && git status", "--", "{{path}}"]
}
```

### Arg types and quoting

- `string` args in `exec.argv` are passed as single elements — no quoting needed.
- `string` args in `exec.shell` are shell-quoted automatically.
- `int` and `bool` args are interpolated as-is (no quoting).
- `{{env.VAR}}` reads from the server process environment.

### Naming convention

Tool names use `kebab-case`. Match the `name` here to the `name` in your
`*.skill.yaml` tool list.

---

## 6. MCP servers — `mcp-servers.yaml`

Registers MCP servers that leather connects to at startup.

```yaml
servers:
  - name: shell
    command: /path/to/shell-mcp /path/to/shell-tools.json
    transport: stdio

  - name: github-mcp
    command: github-mcp-server
    transport: stdio
    env:
      GITHUB_TOKEN: "{{env:GITHUB_TOKEN}}"
```

The `name` field is how skills reference this server (`mcp.server: shell`).
Use relative paths to `shell-mcp` when running from an example directory
(e.g., `../../shell-mcp`).

---

## 7. Skills — `*.skill.yaml`

A skill is **tools + a brief prompt fragment** that tells the agent how to
use those tools. Put skills in `tool_dir` (default: `tools/`).

```yaml
name: github-read
system_prompt_append: |
  You have two GitHub read tools. Extract pr_number and repo from your
  input before calling them:

    get_pr_files  → list changed files with per-file line stats
    get_pr_diff   → fetch unified diff (first 4000 bytes)

  If a tool fails, reason from the input and still produce your output.

tools:
  - name: get_pr_files
    description: List files changed in a PR with per-file line stats.
    type: mcp
    mcp:
      server: shell
      tool: get_pr_files

  - name: get_pr_diff
    description: Fetch the unified diff for a PR (first 4000 bytes).
    type: mcp
    mcp:
      server: shell
      tool: get_pr_diff
```

### Skill `system_prompt_append`

Keep it short. The append should:
1. Name the tools and their purpose (one line each).
2. State any preconditions for calling them (e.g., "extract X from input first").
3. Give a failure fallback ("if a tool fails, reason from what you have").

Do not repeat information already in the agent's system prompt.

### Toolset (tools only, no prompt)

When you want to expose tools without adding prompt fragments, use a toolset:

```yaml
# tools/release-read.toolset.yaml
name: release-read
tools:
  - name: list-tags
    description: List all git tags.
    type: mcp
    mcp:
      server: shell
      tool: list-tags
```

Use toolsets when the agent's own prompt already explains the tools, or when
you need per-turn scope without extra prompt injection.

---

## 8. Curings — `*.curing.yaml`

A curing is the queue-to-agent-to-artifact binding. One file per curing.
Put curings in `curing_dir` (default: `curings/`).

```yaml
name: event-summary
description: Summarize an inbound event payload.
agent: event
queue: event-in
hide_types:
  - github.issues
  - github.pull_request
page_size_bytes: 4000       # truncate hide to this size per cut
max_attempts: 2             # retries before DLQ
timeout_seconds: 120

output:                     # optional
  queue: summary-out
  notify: [telegram-ops]
```

### Fields

| Field | Purpose |
|---|---|
| `name` | Unique curing identifier. Must match the route's `curing:` field in tannery.yaml. |
| `agent` | Agent name. Must match the `name:` in the corresponding `*.agent.md`. |
| `queue` | Input queue name. Must be declared in tannery.yaml `queues:`. |
| `hide_types` | Semantic hide kinds this curing handles. Leave empty (`[]`) for collect-only curings. |
| `page_size_bytes` | Max bytes per hide cut sent to the agent. Smaller = more paging turns. |
| `max_attempts` | Retry count before the item is moved to `<queue>-dlq`. |
| `timeout_seconds` | Per-run wall-clock timeout. |
| `collect_size` | Fan-in: wait for this many queue items (grouped by `collect_by`) before running. |
| `collect_by` | Fan-in grouping key. Currently: `correlation_id`. |
| `output.queue` | Enqueue the agent's response to this queue after completion. |
| `output.notify` | Send artifact to these notify backends after completion. |

### Fan-in collect curing

Used in multi-agent fan-out pipelines. The decision agent waits for all N
parallel analysis results before running:

```yaml
name: decision
agent: decision
queue: analysis-out
hide_types: []           # no direct hide input
collect_size: 3          # wait for 3 items
collect_by: correlation_id
max_attempts: 2
timeout_seconds: 120
output:
  queue: comments-in
```

---

## 9. Tannery — `tannery.yaml`

The tannery configuration wires webhook endpoints to routes, routes to
curings, and curings to queues.

```yaml
hide_dir:     .state/hides
artifact_dir: .state/artifacts
curing_dir:   curings

webhooks:
  - name: github
    path: /webhooks/github
    source: github
    secret: "{{env:GITHUB_WEBHOOK_SECRET}}"
    max_body_bytes: 65536     # 64 KB default; increase for large payloads

routes:
  - name: route-name
    match:
      source: github
      event_type: pull_request   # optional; omit for source-only match
    hide_kind: github.pull_request
    curing: curing-name
    queue: queue-name

queues:
  queue-name:
    concurrency: 1
    max_attempts: 2
    max_depth: 50
```

### Route field order

Convention (matching examples 04–10):

```yaml
  - name: route-name
    match:               # ← matching criteria first
      source: ...
      event_type: ...
    hide_kind: ...       # ← then what kind of hide to write
    curing: ...          # ← then where to send it
    queue: ...
```

### Match rules

- `source:` alone → matches any event from that source (catch-all/fallback).
- `source:` + `event_type:` → matches only that specific event type.
- Routes are evaluated in order; all matching routes are executed (fan-out).

### Queue configuration

```yaml
queues:
  high-priority-in:
    concurrency: 2       # parallel workers on this queue
    max_attempts: 3      # retries before DLQ (overrides curing's max_attempts)
    max_depth: 200       # backpressure: 503 when queue exceeds this depth
```

`max_depth` is the primary backpressure knob. When a webhook arrives and
any destination queue is at `max_depth`, the webhook returns HTTP 503
before writing the hide.

---

## 10. Recipes

### Recipe: Scheduled agent

**Use case:** Run an agent on a cron schedule. No external input.

Files:

```
agents/daily-summary.agent.md
agents/daily-summary.lifecycle.yaml
config.yaml
```

**`agents/daily-summary.agent.md`**

```markdown
---
name: daily-summary
---

You are a system heartbeat agent. Each time you are invoked, respond with a
single sentence describing one interesting fact about software engineering,
distributed systems, or operating systems. Be specific and concise — no
filler. Under 30 words.
```

**`agents/daily-summary.lifecycle.yaml`**

```yaml
agent: daily-summary
schedule: "0 9 * * 1-5"
model: my-model
max_tokens: 512
output:
  notify: [telegram-ops]
```

**Run:** `leather serve --config config.yaml`

---

### Recipe: Shell-tool agent

**Use case:** Agent needs to call shell commands (git, gh, curl).

Files:

```
agents/inspector.agent.md
tools/repo.skill.yaml
shell-tools.json
mcp-servers.yaml
config.yaml
```

**`agents/inspector.agent.md`**

```markdown
---
name: inspector
skills: [repo]
---

You are a repository inspector. Use the `repo` skill's tools to look at the
local repository. Produce a short status report:

1. Current branch and working-tree state.
2. The five most recent commits.
3. The list of local branches.

Be concise; use markdown bullets.

---

Inspect the repository and produce the status report.
```

**`tools/repo.skill.yaml`**

```yaml
name: repo
system_prompt_append: |
  You have shell tools for inspecting a git repository:
    git-status  → current branch and working-tree state
    git-log     → recent commits
    git-branch  → list of local branches

tools:
  - name: git-status
    description: Current branch and working-tree state.
    type: mcp
    mcp:
      server: shell
      tool: git-status

  - name: git-log
    description: Last 20 commits, one per line.
    type: mcp
    mcp:
      server: shell
      tool: git-log

  - name: git-branch
    description: List all local branches.
    type: mcp
    mcp:
      server: shell
      tool: git-branch
```

**`shell-tools.json`**

```json
{
  "tools": [
    {
      "name": "git-status",
      "description": "Current branch and working-tree state.",
      "exec": { "argv": ["git", "status", "--short", "--branch"] },
      "timeout": "5s"
    },
    {
      "name": "git-log",
      "description": "Recent 20 commits.",
      "exec": { "argv": ["git", "log", "--oneline", "-n", "20"] },
      "timeout": "5s"
    },
    {
      "name": "git-branch",
      "description": "Local branches.",
      "exec": { "argv": ["git", "branch"] },
      "timeout": "5s"
    }
  ]
}
```

**`mcp-servers.yaml`**

```yaml
servers:
  - name: shell
    command: /usr/local/bin/shell-mcp ./shell-tools.json
    transport: stdio
```

**Run:** `leather chat --config config.yaml --agent agents/inspector.agent.md`

---

### Recipe: Webhook → single curing

**Use case:** GitHub webhook fires → agent processes payload → artifact written.

Files:

```
agents/event.agent.md
curings/event-summary.curing.yaml
tannery.yaml
config.yaml
mcp-servers.yaml
```

**`agents/event.agent.md`**

```markdown
---
name: event
---

You receive cuts of a JSON event payload. Produce one short paragraph
(max 3 sentences) explaining what happened and what an on-call operator
should do. Do not quote the JSON; summarize.
```

**`curings/event-summary.curing.yaml`**

```yaml
name: event-summary
description: Summarize inbound GitHub events.
agent: event
queue: event-in
hide_types:
  - github.issues
  - github.pull_request
page_size_bytes: 4000
max_attempts: 2
timeout_seconds: 120
output:
  notify: [telegram-ops]
```

**`tannery.yaml`**

```yaml
hide_dir: .state/hides
artifact_dir: .state/artifacts
curing_dir: curings

webhooks:
  - name: github
    path: /webhooks/github
    source: github
    secret: "{{env:GITHUB_WEBHOOK_SECRET}}"
    max_body_bytes: 65536

routes:
  - name: github-events
    match:
      source: github
    hide_kind: github.event
    curing: event-summary
    queue: event-in

queues:
  event-in:
    concurrency: 1
    max_attempts: 2
    max_depth: 100
```

**Run:** `leather serve --config config.yaml --tannery tannery.yaml`

**Test:** `curl -X POST http://127.0.0.1:7749/webhooks/github -d @sample.json`

---

### Recipe: Two-agent pipeline

**Use case:** Agent A produces a structured note; agent B turns it into a
human-readable summary. Connected via `output.queue`.

This is the pattern from example 06 (`triage` → `summarize`).

```
event-in → [triage agent] → triage-out → [summarize agent] → artifact
```

**`agents/triage.agent.md`**

```markdown
---
name: triage
---

You are a pull-request triage agent. Produce a structured note:

INTENT: <one sentence: what the PR does>
RISK:   <low|medium|high> — <one short reason>
AREAS:  <comma-separated subsystems>
FLAGS:  <comma-separated concerns>

If any field is unclear, write `unknown`. Under 200 words.
```

**`agents/summarize.agent.md`**

```markdown
---
name: summarize
---

You receive a structured triage note (INTENT/RISK/AREAS/FLAGS). Turn it into
one short paragraph (max 4 sentences) a busy reviewer can read in 15 seconds.
End with one of: "Approve", "Request changes", or "Escalate".
```

**`curings/triage.curing.yaml`**

```yaml
name: triage
agent: triage
queue: triage-in
hide_types: [github.pull_request]
page_size_bytes: 6000
max_attempts: 2
timeout_seconds: 120
output:
  queue: summary-in
```

**`curings/summarize.curing.yaml`**

```yaml
name: summarize
agent: summarize
queue: summary-in
hide_types: []
page_size_bytes: 4000
max_attempts: 2
timeout_seconds: 120
output:
  notify: [telegram-ops]
```

---

### Recipe: Event routing (multi-route)

**Use case:** Different event types → different agents, with a catch-all.

Pattern from example 07.

```yaml
routes:
  - name: review-comments
    match:
      source: github
      event_type: pull_request_review_comment
    hide_kind: github.pull_request_review_comment
    curing: review-comment
    queue: review-in

  - name: issues
    match:
      source: github
      event_type: issues
    hide_kind: github.issues
    curing: issue-event
    queue: issue-in

  - name: deploy-alerts
    match:
      source: github
      event_type: deployment_status
    hide_kind: github.deployment_status
    curing: escalation
    queue: escalation-in

  # Catch-all: any github event not matched above.
  - name: github-fallback
    match:
      source: github
    hide_kind: github.untyped_event
    curing: issue-event
    queue: issue-in
```

Key points:
- Specific `event_type` routes come before the catch-all (source-only match).
- All matching routes are executed (fan-out), not just the first match.
- The catch-all uses `match: { source: github }` with no `event_type`.

---

### Recipe: Fan-out / fan-in

**Use case:** One event triggers N parallel agents. A join agent waits for all
N results before running. Pattern from example 10.

```
webhook → [router, fan-out all 3 routes, same hide_id]
        → pr-metadata-in  → [pr-metadata agent]  → analysis-out
        → pr-diff-in      → [pr-diff agent]       → analysis-out
        → pr-context-in   → [pr-context agent]    → analysis-out
                                                          ↓
        analysis-out [collect_size: 3, collect_by: correlation_id]
                                                          ↓
                                               [decision agent]
                                                          ↓
                                               → comments-in → [pr-comments agent]
```

**Tannery routes (3 routes, same source+event_type → fan-out):**

```yaml
routes:
  - name: pr-metadata
    match:
      source: github
      event_type: pull_request
    hide_kind: github.pull_request
    curing: pr-metadata
    queue: pr-metadata-in

  - name: pr-diff
    match:
      source: github
      event_type: pull_request
    hide_kind: github.pull_request
    curing: pr-diff
    queue: pr-diff-in

  - name: pr-context
    match:
      source: github
      event_type: pull_request
    hide_kind: github.pull_request
    curing: pr-context
    queue: pr-context-in
```

All three routes match the same event. The handler writes **one shared hide**
(`hide_id`) and enqueues one item per route. Every item carries the same
`correlation_id` (= the shared `hide_id`), which the collect curing uses
to group results.

**Fan-out curing (all three are the same shape):**

```yaml
name: pr-metadata
agent: pr-metadata
queue: pr-metadata-in
hide_types: [github.pull_request]
page_size_bytes: 4000
max_attempts: 2
timeout_seconds: 120
output:
  queue: analysis-out
```

**Fan-in (collect) curing:**

```yaml
name: decision
agent: decision
queue: analysis-out
hide_types: []
collect_size: 3          # wait for all 3 parallel results
collect_by: correlation_id
max_attempts: 2
timeout_seconds: 120
output:
  queue: comments-in
```

The decision agent receives all three analysis blocks concatenated with
`--- ANALYSIS N (from: <curing>) ---` delimiters.

**Decision agent:**

```markdown
---
name: decision
---

You receive analysis blocks from three parallel agents, each delimited by
"--- ANALYSIS N (from: <agent>) ---".

FULL_EVAL if any CONCERN_PATHS touch evals/, models/, datasets/, inference/,
or api/, or any SIGNAL is: model-weights, decoder-params, eval-baseline.
SKIP if all signals are: docs-only, ci-config, dependency-bump, formatting-only.
When in doubt: FULL_EVAL.

Copy PR_NUMBER, REPO, SHA verbatim from ANALYSIS 1 and write:

PR_NUMBER: <number>
REPO:      <full_name>
SHA:       <sha>
Decision:  FULL_EVAL | SKIP
Rationale: <2-3 sentences citing specific files or signals>
Files of concern:
  <filename>  +<add> -<del>  -- <why>
  (or "none")
```

---

### Recipe: Dead-letter queue

**Use case:** Demonstrate or handle retry exhaustion.

```yaml
# tannery.yaml queues block
queues:
  fail-in:
    concurrency: 1
    max_attempts: 2    # try once, retry once, then DLQ
    max_depth: 50
```

```yaml
# curings/fail-demo.curing.yaml
name: fail-demo
agent: always-timeout
queue: fail-in
hide_types: [demo.failure]
max_attempts: 2
timeout_seconds: 120
```

```markdown
# agents/always-timeout.agent.md
---
name: always-timeout
timeout: 1ms
---

You are intentionally configured to time out immediately.
This demonstrates DLQ behavior: retry once (max_attempts=2),
then route the item to fail-in-dlq.
```

After `max_attempts` retries the item lands in `<queue>-dlq` (e.g.,
`fail-in-dlq`). Inspect with:

```bash
curl http://127.0.0.1:7749/queues/fail-in-dlq
```

Requeue for re-processing:

```bash
curl -X POST http://127.0.0.1:7749/queues/fail-in/requeue
```

---

### Recipe: Notify on artifact

**Use case:** Send agent output to Telegram (or another backend) after curing.

**`config.yaml` — notify block:**

```yaml
notify:
  backends:
    - name: telegram-ops
      type: telegram
      chat_id: "123456789"
      token:
        pass: telegram/bot_token
```

**`curings/summary.curing.yaml` — output.notify:**

```yaml
output:
  notify: [telegram-ops]
```

The artifact (agent response) is sent to each listed backend after the curing
run completes. If a backend is misconfigured, the run still succeeds — the
notify failure is logged as a warning and does not affect the queue item.

**Ingest a hide manually for testing:**

```bash
leather ingest --config config.yaml \
  --hide-kind github.issues \
  --queue event-in \
  sample/input.json
```

---

## Appendix: File layout reference

```
my-project/
  config.yaml              # required
  mcp-servers.yaml         # required if using shell-mcp or MCP tools
  shell-tools.json         # required if using shell-mcp
  tannery.yaml             # required for webhook/tannery mode

  agents/
    my-agent.agent.md      # agent definition
    my-agent.lifecycle.yaml

  tools/
    my-skill.skill.yaml    # skills (tools + prompt fragment)
    my-toolset.toolset.yaml

  curings/
    my-curing.curing.yaml

  .state/                  # runtime state (git-ignored)
    hides/
    artifacts/
    queues/
    runs/
```

## Appendix: Queue naming conventions

| Pattern | Meaning |
|---|---|
| `<topic>-in` | Fan-out input queue (e.g., `pr-diff-in`) |
| `<topic>-out` | Intermediate output / fan-in queue (e.g., `analysis-out`) |
| `<topic>-dlq` | Dead-letter queue (auto-created by runtime) |

## Appendix: `leather` subcommands

| Command | Purpose |
|---|---|
| `leather init` | Scaffold `~/.leather` with `.env`, `config.yaml`, an example agent, and a `Makefile`. |
| `leather doctor` | Print every effective config value with source attribution; redacts secrets. |
| `leather serve` | Start scheduler + HTTP API + optional tannery. |
| `leather run` | Execute one agent once and exit. |
| `leather chat` | Interactive multi-turn chat with an agent. |
| `leather validate` | Parse and schema-check all files; report errors. |
| `leather ingest` | Write a file as a hide and optionally enqueue it. |
| `leather workflow` | Run bounded one-shot tannery workflows to queue quiescence. |
| `leather status` | Print job history, token usage, scheduler state. |
| `leather test-agent` | Run an agent against `MockLLM` and print the transcript. |
| `leather snapshot` | Save or restore a point-in-time `tar.gz` archive of runtime state. |
| `leather dlq` | Inspect and requeue outbound dead-letter queue items. |
| `leather attach` | Join a running `serve` instance and stream pretty-printed runtime events. |
| `leather replay` | Replay a snapshot or live session. |
| `leather version` / `leather help` | The obvious. |
