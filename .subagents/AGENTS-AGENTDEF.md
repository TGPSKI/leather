# AGENTS-AGENTDEF.md — leather agent definition format spec

Subagent guide for the **user-facing agent definition format**:
`*.agent.md`, `*.lifecycle.yaml`, multi-turn turn blocks, front-matter
fields, filename conventions, and per-turn tool scope. This is the
**authoring spec** that agent authors and operators read.

Load this guide when:

- Adding, removing, or renaming a front-matter or lifecycle field
- Changing the multi-turn block syntax
- Updating filename conventions
- Documenting variable substitution rules
- Writing examples or tutorials

For the *internal* loader implementation
(`internal/agent`, `internal/model`), see
[AGENTS-CORE.md](AGENTS-CORE.md). For tool / skill / toolset
**semantics** (resolution rules, precedence, composition), see
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).
For demo agents to mirror, see `tanning/agents/` and (when it lands)
[AGENTS-EXAMPLES.md](AGENTS-EXAMPLES.md). For runtime execution, see
[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md). For scheduling semantics, see
[AGENTS-WORKER.md](AGENTS-WORKER.md).

---

## What an agent definition is

A pair of files:

| File | Role |
|---|---|
| `*.agent.md` | Identity + system prompt (Markdown). |
| `*.lifecycle.yaml` | Operational configuration (preferred for schedule, model, parameters). |

The Markdown file holds **what the agent is**; the YAML file holds
**how the agent runs**. Either may carry both for simple cases, but
separating them keeps stable instructions decoupled from rotating
operational knobs.

Files live under the directory set by `--agent-dir`
(`LEATHER_AGENT_DIR`, default `~/.leather/agents/`).

---

## File format — `*.agent.md`

```markdown
---
name: my-agent
schedule: "0 9 * * *"   # optional here if lifecycle file provides it
model: llama3           # optional here if lifecycle file provides it
tags: [daily]
---

You are a concise assistant. Summarize today's tasks in three bullets.
```

The leading `---` block is YAML front matter. The body after the
closing `---` is the **system prompt**, trimmed of leading and trailing
whitespace.

### Front-matter fields

#### Required

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique agent identifier. Used as the job name when no lifecycle file overrides it. |

#### Optional (prefer the lifecycle file)

| Field | Type | Default | Description |
|---|---|---|---|
| `schedule` | string | — | Cron expression or `"once"`. Required somewhere (here or lifecycle). |
| `model` | string | — | LLM model name. Required somewhere. |
| `max_tokens` | int | `LEATHER_MAX_TOKENS` | Token budget override. |
| `timeout` | duration | `LEATHER_LLM_TIMEOUT` | Per-call timeout. |
| `temperature` | float | `0.7` | Sampling temperature. |
| `enabled` | bool | `true` | Set `false` to disable without deleting. |
| `tool_rounds` | int | `Config.MaxToolRounds` | Per-agent override of max tool-call cycles. |
| `queue_input` | string | — | Queue name whose payload items feed prompt-template substitution. |
| `skills` | []string | `[]` | Base skill bundles loaded for this agent. |
| `toolsets` | []string | `[]` | Base named tool exposure sets. |
| `tools` | []string | `[]` | Inline base tool names (rarely needed; prefer toolsets). |
| `tags` | []string | `[]` | Metadata labels for filtering. |

Unknown front-matter keys are silently ignored for forward
compatibility. A missing `name` is a hard error.

### Body

The Markdown after the closing `---` is the system prompt verbatim.

### Multi-turn body

When the body contains `---` separators, the **first section** becomes
the system prompt; each later section becomes one user turn.

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
Verify the tag is present on origin.
```

#### Per-turn declarations

A turn may begin with any combination of:

| Declaration | Effect |
|---|---|
| `skills: [name1, name2]` | This turn resolves tools from the listed skills. |
| `toolsets: [setA]` | This turn resolves tools from the listed toolsets. |
| `tools: [t1, t2]` | This turn exposes only the named tools. |

When any of these appear, the turn's tool exposure replaces (not
augments) the base agent scope. Resolution semantics are governed by
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).

The text following the declarations is the user-turn content,
whitespace-trimmed.

---

## File format — `*.lifecycle.yaml`

```yaml
agent: daily-summary
schedule: "0 9 * * *"
model: llama3
max_tokens: 4096
tags: [daily]
prompt: |
  Summarize today's tasks in three bullets.
