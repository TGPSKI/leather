---
name: agents-doc-lifecycle
description: 'Manage AGENTS.md and .subagents/*.md across the leather subagent guide set (currently 17 guides spanning core, runtime, worker, serve, quality, performance, security, operations, shell-mcp, ui, replay, agent-definition, tools/skills/toolsets semantics, integrations, examples, roadmap, and observability). USE FOR: auditing every guide against the codebase; adding routing-table rows for new packages or features; splitting an overloaded guide; merging thin guides; updating flag/type/subcommand tables; validating cross-references and ownership uniqueness; enforcing the per-guide `last reviewed` footer; running the full ownership/footer/cross-ref health check. DO NOT USE FOR: writing implementation code; debugging runtime errors; running tests.'
compatibility: Requires Go toolchain (`go run`). Designed for GitHub Copilot, Claude Code, and similar coding agents.
metadata:
  argument-hint: 'What agents-doc operation? e.g. "audit", "add routing row for internal/foo", "split AGENTS-WORKER", "merge AGENTS-X and AGENTS-Y", "sync flag table", "validate cross-refs", "lint frontmatter", "check last-reviewed footers", "run §9 acceptance check"'
  user-invocable: "true"
---

# agents-doc-lifecycle

Keeps [`AGENTS.md`](../../../AGENTS.md), every guide in
[`.subagents/`](../../../.subagents/), and the
[`.subagents/README.md`](../../../.subagents/README.md) index
synchronized with the leather codebase.

These documents are **first-class navigation surface** for AI agents
working on leather. Stale or inaccurate guides waste context budget
and steer agents toward the wrong file.

---

## When to use this skill

- A new `internal/` package or `cmd/` binary was added and needs a
  routing row + subagent coverage.
- A new cross-cutting concern landed (a new threat-model surface, a
  perf hot path, an operations playbook) and needs a guide entry.
- A subagent guide has grown past 500 LOC and should be split.
- Two guides together stay under 150 LOC and should merge.
- A flag, model type, CLI subcommand, HTTP endpoint, or trust-boundary
  surface was added or removed.
- A guide's `last reviewed` footer is missing or older than 90 days.
- The routing table in `AGENTS.md` doesn't match `.subagents/README.md`.
- You want a health check before a significant PR or a release.

## When NOT to use this skill

- Writing actual Go code, fixing tests, or running benchmarks.
- Authoring a brand-new agent definition or skill bundle (use the
  `agent-tuning` skill).
- Working on documentation outside the AGENTS/subagents set
  (`docs/ARCHITECTURE.md`, competitive-landscape, etc.).

---

## Authoritative guide set

This skill must keep the **routing table** in
[`../../../AGENTS.md`](../../../AGENTS.md) and the index in
[`../../../.subagents/README.md`](../../../.subagents/README.md) in
sync with the live set of guides:

