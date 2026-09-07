---
name: release-tag
description: "Sign and push the tag for a prepared leather release, then record the Zenodo DOI the pipeline produces in CITATION.cff. USE FOR: after release-prep has committed and pushed. DO NOT USE FOR: preparing the CHANGELOG or docs (use release-prep first); creating GitHub releases directly (the pipeline handles that)."
compatibility: Designed for Claude Code and similar coding agents working in the leather repository.
metadata:
  argument-hint: 'Version to tag, e.g. "v0.1.3". Must match the CHANGELOG entry created by leather-release-prep.'
  user-invocable: "true"
---

# leather-release-tag

Creates and pushes an annotated git tag for a prepared leather release.
The automated pipeline (GitHub Actions) fires on tag push and creates the
GitHub release — **do not call `gh release create` manually**.

Run `leather-release-prep` first to ensure the working tree and CHANGELOG
are ready.

---

## Pre-flight gates (all four must pass)

### Gate 1 — Clean working tree

```bash
git status --porcelain
```

Must return empty output. If not, abort: "working tree is dirty — commit or
stash changes before tagging."

### Gate 2 — In sync with origin/main

```bash
git fetch origin main
git rev-list HEAD..origin/main --count
```

Must return `0`. If not, abort: "local main is behind origin — pull before
tagging."

### Gate 3 — CHANGELOG has the version

```bash
grep -F "## [VERSION]" CHANGELOG.md
```

Must find at least one match. If not, abort: "CHANGELOG.md has no section for
VERSION — run leather-release-prep first."

### Gate 4 — Tag does not already exist

```bash
git tag --list VERSION
```

Must return empty. If not, abort: "tag VERSION already exists locally or on
origin."

---

## Tag and push

All four gates passed — proceed:

```bash
git tag -s VERSION -m "VERSION"
git push origin refs/tags/VERSION
```

The `main` ruleset requires signed commits and tags; `-s` signs with the
configured key, `-a` alone does not.

---

## Verify

Confirm the tag landed on origin:

```bash
git ls-remote origin refs/tags/VERSION
```

Must return a non-empty line. If empty, the push silently failed — retry the
push or investigate `git remote -v`.

---

## What happens next

The `.github/workflows/release.yml` pipeline fires on the tag push. It:

1. Builds the static binary for each target platform.
2. Creates the GitHub release with the CHANGELOG section for this version as
   the body.
3. Attaches the binaries as release assets.

Do not create the GitHub release manually. Do not re-push the tag. Monitor
the Actions tab if the pipeline does not appear within ~30 seconds of the push.

---

## Record the Zenodo DOI

Zenodo archives every GitHub release and mints a version DOI about a minute
after the release is published. Wait for the pipeline, then find the record:

```bash
gh run watch $(gh run list --workflow release.yml --limit 1 --json databaseId -q '.[0].databaseId') --exit-status
curl -s 'https://zenodo.org/api/records?q=metadata.related_identifiers.identifier:%22https://github.com/TGPSKI/leather/tree/VERSION%22' \
  | python3 -c "import json,sys; h=json.load(sys.stdin)['hits']['hits']; print(h[0]['doi'] if h else 'not yet')"
```

Set `doi:` in `CITATION.cff` to that value. The concept DOI under
`identifiers:` (10.5281/zenodo.22637735) stays as it is. Commit, open a PR
with the `full-test` label, and merge it the way release-prep Step 7
describes.

---

## Checklist

- [ ] `leather-release-prep` committed and pushed
- [ ] Gate 1 passed (clean tree)
- [ ] Gate 2 passed (in sync with origin)
- [ ] Gate 3 passed (CHANGELOG has the version)
- [ ] Gate 4 passed (tag does not exist)
- [ ] Tag created and pushed
- [ ] `git ls-remote` confirms tag is on origin
- [ ] Release pipeline green and the GitHub release exists
- [ ] CITATION.cff `doi` set to the new Zenodo record and merged
