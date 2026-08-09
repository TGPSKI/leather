# `.subagents/` — leather subagent guides

This directory holds the **domain-scoped subagent guides**. Each file is the
authoritative source for one domain. Load the matching guide before doing
focused work in that area instead of reading the full root guide.

**The routing table lives in [../AGENTS.md](../AGENTS.md), and only there.**
Which guide owns which packages, and which guide to load for a given task, is
answered by that table. This file used to mirror it; the mirror drifted from
the original, which is the one failure a duplicated index reliably produces.
It is not reproduced here.

---

## Conventions

- Every guide ends with `_Last reviewed: YYYY-MM-DD_`. The
  `agents-doc-lifecycle` skill audits this footer.
- Every guide stays in the **80–500 LOC** band. Above 500 LOC is a
  split signal; below 80 LOC is a merge signal.
- Cross-references between guides are markdown links with the bare
  filename (e.g. `[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md)`).
- A guide owns one **load-this-guide** sentence at the top declaring
  its scope and pointing at adjacent guides.
- Each package is owned by exactly one guide. Adding a package means adding a
  row to the root routing table, not a note in two guides.

Adding, splitting, or merging a guide is a change to `../AGENTS.md` first:
add the row, then write the file. The `agents-doc-lifecycle` skill checks
ownership uniqueness, footers, and cross-references across the whole set.