| Guide | Domain |
|---|---|
| [`.subagents/AGENTS-CORE.md`](../../../.subagents/AGENTS-CORE.md) | Loader internals, session, model types |
| [`.subagents/AGENTS-AGENTDEF.md`](../../../.subagents/AGENTS-AGENTDEF.md) | Author-facing agent file format |
| [`.subagents/AGENTS-TOOLS-SKILLS-TOOLSETS.md`](../../../.subagents/AGENTS-TOOLS-SKILLS-TOOLSETS.md) | Tool/skill/toolset resolution semantics |
| [`.subagents/AGENTS-RUNTIME.md`](../../../.subagents/AGENTS-RUNTIME.md) | Runner, tool, MCP, cache, notify |
| [`.subagents/AGENTS-WORKER.md`](../../../.subagents/AGENTS-WORKER.md) | Scheduler, queue, worker |
| [`.subagents/AGENTS-SERVE.md`](../../../.subagents/AGENTS-SERVE.md) | CLI, config, schema, HTTP API |
| [`.subagents/AGENTS-SHELL-MCP.md`](../../../.subagents/AGENTS-SHELL-MCP.md) | `cmd/shell-mcp` companion binary |
| [`.subagents/AGENTS-UI.md`](../../../.subagents/AGENTS-UI.md) | Browser SPA |
| [`.subagents/AGENTS-REPLAY.md`](../../../.subagents/AGENTS-REPLAY.md) | Replay capture, storage, action surface |
| [`.subagents/AGENTS-QUALITY.md`](../../../.subagents/AGENTS-QUALITY.md) | Tests, build, CI, observability |
| [`.subagents/AGENTS-PERFORMANCE.md`](../../../.subagents/AGENTS-PERFORMANCE.md) | Hot paths, bench catalog, baseline |
| [`.subagents/AGENTS-SECURITY.md`](../../../.subagents/AGENTS-SECURITY.md) | Threat model, trust boundaries |
| [`.subagents/AGENTS-OPERATIONS.md`](../../../.subagents/AGENTS-OPERATIONS.md) | Deploy, supervise, backup, upgrade |
| [`.subagents/AGENTS-INTEGRATIONS.md`](../../../.subagents/AGENTS-INTEGRATIONS.md) | Notifier / MCP / webhook / scanner authoring patterns |
| [`.subagents/AGENTS-EXAMPLES.md`](../../../.subagents/AGENTS-EXAMPLES.md) | `tanning/` corpus + tutorial sequence |
| [`.subagents/AGENTS-ROADMAP.md`](../../../.subagents/AGENTS-ROADMAP.md) | `ROADMAP.md` grooming and promotion path |
| [`.subagents/AGENTS-OBSERVABILITY.md`](../../../.subagents/AGENTS-OBSERVABILITY.md) | Log levels, run history, status/health/metrics |

**Deferred:** none currently. Phase 6 closed 2026-05-19. Track any
future guides in [`ROADMAP.md`](../../../ROADMAP.md).

---

## Executable tool

Located at [`scripts/main.go`](scripts/main.go); thin wrapper
[`scripts/run.sh`](scripts/run.sh). Run from the repo root:

```bash
# Show .subagents/*.md files missing from AGENTS.md routing table:
bash .agents/skills/agents-doc-lifecycle/scripts/run.sh sync

# Same, and patch AGENTS.md with stub rows for missing guides:
bash .agents/skills/agents-doc-lifecycle/scripts/run.sh sync --fix

# Full audit: sync + LOC band check + last-reviewed footer check:
bash .agents/skills/agents-doc-lifecycle/scripts/run.sh audit

# Same, exit non-zero on any issue (CI pre-flight):
bash .agents/skills/agents-doc-lifecycle/scripts/run.sh check

# Tool's own tests:
(cd .agents/skills/agents-doc-lifecycle/scripts && go test -race .)
```

Flags: `--root` (default `AGENTS.md`), `--subagents-dir`
(default `.subagents`), `--divide-threshold` (default `500`),
`--union-threshold` (default `80`), `--fix` (sync only).

The tool covers the mechanical checks. The procedures below cover the
judgment-based operations (split, merge, table sync, content authoring,
cross-reference validation) the tool cannot perform.

> If you change the tool's flag defaults, update the breadth/depth
> table and the `--divide-threshold` / `--union-threshold` references
> in this file in the same PR.

---

## Procedure

### 1. Audit — establish current state

```bash
bash .agents/skills/agents-doc-lifecycle/scripts/run.sh audit
```

Then read the surface the tool cannot inspect:

```text
Read: AGENTS.md
Read: .subagents/README.md
Read: every .subagents/AGENTS-*.md
List: internal/   (compare against routing-table ownership)
List: cmd/        (compare against routing-table ownership)
List: ui/         (confirm AGENTS-UI.md still claims it)
```

Produce an **audit report** with this template:

