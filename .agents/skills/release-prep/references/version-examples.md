# Version bump examples

## PATCH (bug fixes, docs, refactors — no new user-facing surface)

Last tag: `v0.1.2`
Commits:
- `fix(session): handle empty response body gracefully`
- `docs: update GUIDE.md examples`
- `refactor(config): extract parseTimeout helper`

→ NEXT_VERSION = `v0.1.3`

---

## MINOR (new CLI command or flag, new config key, new API endpoint)

Last tag: `v0.1.2`
Commits:
- `cli: add leather doctor subcommand`
- `cli: add leather init subcommand`
- `fix(scheduler): off-by-one in cron window check`

→ NEXT_VERSION = `v0.2.0`

---

## MAJOR (breaking change to config schema, CLI interface, or public API)

Last tag: `v0.1.2`
Commits:
- `feat(config)!: rename llm_url to llm_endpoint in config.yaml`
- `cli: add leather migrate subcommand`

→ NEXT_VERSION = `v1.0.0`

---

## Explicit override

User says: "tag this as v0.3.0"
→ Use `v0.3.0` regardless of auto-detection result. State the override.

---

## Edge cases

- If there are zero commits since the last tag, ask the user whether to proceed
  with a re-tag or abort.
- If the last tag is a pre-release (e.g. `v0.2.0-rc.1`), treat it as
  `v0.2.0` for bump purposes unless the user says otherwise.
- If no tags exist at all, NEXT_VERSION = `v0.1.0`.
