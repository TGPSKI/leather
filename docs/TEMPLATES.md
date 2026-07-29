# Template Reference

Leather uses several small templating syntaxes, each scoped to a specific
file format. They are intentionally different so you can tell at a glance
which one is in play.

## Flavors at a glance

| Syntax | Example | Where it works | What it does |
| --- | --- | --- | --- |
| `{{env:VAR}}` | `secret: "{{env:GITHUB_WEBHOOK_SECRET}}"` | **Specific fields only:** `tannery.yaml` webhook `secret`, HTTP tool/skill URLs, headers and body templates, and `http_poll` source URLs/headers. **Not** general `config.yaml` settings — see below. | Expanded at load time from the process environment. Missing var → load error (fail closed). |
| `{{env.VAR}}` | `"args": ["--token", "{{env.GH_TOKEN}}"]` | `shell-tools.json` (shell-mcp manifests) | Same semantics as `{{env:VAR}}` but uses the dot form to match the JSON manifest house style. |
| `{{key}}` | `"Summarize {{repo_name}}"` | Agent prompts, lifecycle prompts, runner-injected runtime variables | Substituted at turn dispatch from runtime variables produced by `extract:` rules on prior tool results. Unknown keys are left literal unless `strict_templates` is set. |
| `{{.field}}` | `"url": "https://api.example.com/{{.id}}"` | HTTP tool argument templates, file-path templates inside tool definitions | Go `text/template` with dot-rooted access to the tool's argument object. |
| `{{hide_id}}` / `{{correlation_id}}` | `queue_pattern: "pr-meta-{{correlation_id}}"` | `tannery.yaml` route `queue_pattern`, curing `output.queue` | Per-event substitution at route-match time. Used for fan-out into isolated single-use queues. |

## Choosing the right one

- **Loading a secret from the environment?** Use `{{env:VAR}}` in the YAML
  fields that support it (webhook `secret`, tool URLs/headers/bodies) or
  `{{env.VAR}}` in `shell-tools.json`. Both resolve at load time, both fail
  closed on a missing variable.
- **Pointing a run at a different model or endpoint?** Use the **`LEATHER_*`
  env overrides**, not a template. `config.yaml`'s scalar settings (`model`,
  `llm_endpoint`, `max_tokens`, `log_level`, …) are plain values — a
  `{{env:VAR}}` written there is **not** expanded and reaches the runtime as
  the literal string. Keep the config's committed default readable
  (`model: llama3`) and override per run:

  ```
  LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq \
  LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000 leather serve
  ```

  `leather doctor` prints the effective value with its source, which is the
  quickest way to confirm an override landed.
- **Passing a runtime value between agent turns?** Use `{{key}}` in the
  prompt and an `extract:` rule on the upstream tool call to populate it.
- **Building a tool URL or path from the tool's own arguments?** Use
  `{{.field}}` (Go template) inside the tool definition.
- **Fanning a webhook into per-event queues?** Use `{{correlation_id}}` (or
  `{{hide_id}}`) inside `queue_pattern` or a curing's `output.queue`.

## Common mistakes

- Mixing `{{env:VAR}}` and `{{env.VAR}}` — the colon form is YAML-only and
  the dot form is `shell-tools.json`-only. They are not interchangeable.
- Writing `model: "{{env:LEATHER_MODEL}}"` in `config.yaml`. The colon form is
  scoped to the fields listed above; a general config setting is **not**
  templated, so this silently sends the literal `{{env:LEATHER_MODEL}}` to the
  provider as a model name. Use the `LEATHER_MODEL` env override instead.
- Using `{{key}}` in a YAML config file expecting it to read from the
  environment. `{{key}}` is a *runtime* variable, only meaningful inside an
  agent prompt or lifecycle step.
- Using `{{.field}}` outside a Go-template-aware field (HTTP tool args,
  certain file path fields). Everywhere else it is left literal.
- Expecting `{{correlation_id}}` inside an agent prompt. It is a route-time
  template, not a runtime variable.

## Built-in runtime variables

Every agent prompt has these pre-populated by `runner.BuildRunData` before
any user `extract:` rule fires:

| Key | Value |
|---|---|
| `agent_name` | The agent's `name` field. |
| `schedule` | The cron expression or `""` for non-scheduled agents. |
| `now` | RFC3339 timestamp at run start. |
| `tags` | Comma-joined `tags:` list (or `""`). |

Skill-declared `parameters` are merged on top of these and override on key
collision; `extract:` rules from prior tool calls override both. Nothing
shadows the agent's name unless the agent explicitly sets `agent_name` as
a skill parameter.

## See also

- [GLOSSARY.md](GLOSSARY.md) — definitions for hide, curing, correlation ID.
- [ARCHITECTURE.md](ARCHITECTURE.md) — where each template is applied in the
  pipeline.
- [examples/10-ci-gate/tannery.yaml](../examples/10-ci-gate/tannery.yaml) —
  a working example of `queue_pattern` fan-out plus `{{env:VAR}}` secret
  loading.
