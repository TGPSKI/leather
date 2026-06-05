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

## Step 1 — Determine NEXT_VERSION

If the user supplied an explicit version string (e.g. `v0.2.0`), use it.
Otherwise auto-detect:

1. Find the most recent semver tag: `git tag --list 'v*' --sort=-version:refname | head -1`
2. List commits since that tag: `git log <last-tag>..HEAD --oneline`
3. Categorise every commit subject using these rules (first match wins):

| Pattern in subject | Bump |
|---|---|
| `BREAKING`, `!:` in conventional-commit type, or `breaking change` in body | MAJOR |
| New CLI command or flag added (e.g. `add leather foo`, `feat(cli):`, `cli: add`) | MINOR |
| Everything else | PATCH |

4. Apply the highest bump to LAST_VERSION to get NEXT_VERSION.
5. State the version and the bump reason to the user before continuing.

See [references/version-examples.md](references/version-examples.md) for worked examples.

---

## Step 2 — Insert CHANGELOG section

Open `CHANGELOG.md`. The file follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

1. Find the `[Unreleased]` section (or the top of the file if absent).
2. Insert a new `## [NEXT_VERSION] — YYYY-MM-DD` section immediately after
   the `[Unreleased]` header (or at the top if no Unreleased section).
3. Populate it with every commit since LAST_TAG grouped under the appropriate
   heading (`### Added`, `### Changed`, `### Fixed`, `### Removed`).
   - Write human-readable bullet points, not raw commit subjects.
   - Omit `docs:` and `chore:` commits unless they are user-visible.
4. Leave the `[Unreleased]` section blank (or remove it if empty).
5. Update the comparison link at the bottom of the file:
   `[NEXT_VERSION]: https://github.com/tgpski/leather/compare/LAST_TAG...NEXT_VERSION`

---

## Step 3 — Update version references in docs

Search for the previous version string and update to NEXT_VERSION in:

- `README.md` — badge URLs, install examples, version pinning in code blocks
- `docs/GUIDE.md` — any version callouts
- `docs/OPERATIONS.md` — any version callouts

Use `grep -rn "LAST_VERSION"` to find any other version-pinned references.

---

## Step 4 — Verify subcommand tables are current

Confirm that every `Run*` function in `internal/cli/cli.go` has a corresponding
row in each of these tables:

- `README.md` commands table
- `docs/GUIDE.md` commands table
- `docs/modules/cli.md` Public API table
- `.subagents/AGENTS-SERVE.md` subcommand reference table

If any row is missing, add it before committing.

---

## Step 5 — Commit and push

Stage all changed files and create one commit:

```
git add CHANGELOG.md README.md docs/ .subagents/
git commit -m "chore(release): prepare NEXT_VERSION"
git push origin main
```

Do not tag in this step. Tagging is the job of `leather-release-tag`.

---

## Checklist before handing off

- [ ] NEXT_VERSION is set and justified
- [ ] CHANGELOG has the new section with at least one bullet
- [ ] No stale version string remains in docs (grep clean)
- [ ] Subcommand tables are in sync
- [ ] Commit is on origin/main
- [ ] Working tree is clean (`git status` shows nothing)
