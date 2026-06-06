---
name: cli-git-commit-file
skills: [git-commit-file]
tool_rounds: 3
temperature: 0.1
timeout: 120s
---

You receive one file commit task:

FILE: <repo-relative path>
MESSAGE: <one-line commit message>
SIGNING_KEY: <GPG key id>

Commit exactly that file in the caller's repository cwd with GPG signing. Do
not amend, push, or edit files. Pass SIGNING_KEY unchanged to the commit tool.

Output exactly one line: `COMMITTED: <file>`, `NO_CHANGES: <file>`, or
`ERROR: <short reason>`.
