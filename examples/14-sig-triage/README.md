# 14 — sig-triage

Assign a Kubernetes SIG to issues that have none, with a small local model.
Three agents, one job each; the write stage is dry by default.

```
analyze-in → [analyze] → match-in → [match] → label-in → [label]
```

- **analyze** — SIG-agnostic. Extracts components/symptoms/keywords from the
  issue. No tools.
- **match** — loads `sigs.reference.yaml` via `get_sig_reference` and picks one
  SIG (or `unknown`). The only stage that knows the taxonomy.
- **label** — assigns the SIG via `apply_sig`. The only stage with side effects.

## Dry vs live (mirrors examples 09–12)

`apply_sig` is gated on `LEATHER_DEMO_MODE` (default `dry`): it prints the action
it would take. Set `LEATHER_DEMO_MODE=live` to actually call `gh`.

`SIG_ACTION` selects the last-step action:

| SIG_ACTION        | Effect (live)                                             |
| ----------------- | --------------------------------------------------------- |
| `comment` (default) | `gh issue comment` posting `/sig <name>` (upstream-safe) |
| `label`           | `gh issue edit --add-label sig/<name>` (needs triage rights) |
| `both`            | comment then label                                        |

## Run the demo (dry)

```
make 14                 # from examples/, dry mode
make 14-live            # LEATHER_DEMO_MODE=live (needs gh auth + a repo you own)
SIG_ACTION=label make 14
```

Or directly:

```
cat sample/issue.json | ../../leather workflow run \
  --config config.yaml --tannery tannery.yaml \
  --curing analyze --queue analyze-in --kind github.issues --source cli
```

Stage artifacts land in `.state/artifacts/{analyze,match,label}/`.

## Batch over real unsigged issues

```
./scripts/fetch-unsigged.sh                 # REPO= LABEL= LIMIT= to tune
leather serve --config config.yaml --tannery tannery.yaml --run-duration 300s
```

## Live mode notes

Direct `--add-label` needs triage rights on the target repo — fine on a repo you
own or a mirror. On upstream kubernetes/kubernetes use `SIG_ACTION=comment`; the
`/sig <name>` prow command is the sanctioned path. Update the SIG names from
`sigs.yaml` with `scripts/gen-taxonomy.sh`; the `features:` lists in
`sigs.reference.yaml` are curated by hand.
