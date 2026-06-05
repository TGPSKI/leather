# AGENTS-EXAMPLES.md — leather examples and tutorials

Subagent guide for the **demo content** domain: agents, skills,
toolsets, and tools shipped under [`tanning/`](../tanning/), the
tutorial sequence, and the "first 5 minutes" path for new users.

Load this guide when adding, removing, or updating any file under
`tanning/`, when writing or refreshing a tutorial under `docs/`, or
when answering "is there an example of X?". For the file-format spec
those examples must obey, see [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md).
For tool / skill / toolset resolution semantics, see
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).
For the example-as-test policy, see [AGENTS-QUALITY.md](AGENTS-QUALITY.md).

---

## Scope

This guide owns:

- The contents of [`tanning/`](../tanning/) (demo agents, skills,
  toolsets, tools, config, MCP server list).
- The "safe to copy" guarantee and what it means.
- The tutorial sequence (0–4) under `docs/tutorials/` once authored.
- The example-as-test contract in CI.

It does **not** own the file-format spec (AGENTS-AGENTDEF) or the
runtime that loads these examples (AGENTS-CORE / AGENTS-RUNTIME).

---

## `tanning/` directory map

```
tanning/
  config.yaml                  Minimal config that points at this directory.
  mcp-servers.yaml             Example MCP server registrations.
  shell-tools.json             Example shell-mcp tool manifest.
  agents/
    *.agent.md                 Demo agent identity + system prompt.
    *.lifecycle.yaml           Demo agent schedule + model selection.
  tools/
    *.skill.yaml               Demo skills (named tool bundles).
    *.toolset.yaml             Demo toolsets (composed skill aliases).
  tuning/                      Reserved for tuning harness output.
```

Naming convention: the basename of an agent's `*.agent.md` and its
`*.lifecycle.yaml` must match (e.g. `go-release-prep.agent.md` +
`go-release-prep.lifecycle.yaml`). The lifecycle YAML's `agent:`
field is the authoritative link; the filename match is a human
convention.

---

## Safe-to-copy guarantee

> Every file under `tanning/` must be safe for a new user to copy
> into `~/.leather/` and run with no edits beyond providing required
> secrets.

What "safe" means here:

- **No destructive shell commands.** No `rm -rf`, no `git push
  --force`, no `gh release delete`. Read-only or strictly additive.
- **No real credentials.** Secrets referenced as `${VAR_NAME}` only.
  The user supplies values; leather fails closed if missing.
- **No assumptions about the user's repo layout** beyond a normal Go
  project root with `go.mod`.
- **Bounded resource use.** Schedules must be sane (no
  `* * * * *`); shell tools must declare `timeout` and
  `max_output_bytes`.
- **No outbound network beyond what is documented.** Every external
  call (GitHub API, MCP server) is named in the agent's body or in
  this guide.

A PR that violates the guarantee blocks until the violation is fixed
or the file moves to `docs/examples/` (out of `tanning/`) with a
clear "not safe to copy" banner.

---

## Per-agent walk-through

For every agent under `tanning/agents/`, this guide must contain a
short walk-through. Format:

### `<agent-name>`

- **Files:** `<name>.agent.md`, `<name>.lifecycle.yaml`.
- **Purpose:** one sentence.
- **Required secrets:** `${VAR}` list.
- **Required MCP servers:** names referenced from
  `mcp-servers.yaml`.
- **Required tools / skills / toolsets:** from `tanning/tools/`.
- **Schedule:** cron expression and what it implies for cost.
- **Safe to copy?** Yes / Yes-with-caveats / No.

Current corpus (keep this list in sync — one entry per file in
`tanning/agents/`):

### `go-release-prep`

- **Files:** `go-release-prep.agent.md`,
  `go-release-prep.lifecycle.yaml`.
- **Purpose:** prepare a Go-module release: changelog draft, version
  bump suggestions, tag candidate.
- **Required secrets:** `${GITHUB_TOKEN}` (read-only scope is enough).
- **Required MCP servers:** the `shell-mcp` companion (for `git`
  commands).
- **Required tools / skills / toolsets:** `shell-git.skill.yaml`,
  `github-repo.skill.yaml`, `release-tag-read.toolset.yaml`.
- **Schedule:** typically `once` or manual; safe to invoke ad hoc.
- **Safe to copy?** Yes.

### `go-release-tag`

- **Files:** `go-release-tag.agent.md`,
  `go-release-tag.lifecycle.yaml`.
- **Purpose:** create and verify a release tag after prep approval.
- **Required secrets:** `${GITHUB_TOKEN}` (write scope for release
  creation).