| Check | Pass / Fail | Notes |
|---|---|---|
| Every `internal/` package owned by exactly one guide | | List orphans / duplicates. |
| Every `cmd/` binary owned by exactly one guide | | |
| `AGENTS.md` routing table and `.subagents/README.md` rows match 1:1 | | |
| Every routing-table link points to a file that exists | | |
| Every guide between 80–500 LOC | | List out-of-band guides. |
| Every guide ends with `_Last reviewed: YYYY-MM-DD_` | | |
| No `last reviewed` date older than 90 days | | |
| Flag table in `AGENTS-SERVE.md` matches `internal/cli` + `internal/config` | | |
| Type table in `AGENTS-CORE.md` matches `internal/model` exports | | |
| CLI subcommand table matches `internal/cli` dispatch | | |
| HTTP API table in `AGENTS-SERVE.md` matches mux registration | | |
| Cross-references (markdown links between guides) resolve | | |
| Every guide has a non-empty Verification checklist | | |
| `09-subagent-gaps.md` §9 acceptance criteria all green | | |

If any row fails, follow the matching procedure below.

### 2. Update routing table rows

When a new package, binary, or cross-cutting concern appears:

1. Identify which existing guide owns the closest domain.
2. If it fits inside an existing guide's scope, add it to that guide's
   "Package responsibilities" or "Scope" section and update the guide's
   `Owns` cell in [`AGENTS.md`](../../../AGENTS.md) and
   [`.subagents/README.md`](../../../.subagents/README.md).
3. If it's a genuinely new domain, create a new guide (procedure 6
   below), then add a routing row to **both** the root routing table
   and the `.subagents/README.md` index in the same PR.
4. Run `audit` to confirm parity.

Row format (root routing table):

```markdown
| Brief description of what you're working on | [.subagents/AGENTS-NAME.md](.subagents/AGENTS-NAME.md) | `internal/pkg1`, `internal/pkg2` |
```

Row format (`.subagents/README.md` index):

```markdown
| [AGENTS-NAME.md](AGENTS-NAME.md) | Short domain | `internal/pkg1`, `internal/pkg2` |
```

### 3. Split — divide an overloaded guide

When a guide exceeds 500 LOC, spans more than 3–4 packages, or covers
two distinct authoring audiences (e.g. **internals** + **user-facing
spec**):

1. Identify the clearest domain boundary. The boundary should be one
   you can describe in a single sentence.
2. Create `.subagents/AGENTS-{NEWDOMAIN}.md` from the template in
   procedure 6.
3. **Lift** the relevant sections out of the original guide (do not
   duplicate). Leave a pointer like
   *"For X, see [AGENTS-NEWDOMAIN.md](AGENTS-NEWDOMAIN.md)."*
4. Update cross-references in both guides; add a row to
   `AGENTS.md` and `.subagents/README.md`.
5. Verify no dead links remain (procedure 8).
6. Run `check` and confirm exit 0.

**Split heuristic shortcuts:**

- Internals vs author-facing spec: split (CORE ↔ AGENTDEF case).
- File-format spec vs resolution semantics: split (AGENTDEF ↔
  TOOLS-SKILLS-TOOLSETS case).
- Two binaries in one guide: split (SERVE ↔ SHELL-MCP case).
- Backend UI section >50 LOC inside a backend guide: split
  (SERVE ↔ UI case).

### 4. Merge — unite thin guides

When two guides together stay under 150 LOC and their domains
collapsed:

1. Choose the file to keep (prefer the more general name).
2. Merge content, including "Common mistakes" rows and Verification
   checklist items.
3. Delete the smaller file.
4. Update both `AGENTS.md` routing table and
   `.subagents/README.md` index — replace two rows with one.
5. Update every cross-reference pointing at the deleted guide.
6. Run `check`.

### 5. Sync tables and field references

Implementation changes can outpace documentation. Update in lockstep
with the owning PR:

**Flag table (`AGENTS-SERVE.md`):**
- Read `internal/cli/cli.go` and `internal/config/env.go`.
- Compare registered flags + env vars against the table.
- Add missing rows; remove deleted rows; update defaults that
  changed.

**Type table (`AGENTS-CORE.md`):**
- Read `internal/model/` files.
- Compare exported types against the table.
- Add new types; remove deleted types; update descriptions.

**CLI subcommand table (`AGENTS-SERVE.md`):**
- Read `internal/cli/cli.go` dispatch switch.
- Compare cases against the table.

