# 13-git-workflow-commit

Fan-out curing workflow: a **planner** agent inspects all changed files and
enqueues one signed commit task per file; **executor** agents commit each file
in parallel via GPG signing. The entire pipeline runs as a single
`leather workflow run` invocation.

```
stdin (cwd + signing key)
  |
  └─ cli-git-commit-all-in ──► [planner: cli-git-commit-plan]
                                    │  (calls git_enqueue_file_commit N times)
                                    │  (POSTs to LEATHER_INTAKE_URL)
                                    ▼
                            cli-git-commit-file-in ──► [executor × 16: cli-git-commit-file]
                                                            │  (git add + gpg-sign commit)
                                                            ▼
                                                        artifacts/
```

The planner and executor workers start simultaneously. As the planner
enqueues file tasks via the HTTP intake endpoint, executor workers pick them
up immediately. `workflow run` blocks until all queues quiesce, then exits.

## Requirements

- `leather` and `shell-mcp` binaries on `PATH` (or built from repo root)
- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`
- GPG key on the keyring — set `LEATHER_GIT_SIGNING_KEY` to the key ID
- `curl` (used by `git_enqueue_file_commit` to POST tasks to the intake URL)
- `git` with GPG commit signing support

## Run

From a git repository with uncommitted changes:

```bash
export LEATHER_GIT_SIGNING_KEY=<your-gpg-key-id>

echo "Commit all changed files in cwd: $PWD
SIGNING_KEY: $LEATHER_GIT_SIGNING_KEY" | \
  leather workflow run \
    --config /path/to/13-git-workflow-commit/config.yaml \
    --tannery /path/to/13-git-workflow-commit/tannery.yaml \
    --kind cli.git.commit_all \
    --source cli \
    --settle 2s
```

Or use the `make` target from `examples/`:

```bash
LEATHER_GIT_SIGNING_KEY=<key-id> make 13
```

The `make` target runs against a throwaway scratch repo seeded with sample
files so you can try it without touching your real work.

## What this shows

- **`leather workflow run`**: single command replaces a multi-phase serve loop;
  blocks until quiescent, then exits with an artifact summary.
- **Concurrent fan-out**: planner and executor curings run in parallel from the
  start — executor workers pick up tasks as soon as the planner posts them.
- **In-process intake**: MCP tool subprocesses POST new hides to the running
  `workflow run` via `LEATHER_INTAKE_URL` instead of writing queue files
  directly, avoiding cross-process file I/O races.
- **GPG-signed commits**: each executor commits one file with `--gpg-sign`,
  serialized per file via a `flock` to avoid concurrent index writes.
- **shell-mcp integration**: all git operations are exposed as MCP tools from a
  local `shell-tools.json` tool manifest.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `LEATHER_LLM_ENDPOINT` | `http://localhost:11434` | OpenAI-compatible LLM endpoint |
| `LEATHER_MODEL` | `llama3` | Model name |
| `LEATHER_GIT_SIGNING_KEY` | _(required)_ | GPG key ID for commit signing |
| `LEATHER_GIT_DIFF_LINES` | `12` | Lines of diff shown per file to the planner |