```

**YAML fields are always authoritative.** The filename is a
human-readable convention only and is never parsed.

### Filename conventions

| Pattern | Meaning |
|---|---|
| `{agent-name}.lifecycle.yaml` | Singleton lifecycle for `agent-name`. |
| `{instance-name}.{agent-name}.lifecycle.yaml` | Named instance of `agent-name`. |

### Required fields (flat / singleton form)

| Field | Type | Description |
|---|---|---|
| `agent` | string | Name of the agent definition this lifecycle applies to. Must match the `name` in the paired `*.agent.md`. |
| `schedule` | string | Cron expression or `"once"`. (Per-instance in list form.) |
| `model` | string | LLM model name. (Per-instance in list form.) |

### Optional fields

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | `agent` value | Job-name override (singleton only). |
| `enabled` | bool | `true` | Set `false` to suspend without deleting. |
| `max_tokens` | int | `LEATHER_MAX_TOKENS` | Token budget override. |
| `timeout` | duration | `LEATHER_LLM_TIMEOUT` | Per-call timeout. |
| `temperature` | float | `0.7` | Sampling temperature. |
| `prompt` | string | — | Single user prompt override. |
| `prompts` | []string | `[]` | Ordered user-turn chain; replaces body-derived turns when non-empty. |
| `parameters` | map | `{}` | Named prompt variables, substituted into `prompt`/`prompts` via `{{varname}}`. |
| `skills` | []string | `[]` | Additive base skill bundles. |
| `toolsets` | []string | `[]` | Additive base named tool exposure sets. |
| `tools` | []string | `[]` | Additive inline tool names. |
| `tool_rounds` | int | `Config.MaxToolRounds` | Override the tool-call/result cycle limit. |
| `queue_input` | string | — | Queue name to dequeue payloads from at run time. |
| `queue_batch_size` | int | `1` | Dequeue this many items per run. |
| `queue_max_attempts` | int | `3` | Per-item retry cap before drop. |
| `cache` | block | — | See "Cache block" below. |
| `output` | block / list | — | See "Output routes" below. |
| `hooks` | block | — | See "Hooks block" below. |
| `tags` | []string | `[]` | Metadata labels. |

### List form — N jobs from one file

```yaml
agent: daily-summary
instances:
  - name: morning
    schedule: "0 9 * * *"
    model: llama3
  - name: evening
    schedule: "0 21 * * *"
    model: llama3-70b
    temperature: 0.5
    parameters:
      mood: reflective
```

Each instance must have `name`, `schedule`, and `model`. Per-instance
`skills` / `toolsets` append to top-level values; per-instance scalars
override.

### Cache block

```yaml
cache:
  enabled: true
  ttl: 1h
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Cache LLM responses keyed by prompt SHA-256. |
| `ttl` | duration | `0` | `0` = never expire. |

### Output routes

```yaml
output:
  - type: file
    path: ~/notes/daily.md
  - type: queue
    queue: review-queue
  - type: notify
    backend: telegram
  - type: http
    url: https://example.com/webhook
    method: POST
```

`output:` accepts a single block or a list. Supported `type:` values:
`file`, `queue`, `http`, `notify`.

### Hooks block

```yaml
hooks:
  pre_run: echo "starting {{agent_name}}"
  post_success: notify-send "{{agent_name}} done"
  post_error: logger -t leather "{{agent_name}} failed: {{error}}"
```

Hooks run as the leather process user with a 10 s hard cap.
Hook stdout/stderr is captured to the debug log and **never** forwarded
to the model. Hook failures do not affect job status.

---

## Variable substitution

Supported placeholders in `prompt`, `prompts[]`, and `hooks`:

| Variable | Source |
|---|---|
| `{{agent_name}}` | The resolved job/instance name. |
| `{{schedule}}` | Cron expression for this instance. |
| `{{now}}` | Current time formatted `2006-01-02 15:04:05`. |
| `{{tags}}` | Comma-joined tag list. |
| `{{<param>}}` | Any key from `parameters:` in the lifecycle file. |
| `{{error}}` | Only inside `hooks.post_error`. |

Unknown placeholders are left as literal text. Operators get a `WARN`
log line listing any unresolved placeholder per run.

---

## Skill, toolset, and inline tool YAML — pointer

The three companion file types live under `--tool-dir`
(`LEATHER_TOOL_DIR`):

```
{name}.skill.yaml      — bundle of (tools + prompt fragment + parameters)
{name}.toolset.yaml    — named group of tool names only (no prompt)
{name}.tool.yaml       — single callable tool definition
```

The **format** of each is owned by this guide; the **resolution
semantics** (precedence, naming collisions, visibility per turn,
composition) are owned by
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).

### `*.skill.yaml` minimal example

```yaml
name: release-write
prompt: |
  You can create and push git tags.
tools:
  - shell-git/tag
  - shell-git/push
parameters:
  - name: ref
    description: git ref to tag
    required: true
```

