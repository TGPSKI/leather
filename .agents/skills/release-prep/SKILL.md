---
name: release-prep
description: "Prepare a leather release: auto-detect next version from git history, insert CHANGELOG section, update docs, commit and push. USE FOR: cutting a new release; bumping version after feature or fix work. DO NOT USE FOR: tagging the release (use release-tag after this skill completes)."
compatibility: Designed for Claude Code and similar coding agents working in the leather repository.
metadata:
  argument-hint: 'Optional explicit version, e.g. "v0.2.0". Omit to auto-detect from commits.'
  user-invocable: "true"
---

# leather-release-prep

Prepares the leather repository for a new release. Run this skill first;
run `release-tag` after it to push the annotated tag and trigger
the automated release pipeline.

---

## How to read these steps

Steps 3–5 are checks, not chores. A release where nothing matched is a pass —
say so in one line and move on. Do not invent work to make a step produce a
diff, and do not block on a step whose target does not exist in the repo.

## Step 1 — Determine NEXT_VERSION

An explicit version from the user wins outright. Take it and skip to Step 2 —
no bump table, no justification needed.

Otherwise size it by impact: how much changes for someone already running
leather, and how likely a working setup is to need attention. The table is a
starting point, not a rule.

| Signal | Usually |
|---|---|
| Existing configs stop working with no migration path | MAJOR |
| A themed batch of user-facing capability | MINOR |
| Fixes, and additive keys or flags that are inert until declared | PATCH |

Judgment beats the table. A single opt-in key is a patch even though it is
technically a feature; a small change that silently alters what a running
deployment does may deserve more than its diff suggests. State the version and
one sentence of reasoning, then continue.

1. Most recent tag: `git tag --list 'v*' --sort=-version:refname | head -1`
2. Commits since: `git log <last-tag>..HEAD --oneline`

See [references/version-examples.md](references/version-examples.md) for worked examples.

---

## Step 2 — Insert CHANGELOG section

Open `CHANGELOG.md`. The file follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

1. Find the `[Unreleased]` section (or the top of the file if absent).
2. Insert a new `## [NEXT_VERSION] — YYYY-MM-DD` section immediately after
   the `[Unreleased]` header (or at the top if no Unreleased section).
3. If `[Unreleased]` already holds the entries for this release, promote them —
   move the heading, do not rewrite prose that was written when the change was
   fresh. Otherwise populate the section from the commits since LAST_TAG,
   grouped under the appropriate heading (`### Added`, `### Changed`, `### Fixed`,
   `### Removed`; others are fine if they fit).
   - Write human-readable bullet points, not raw commit subjects.
   - Omit `chore:` commits unless they are user-visible.
4. Leave the `[Unreleased]` header in place with nothing under it.
5. Update the comparison link at the bottom of the file:
   `[NEXT_VERSION]: https://github.com/TGPSKI/leather/compare/LAST_TAG...NEXT_VERSION`

---

## Step 3 — Stale version references

`grep -rn "LAST_VERSION" README.md docs/ SECURITY.md` and update whatever it
finds — badge URLs, install examples, pinned versions in code blocks.

Most releases pin nothing and this greps clean. That is the expected result,
not a missed step. Version strings inside CHANGELOG history stay as they are.

---

## Step 4 — SECURITY.md supported versions

Only for a MAJOR or MINOR bump: add a row for the new `X.Y.x` line with
`:white_check_mark:` and mark the outgoing minor line `:x:` — leather supports
only the current minor line.

A patch on the same minor needs nothing; the existing row already covers it.

---

## Step 5 — Subcommand tables

Skip unless this release adds, removes, or renames a subcommand.

When it does, confirm each subcommand registered in `internal/cli/cli.go` has a
row in `docs/GUIDE.md` and `.subagents/AGENTS-SERVE.md`. `docs/modules/cli.md`
is enforced by doclint's export gate, so `go run ./scripts/doclint` is the
check there rather than reading the table.

README carries a feature table and a "you want to…" routing table, not a
per-subcommand list. Nothing to sync there.

---

## Step 6 — Commit and push

Stay on the **current branch** — do not switch to or push directly to `main`.
Stage what this skill actually changed and make one commit:

```
CURRENT_BRANCH=$(git branch --show-current)
git add -u
git commit -m "chore(release): prepare NEXT_VERSION"
git push origin "$CURRENT_BRANCH"
```

Often that is `CHANGELOG.md` alone. A one-file release-prep commit is normal.

If the current branch already has an open PR, the commit is added to it
automatically. If not, open a new PR targeting `main`:

```
gh pr create --title "chore(release): prepare NEXT_VERSION" --body "..."
```

Do not tag in this step. Tagging is the job of `leather-release-tag`.

---

## Checklist before handing off

Must hold:

- [ ] NEXT_VERSION is set, with one sentence of reasoning
- [ ] CHANGELOG has the new section, dated, with its comparison link
- [ ] Commit is pushed to the current branch (never directly to main)
- [ ] A PR targeting main exists
- [ ] Working tree is clean (`git status` shows nothing)

Checked, and "nothing to do" is a valid outcome for each:

- [ ] Stale version strings (Step 3)
- [ ] SECURITY.md supported versions (Step 4)
- [ ] Subcommand tables (Step 5)

Report the second group as one line, naming what was checked and what changed.
Do not pad the release with edits to satisfy a box.