- **Required MCP servers:** `shell-mcp`.
- **Required tools / skills / toolsets:**
  `shell-release.skill.yaml`, `github-release.skill.yaml`,
  `release-tag-write.toolset.yaml`,
  `release-tag-verify.toolset.yaml`.
- **Schedule:** `once`; gated behind explicit invocation.
- **Safe to copy?** Yes-with-caveats — writes a tag and a GitHub
  release. Run against a throwaway repo first.

---

## Tutorial sequence

Five tutorials anchor the first-week experience. Each lives under
`docs/tutorials/` (create the directory on first authoring). Status
column reflects whether the tutorial file exists today.

| # | Title | Target time | Status |
|---|---|---|---|
| 00 | First agent (hello-world, MockLLM) | 5 min | not authored |
| 01 | Scheduled bot (cron + notify) | 15 min | not authored |
| 02 | Multi-turn with skills | 30 min | not authored |
| 03 | MCP tools (`shell-mcp` walkthrough) | 30 min | not authored |
| 04 | Replay (capture, view, redact, export) | 30 min | not authored |

Authoring rules:

- Every tutorial ends with a "you should now have…" outcome bullet
  list.
- Every tutorial uses files that already exist in `tanning/`, or
  introduces new ones into `tanning/` in the same PR.
- Every tutorial is runnable end-to-end against `MockLLM`; live LLM
  use is optional and clearly marked.

---

## Example-as-test policy

Every file under `tanning/agents/`, `tanning/tools/`, and every
config in `tanning/` must pass `leather validate` in CI.

The hook lives in [AGENTS-QUALITY.md](AGENTS-QUALITY.md); the
authoring contract is:

- A new example PR includes a CI run that validates the new file.
- A change to a schema in `internal/schema` that breaks a `tanning/`
  file is a release-blocker — fix the example in the same PR.
- A new agent under `tanning/agents/` ships with a `MockLLM`
  test-agent invocation logged in the PR description showing it
  runs to completion.

---

## Adding a new example

1. **Decide whether it belongs.** If it demonstrates a *core
   capability*, ship it under `tanning/`. If it's a one-off
   integration tip, write a section in `docs/` instead.
2. **Mirror the existing pattern.** Reuse skill / toolset files when
   possible; do not invent parallel toolsets that duplicate
   `tanning/tools/`.
3. **Verify the safe-to-copy guarantee** against the checklist above.
4. **Update this guide's per-agent walk-through table** in the same
   PR.
5. **Run `leather validate`** and `leather test-agent <name>` against
   `MockLLM`.

---

## Removing or renaming an example

- Renaming a `tanning/` file requires updating every cross-reference
  in this guide and in `tanning/agents/*.lifecycle.yaml`
  `agent:` fields if the change touches an agent name.
- Removing an example requires a one-line entry in
  [`ROADMAP.md`](../ROADMAP.md) noting *why* it was
  removed (tracked by [AGENTS-ROADMAP.md](AGENTS-ROADMAP.md)).

---

## Verification checklist

Before opening a PR that touches `tanning/` or a tutorial:

- [ ] `leather validate --agent-dir tanning/agents --tool-dir
      tanning/tools` exits 0.
- [ ] `leather test-agent <name>` against `MockLLM` prints a
      complete turn transcript.
- [ ] Safe-to-copy guarantee re-verified for any added or modified
      file.
- [ ] Per-agent walk-through table in this guide updated for any
      add / rename / remove.
- [ ] Cross-references to skills / toolsets resolve under the
      precedence rules in
      [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).
- [ ] No new direct env reads — secrets only via `${VAR_NAME}`.
- [ ] CI example-as-test stage green.

---

## `leather init` scaffold convention

`leather init [--dir <path>] [--overwrite]` writes four files into the target
directory:

```
config.yaml                     minimal config pointing at agents/ and .state/
agents/my-agent.agent.md        stub agent with name front matter
agents/my-agent.lifecycle.yaml  hourly schedule wired to my-agent
Makefile                        run + validate + clean targets
```

The scaffold follows the same conventions as `examples/01-hello-mock`:
- `agent_dir: agents` and `state_dir: .state` in `config.yaml`
- agent name in front matter matches the lifecycle `agent:` field
- no model hard-coded — user supplies `LEATHER_MODEL` at run time

When adding a new example under `examples/`, verify that its structure
matches what `leather init` produces for the shared fields above, so new
users graduate from `init` to a real example without friction.

---

_Last reviewed: 2026-06-04_