**HTTP API endpoint table (`AGENTS-SERVE.md`):**
- Read `apiMux` registration in `internal/cli/cmd_serve.go`.
- Each endpoint also listed in `AGENTS-UI.md`'s `api.js` wrapper.

**Front-matter / lifecycle field reference (`AGENTS-AGENTDEF.md`):**
- Read `internal/agent/*.go` loader + `internal/model.Agent`.
- Every field exposed to authors appears in the relevant table.

**`shell-mcp` manifest schema (`AGENTS-SHELL-MCP.md`):**
- Read `cmd/shell-mcp/main.go` manifest decoder.
- Update fields, defaults, and templating rules.

**Worker file fields (`AGENTS-WORKER.md`):**
- Read `internal/worker/*.go` loader.

**Trust-boundary table (`AGENTS-SECURITY.md`):**
- Walk every new package or endpoint added since the last review.
- Add a row to the threat model if it introduces a new actor or input.

**Verification checklists:**
- Each guide must have a non-empty checklist at the bottom.
- Checklist items must reference concrete commands
  (`go test ./internal/<pkg>/...`) or concrete file paths, never
  vague aspirations.

### 6. Create a new guide — template

Use this template for any new guide. Every section is required.

```markdown
# AGENTS-{NAME}.md — leather {domain description}

Subagent guide for the {domain}: {one-sentence scope}.

Load this guide when working on {package list / file map / concern}.
For {adjacent domain}, see [AGENTS-{OTHER}.md](AGENTS-{OTHER}.md).
{repeat the cross-link sentence for every adjacent guide}

---

## Scope                       (or "Package responsibilities")

{What this guide owns. If a package: name the package and what it owns.
If cross-cutting: name the policy surface.}

---

## {Field reference / Patterns / Architecture sections}

{Tables, code blocks, examples.}

---

## Dependency direction         (if a package guide)

```
{import diagram}
```

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| … | … |

---

## Verification checklist

Before opening a PR touching this domain:

- [ ] `go test ./...{relevant paths}` passes
- [ ] {domain-specific invariants}
- [ ] Cross-references to adjacent guides remain accurate
- [ ] Field/flag/endpoint tables in this guide match the code

---

_Last reviewed: YYYY-MM-DD_
```

Authoring rules:

- Stay inside the 80–500 LOC band.
- Always end with the `_Last reviewed:_` footer using `YYYY-MM-DD`.
- The first paragraph after the H1 declares scope and links every
  adjacent guide.
- Every cross-reference uses the bare filename
  (`[AGENTS-FOO.md](AGENTS-FOO.md)` within `.subagents/`,
  `[.subagents/AGENTS-FOO.md](.subagents/AGENTS-FOO.md)` from the root).
- Code blocks use Go for Go, YAML for config, JSON for manifests.

### 7. Lint front matter and metadata

For this skill and any sibling skill (`agent-tuning`,
`code-quality-lifecycle`, `documentation-lifecycle`):

- `name:` matches the directory name.
- `description:` exists, ≤ 2000 chars, mentions both USE FOR and
  DO NOT USE FOR.
- `metadata.argument-hint:` is present.
- `metadata.user-invocable:` is the literal string `"true"` or
  `"false"`.
- Trailing `---` closes the YAML block.

### 8. Validate cross-references

For every guide:

- Every `[Anchor](path)` link's target file exists.
- Every `[…](AGENTS-NAME.md)` references a guide currently in the
  authoritative set.
- Every `[…](../docs/…)` or `[…](../ROADMAP.md)` target resolves.

A simple shell pass that catches most issues:

```bash
grep -rEho '\]\(([^)]+\.md)[^)]*\)' .subagents AGENTS.md \
  | sed -E 's/.*\((.*\.md).*\)/\1/' \
  | sort -u \
  | while read p; do
      [ -f ".subagents/$p" ] || [ -f "$p" ] || [ -f "docs/$p" ] \
        || echo "missing: $p"
    done
```

Promote this into the executable tool when convenient.

### 9. Audit ownership uniqueness

