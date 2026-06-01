# AGENTS-TOOLS-SKILLS-TOOLSETS.md — leather tool & skill semantics

Subagent guide for **how leather resolves tools, skills, and toolsets**
at agent-load time and per turn: definitions, precedence, naming and
collision rules, composition, and visibility scoping.

Load this guide when:

- Adding or modifying the precedence rules for `tools` / `toolsets` / `skills`
- Changing how per-turn declarations interact with base agent scope
- Resolving a naming-collision question (`shell/git-log` vs `git-log`)
- Documenting how a new tool source (HTTP, MCP, future) plugs in
- Writing or reviewing a new skill bundle

For the **file-format** spec of `*.skill.yaml`, `*.toolset.yaml`,
`*.tool.yaml`, see [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md). For the
runtime execution loop, see [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md).
For the `shell-mcp` companion, see [AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md).
For the loader internals, see [AGENTS-CORE.md](AGENTS-CORE.md).

---

## Definitions

| Term | What it is | Where it lives | Owns |
|---|---|---|---|
| **Tool** | A single callable: HTTP request or MCP-bound tool. | `*.tool.yaml` (inline) or registered via MCP server. | Name, executor config, parameter schema. |
| **Toolset** | A named bundle of **tool names only**. No prompt fragment. | `*.toolset.yaml`. | Composition; collision-free grouping. |
| **Skill** | A bundle of tools **plus** a prompt fragment and (optional) parameters. | `*.skill.yaml`. | The "you can do X" prompt + the tools that make X possible. |

A toolset says **"these tools are available"**. A skill says **"here
is how to use these tools, with prose"**.

---

## Lifecycle — when each piece resolves

```
agent load (LoadDir)
  └─ parse *.agent.md (front-matter + body turns)
  └─ apply *.lifecycle.yaml overrides
  └─ for each agent: base scope = union of:
        config defaults  +  agent.skills  +  agent.toolsets  +  agent.tools
  └─ for each turn: turn scope = (if any turn declarations present)
        replace base scope with: union of declared skills/toolsets/tools
        else: base scope
session start (runner)
  └─ resolve scope → ordered list of model.ToolDefinition
  └─ resolve skill prompt fragments → appended to system prompt in order
  └─ resolve skill parameters → exposed for variable substitution
per turn (runner)
  └─ pass resolved tool definitions to LLM as the tools spec
```

Two distinct moments matter: **load** (file parsing + agent assembly)
and **session start** (resolution into runtime objects). Per-turn
overrides apply at session start, not at load.

---

## Precedence rules

For a single agent, the **effective tool list** is built by walking
sources in this priority order (highest first):

| # | Source | Wins on conflict |
|---|---|---|
| 1 | Per-turn `tools:` declaration | Replaces lower entries for that turn. |
| 2 | Per-turn `toolsets:` declaration | Replaces lower entries for that turn. |
| 3 | Per-turn `skills:` declaration | Replaces lower entries for that turn. |
| 4 | Agent-level `tools:` (front-matter or lifecycle) | Replaces 5–7 within a turn that has no per-turn declaration. |
| 5 | Agent-level `toolsets:` | Replaces 6–7 similarly. |
| 6 | Agent-level `skills:` | Replaces 7 similarly. |
| 7 | `Config.DefaultToolsets` | Baseline when nothing else is declared. |

**The "replace" semantic is per-turn-scope**, not global. A turn
without any declarations falls through to the base agent scope (4–7),
which is itself the union of those sources.

### Why replace, not append?

Per-turn declarations are how authors **narrow** tool access for risky
turns (a release-tag turn that should not be able to push, a verify
turn that should be read-only). An "append" semantic would silently
re-expose every base-scope tool and defeat the safety intent.

If an author wants append behavior, they write the union explicitly in
the turn declaration.

---

## Naming & collision rules

### Canonical tool name

A tool's canonical name is:

| Source | Canonical form |
|---|---|
| Inline HTTP tool (`*.tool.yaml`) | The `name:` field. |
| MCP-bound tool | `<server>/<remote-name>`. |
| `shell-mcp` tool (special case of MCP) | `<server>/<manifest-name>` where the operator chose `<server>` in `mcp-servers.yaml`. |

Tool names match `^[a-z][a-z0-9_-]*(/[a-z][a-z0-9_-]*)?$`. The
prefix-before-slash is the server segment; everything after is the
tool segment. One slash maximum.

### Collisions

Detected at **session start**, not load:

- **Same canonical name from two sources** → hard error. Operator must
  rename one source.
- **Same tool name appearing in two skills/toolsets in the same scope** →
  deduplicated (the tool is listed once, its definition must be
  identical across sources or it is a hard error).
