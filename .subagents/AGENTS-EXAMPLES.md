# AGENTS-EXAMPLES.md — leather examples and tutorials

Subagent guide for the **demo content** domain: the numbered examples
under [`examples/`](../examples/), the reference-deployment study in
[`docs/REFERENCE-TANNERY.md`](../docs/REFERENCE-TANNERY.md), and the
"first 5 minutes" path for new users.

Load this guide when adding, removing, or updating any file under
`examples/`, when writing or refreshing a tutorial under `docs/`, or
when answering "is there an example of X?". For the file-format spec
those examples must obey, see [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md).
For tool / skill / toolset resolution semantics, see
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).
For the example-as-test policy, see [AGENTS-QUALITY.md](AGENTS-QUALITY.md).

---

## Scope

This guide owns:

- The contents of [`examples/`](../examples/) (per-example agents,
  skills, toolsets, curings, config, MCP server list) and the index
  table in `examples/README.md`.
- The "safe to copy" guarantee and what it means.
- [`docs/REFERENCE-TANNERY.md`](../docs/REFERENCE-TANNERY.md) — the
  study of a real deployment that cannot meet the one-`make`-target
  contract and so is read in place, pinned upstream, rather than
  vendored. Anything that cannot run from a fresh clone belongs there,
  not in `examples/`.
- The example-as-test contract in CI.

It does **not** own the file-format spec (AGENTS-AGENTDEF) or the
runtime that loads these examples (AGENTS-CORE / AGENTS-RUNTIME).

---

## Example directory map

Each numbered example is self-contained and runs from a fresh clone
with one `make` target. `scripts/new-example.sh` scaffolds this tree:

```
examples/NN-slug/
  README.md                    What it demonstrates, and how to run it.
  config.yaml                  Minimal config that points at this directory.
  tannery.yaml                 Only for tannery examples: dirs, queues, routes.
  mcp-servers.yaml             Only when the example uses MCP tools.
  shell-tools.json             Only when the example uses shell-mcp.
  agents/
    *.agent.md                 Agent identity + system prompt.
    *.lifecycle.yaml           Agent schedule + model selection.
  curings/
    *.curing.yaml              Queue-to-agent bindings for tannery examples.
  tools/
    *.skill.yaml               Skills — the only place tools are defined.
    *.toolset.yaml             Toolsets — lists of tool *names* a skill defines.
  sample/                      Fixtures, including sample/dry/ for dry mode.
  scripts/                     run-demo.sh and the example's pretty.sh copy.
```

Naming convention: the basename of an agent's `*.agent.md` and its
`*.lifecycle.yaml` must match (e.g. `triage.agent.md` +
`triage.lifecycle.yaml`). The lifecycle YAML's `agent:` field is the
authoritative link; the filename match is a human convention.

---

## Safe-to-copy guarantee

> Every file under `examples/` must be safe for a new user to copy
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
or the file moves to `docs/examples/` (out of `examples/`) with a
clear "not safe to copy" banner.

---

## Per-example walk-through

**The corpus is enumerated in exactly one place:** the index table in
[`examples/README.md`](../examples/README.md). This guide does not
carry a second copy — a duplicated index drifts from the original, and
the mirror is always the one that is wrong (the same reason
`.subagents/README.md` no longer mirrors the root routing table).

Each example carries its own walk-through in its `README.md`, which
must state:

- **Purpose:** one sentence — what mechanism it demonstrates.
- **Needs LLM?** yes / no, and whether `make NN` runs dry.
- **Required secrets:** `${VAR}` list.
- **Required MCP servers:** names referenced from `mcp-servers.yaml`.
- **Required tools / skills / toolsets:** which skill file defines
  each tool the agents name.
- **Schedule:** cron expression and what it implies for cost.
- **Safe to copy?** Yes / Yes-with-caveats / No.

When adding an example, add its row to `examples/README.md` and its
`make help` line. Those two registrations stay hand-written; the
scaffolder prints them.

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
- Every tutorial uses files that already exist in `examples/`, or
  introduces new ones into `examples/` in the same PR.
- Every tutorial is runnable end-to-end against `MockLLM`; live LLM
  use is optional and clearly marked.

---

## Example-as-test policy

Every file under `examples/*/agents/`, `examples/*/tools/`, and every
config in `examples/` must pass `leather validate` in CI.

The hook lives in [AGENTS-QUALITY.md](AGENTS-QUALITY.md); the
authoring contract is:

- A new example PR includes a CI run that validates the new file.
- A change to a schema in `internal/schema` that breaks an `examples/`
  file is a release-blocker — fix the example in the same PR.
- A new agent under `examples/*/agents/` ships with a `MockLLM`
  test-agent invocation logged in the PR description showing it
  runs to completion.

---

## Adding a new example

1. **Decide whether it belongs.** If it demonstrates a *core
   capability*, ship it under `examples/`. If it's a one-off
   integration tip, write a section in `docs/` instead.
2. **Mirror the existing pattern.** Reuse skill / toolset files when
   possible; do not invent parallel toolsets that duplicate
   `examples/*/tools/`.
3. **Verify the safe-to-copy guarantee** against the checklist above.
4. **Add the row to `examples/README.md`** and the `make help` line in the same
   PR.
5. **Run `leather validate`** and `leather test-agent <name>` against
   `MockLLM`.

---

## Removing or renaming an example

- Renaming an `examples/` file requires updating every cross-reference
  in this guide and in `examples/*/agents/*.lifecycle.yaml`
  `agent:` fields if the change touches an agent name.
- Removing an example requires a one-line note in the PR description or
  release notes explaining why it was removed.

---

## Verification checklist

Before opening a PR that touches `examples/` or a tutorial:

- [ ] `cd examples/NN-slug && leather validate --config ./config.yaml`
      exits 0 — this loads the whole tool registry, so it also catches
      duplicate tool names and dangling skill/toolset references.
- [ ] `leather test-agent <name>` against `MockLLM` prints a
      complete turn transcript.
- [ ] Safe-to-copy guarantee re-verified for any added or modified
      file.
- [ ] Index row in `examples/README.md` updated for any
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

## Agent skills for release workflow

`.agents/skills/` contains two skills that automate the leather release
cycle. These are **Claude Code / coding-agent skills**, not leather
agent definitions — they run in the IDE assistant, not under `leather serve`.

| Skill | Path | Purpose |
|---|---|---|
| `release-prep` | [`.agents/skills/release-prep/SKILL.md`](../.agents/skills/release-prep/SKILL.md) | Detect next version, insert CHANGELOG section, update docs, commit + push `main`. |
| `release-tag` | [`.agents/skills/release-tag/SKILL.md`](../.agents/skills/release-tag/SKILL.md) | Run four pre-flight gates, create and push annotated tag, verify on origin. |

**Workflow:** run `release-prep` first. When its commit is on `origin/main`,
run `release-tag`. The tag push triggers `.github/workflows/release.yml` which
builds binaries and creates the GitHub Release — do not call `gh release create`
manually.

---

_Last reviewed: 2026-08-09_