No two guides may claim the same package or `cmd/` binary.
Cross-cutting guides (`SECURITY`, `OPERATIONS`, `PERFORMANCE`,
`TOOLS-SKILLS-TOOLSETS`, `AGENTDEF`) own **policy** or **semantics**,
not packages — they may reference packages owned by package-guides
without claiming them.

For each package guide row in `AGENTS.md`, parse the `Owns` cell into
the set of `internal/...` / `cmd/...` paths and assert pairwise
disjoint sets.

### 10. Enforce `last reviewed` footer

Every guide's final non-blank line must match:

```
_Last reviewed: YYYY-MM-DD_
```

The audit script greps for this; missing footer or footer >90 days old
is a finding. Update the date in the same PR that touches the guide.

### 11. Run the ownership/footer/cross-ref acceptance check

Walk every bullet and confirm:

- Every package/binary in `internal/` and `cmd/` has exactly one
  owning guide.
- Every guide between 80 and 500 LOC.
- Every guide ends with a `last reviewed` footer.
- No two guides own overlapping ground.
- Routing in `AGENTS.md` and `.subagents/README.md` match.
- New cross-cutting topics (security, operations, performance) each
  have a guide and inbound links from every affected package guide.

---

## Breadth vs depth calibration

| Guide line count | Action |
|---|---|
| < 80 LOC | Too thin — merge with the closest guide or expand with real content. |
| 80–400 LOC | Healthy — update in place. |
| 400–500 LOC | Watch — flag in audit; plan a split. |
| > 500 LOC | Split now — pick the clearest domain boundary. |

Subagent count expectation: **17 today**. Resist creating a new guide
for every new package; prefer adding a section to an existing guide
until the guide is genuinely overloaded or a genuinely new
cross-cutting concern emerges.

---

## Lifecycle operations reference

| Operation | Trigger | Action |
|---|---|---|
| Add routing row | New `internal/` package or `cmd/` binary | Update `AGENTS.md` + `.subagents/README.md`; assign owner guide. |
| Add a cross-cutting concern | New trust surface / perf path / ops playbook | Add row to the matching cross-cutting guide, add cross-link in every affected package guide. |
| Split guide | LOC > 500 or two audiences in one file | New file from template; lift content; update routing + cross-refs; run `check`. |
| Merge guides | Both < 80 LOC and domains collapsed | Keep larger; delete smaller; collapse routing rows. |
| Sync flag table | Flag added / removed / renamed | Edit `AGENTS-SERVE.md` flag table + env-var defaults. |
| Sync type table | Exported type changed in `internal/model` | Edit `AGENTS-CORE.md` type table. |
| Sync HTTP API table | Endpoint added / removed | Edit `AGENTS-SERVE.md` endpoint table + `AGENTS-UI.md` `api.js` wrapper note. |
| Sync subcommand table | Subcommand added / removed | Edit `AGENTS-SERVE.md` subcommand table. |
| Sync field reference | Author-facing front-matter / lifecycle field changed | Edit `AGENTS-AGENTDEF.md` table. |
| Sync manifest schema | `shell-tools.json` field changed | Edit `AGENTS-SHELL-MCP.md` table. |
| Refresh `last reviewed` | Any non-trivial edit | Bump footer date in the same PR. |
| Validate cross-refs | After any rename / move | Run procedure 8. |
| Audit ownership uniqueness | Quarterly + before any release | Procedure 9. |
| Full audit | Before a significant PR, release, or phase boundary | Procedures 1, 8, 9, 10, 11. |

---

## References

- [`../../../AGENTS.md`](../../../AGENTS.md) — root guide and routing table
- [`../../../.subagents/README.md`](../../../.subagents/README.md) — index of all guides
- [`../../../ROADMAP.md`](../../../ROADMAP.md) — backlog and deferred items
- [`scripts/main.go`](scripts/main.go) — executable sync/audit/check tool
- [`scripts/main_test.go`](scripts/main_test.go) — tool tests
- [`scripts/run.sh`](scripts/run.sh) — thin wrapper

---

_Last reviewed: 2026-05-19_