- **`shell/git-log` (MCP) vs `git-log` (inline)** → distinct canonical
  names; both are exposed. The LLM sees both; the agent author chose
  to allow that.

### Visibility scoping

A tool is visible to the model **only** for the turn whose effective
scope contains it. Out-of-scope tool calls return an MCP-style
`tool not available` error to the model without invoking anything.

---

## Composition

### Toolsets can reference toolsets

```yaml
# release-all.toolset.yaml
name: release-all
tools: []
includes: [release-read, release-write, release-verify]
```

`includes:` flattens at session start; cycles are detected and
rejected. Flattened tool list is deduplicated by canonical name.

### Skills do not nest

A skill is a leaf. To compose, list multiple skills on the agent:

```yaml
skills: [git-basics, release-write]
```

Their prompt fragments are appended to the system prompt in **list
order**. Their tool lists are unioned. Their `parameters:` blocks
merge; on a parameter-name collision, the later skill wins (warning
logged).

### Mixing tools, toolsets, and skills in one scope

All three are allowed in the same `tools:` / `toolsets:` / `skills:`
trio. Resolution unions them and deduplicates by canonical name.

---

## Visibility per turn — worked example

```markdown
---
name: release-agent
skills: [git-basics]
toolsets: [release-read]
---

You assist with releases.

---
toolsets: [release-write]
Tag the release now.

---
tools: [shell-git/log]
Verify the tag is present locally.
```

Effective scope per turn:

| Turn | Scope source | Resolved tools |
|---|---|---|
| 1 (system prompt only) | n/a | n/a |
| 2 ("Tag the release…") | per-turn `toolsets: [release-write]` (replaces base) | union of `release-write` toolset's tools |
| 3 ("Verify the tag…") | per-turn `tools: [shell-git/log]` (replaces base) | `[shell-git/log]` only |

Note that turn 2 loses access to `git-basics` skill's tools and
`release-read` toolset's tools. If the author wants them, they include
them explicitly in the turn declaration.

---

## Authoring guides

### When to write a new tool (`*.tool.yaml`)

- The tool is an HTTP endpoint with no MCP wrapper.
- The tool is one-off and not worth bundling.

Otherwise, prefer exposing it via an MCP server (the `shell-mcp`
binary for shell commands; an HTTP MCP server for HTTP APIs).

### When to write a new toolset (`*.toolset.yaml`)

- You have ≥ 2 tools that always go together.
- You want a name that agents can reference instead of listing tools.
- You do **not** need to give the model a prompt fragment.

### When to write a new skill (`*.skill.yaml`)

- You want to teach the model **how** to use the tools, not just
  expose them.
- You want parameters surfaced into the prompt.
- The capability is a coherent verb (e.g. `release-write`,
  `inbox-triage`), not a grab bag.

### When to write a per-turn declaration

- The turn does something dangerous and the agent's base scope is too
  permissive.
- The turn is the only one that needs a specific tool, and you don't
  want it polluting other turns' scope.

---

## Examples

Working examples live in `tanning/`:

| File | Demonstrates |
|---|---|
| `tanning/agents/go-release-prep.agent.md` | Multi-turn with per-turn toolset narrowing. |
| `tanning/agents/go-release-tag.agent.md` | Skill + per-turn override. |
| `tanning/tools/*.tool.yaml` | HTTP and MCP-bound inline tools. |
| `tanning/tuning/skills/*.skill.yaml` | Skill bundles with prompt fragments. |
| `tanning/tuning/toolsets/*.toolset.yaml` | Toolset composition with `includes:`. |

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Assuming per-turn declarations **add** to base scope | They **replace**. List anything you still need explicitly. |
| Defining the same tool in `*.tool.yaml` and in an MCP server with the same name | Canonical-name collision; rename one. |
| Putting prompt fragments in a toolset | Use a skill instead; toolsets are name-only. |
| Nesting skills (referencing one skill from another) | Compose at the agent level (`skills: [a, b]`); flatten manually. |
| Relying on `Config.DefaultToolsets` to "always be there" | A per-turn declaration replaces defaults too. |
| Declaring a tool that the loaded MCP servers do not expose | Hard error at session start with the missing tool's canonical name. |

---

## Verification checklist

Before opening a PR that affects tool/skill/toolset resolution:

- [ ] Precedence table in this file still matches `internal/runner`
      / `internal/agent` resolution code
- [ ] Naming-collision test covers the change
- [ ] At least one `tanning/` example exercises the change end-to-end
- [ ] File-format change cross-linked from [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md)
- [ ] If a new resolution source is introduced, this guide's precedence
      table is updated **and** `Config.DefaultToolsets` semantics are
      re-verified
- [ ] Documentation for per-turn replace-vs-append intent remains
      clear

---

_Last reviewed: 2026-05-19_