### `*.toolset.yaml` minimal example

```yaml
name: release-read
tools:
  - shell-git/log
  - shell-git/status
  - shell-git/diff
```

### `*.tool.yaml` minimal example

```yaml
name: shell-git/log
type: http
url: http://localhost:7752/git-log
method: POST
output_file: ./.leather/runs/git-log.txt
```

Field reference for each kind is reproduced in
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).

---

## Merge precedence — agent + lifecycle

The loader runs four phases (owned by
[AGENTS-CORE.md](AGENTS-CORE.md)). The user-visible outcome:

1. **`*.agent.md`** front matter and body parsed.
2. **`*.lifecycle.yaml`** (if present) applied; lifecycle wins on
   conflicts.
3. **List form** lifecycle expands to N jobs sharing the agent
   definition; per-instance values override top-level lifecycle values.
4. Agents missing a `schedule` in either file **fail validation**.
   Duplicate job names across all lifecycle records **fail validation**.

A lifecycle file referencing an unknown agent name is reported as an
error and skipped.

---

## Validation rules

`leather validate` reports failures for:

- Missing required field (`name`, `schedule`, `model` somewhere).
- Invalid cron expression.
- Unknown `output.type` value.
- `enabled: false` is valid; the agent is loaded but never registered.
- `parameters:` keys not used in any prompt are warnings, not errors.

`leather validate` also validates `config.yaml`, `mcp-servers.yaml`,
`*.skill.yaml`, `*.toolset.yaml`, `*.tool.yaml`, and `*.worker.yaml`
against their schemas in `schemas/`.

---

## Authoring patterns

### "Just the prompt", inline scheduling

```markdown
---
name: standup
schedule: "0 8 * * 1-5"
model: llama3
---

Three bullets: yesterday, today, blockers.
```

Single file; no lifecycle YAML needed.

### Stable instructions, rotating schedule

```markdown
<!-- standup.agent.md -->
---
name: standup
---

Three bullets: yesterday, today, blockers.
```

```yaml
# standup.lifecycle.yaml
agent: standup
schedule: "0 8 * * 1-5"
model: llama3
```

Edit the schedule without touching the prompt.

### Multiple instances of one agent

```yaml
# standup.lifecycle.yaml
agent: standup
instances:
  - name: morning-standup
    schedule: "0 8 * * 1-5"
    model: llama3
  - name: friday-retro
    schedule: "0 17 * * 5"
    model: llama3-70b
    parameters:
      focus: retrospective
```

### Multi-turn release flow

See `tanning/agents/go-release-prep.agent.md` and
`tanning/agents/go-release-tag.agent.md` for working examples that
exercise per-turn skills and toolsets.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Putting `schedule:` in neither file | Validation fails. Put it in lifecycle (preferred) or front-matter. |
| Hard-coding API tokens in `parameters:` | Tokens belong in `notify` blocks via `env:` / `pass:` refs, not as prompt parameters. |
| Naming a lifecycle file `whatever.yaml` (no `lifecycle`) | The loader only picks up `*.lifecycle.yaml`. |
| `agent:` in lifecycle differs from `name:` in `*.agent.md` | Loader rejects with "unknown agent" error. |
| Expecting per-turn declarations to *augment* base tool scope | They **replace** base scope for that turn. |
| Body uses `---` accidentally inside content | Use a YAML block scalar or escape; `---` at column 0 is a turn boundary. |

---

## Cross-cutting links

- [AGENTS-CORE.md](AGENTS-CORE.md) — internal loader implementation
  and type definitions.
- [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md) —
  resolution semantics for `skills` / `toolsets` / `tools`.
- [AGENTS-WORKER.md](AGENTS-WORKER.md) — cron expression semantics,
  `"once"` jobs, DST handling.
- [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md) — what executes after load.
- [AGENTS-SECURITY.md](AGENTS-SECURITY.md) — secret reference syntax.

---

## Verification checklist

Before opening a PR that changes the agent-definition format:

- [ ] New field documented in the appropriate table in this file
- [ ] Loader test in `internal/agent` covers the new field
- [ ] `leather validate` reports clear errors for invalid input
- [ ] `tanning/agents/` updated with at least one example using the new
      field (when the field is user-visible)
- [ ] [ROADMAP.md](../ROADMAP.md) entry marked completed if this PR
      closes one
- [ ] Filename conventions table updated when changed
- [ ] Variable-substitution table updated when adding a placeholder
- [ ] Cross-link to AGENTS-TOOLS-SKILLS-TOOLSETS.md remains accurate

---

_Last reviewed: 2026-05-19_
