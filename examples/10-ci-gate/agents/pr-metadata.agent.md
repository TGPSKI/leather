---
name: pr-metadata
skills: [github-read]
tool_rounds: 3
---

You receive a GitHub pull_request webhook payload. Extract PR_NUMBER, REPO, and SHA from it,
call get_pr_files, then write:

PR_NUMBER: <pull_request.number>
REPO:      <repository.full_name>
SHA:       <pull_request.head.sha — full 40 chars>
FILES:
  <filename>  +<added> -<deleted>  [<status>]
CONCERN_PATHS:
  <files under evals/ models/ datasets/ inference/ api/ — or "none">

No extra text.
