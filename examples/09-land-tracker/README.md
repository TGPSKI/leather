# 09-land-tracker

A leather agent that monitors land.com property listings for price and status
changes, then notifies via Telegram when anything moves.

The target context: tracking a personal shortlist of rural land parcels over
weeks or months. Prices shift, listings go under contract, and status changes
from "for sale" to "sold" often happen faster than a manual check cadence.

## What this shows

- **Live web fetching with CDN bypass** — land.com is protected by Akamai bot
  detection. `scripts/fetch-property.sh` uses a two-step curl session: prime
  a cookie jar from the site homepage, then fetch the property page with those
  cookies and a matching `Referer` and `Sec-Fetch-Site: same-origin` header.
  This mimics a real browser navigation and clears the bot check.
- **State diffing across runs** — `read_state` loads the prior JSON snapshot;
  after fetching, the agent compares current values and flags any change. The
  demo seeds `.state/property-state.json` from `sample/demo-state.json` (Jan
  2025 baseline prices) so changes are visible on the first run.
- **Randomised multi-instance scheduling** — `land-tracker-long.lifecycle.yaml`
  produces four daily scheduler instances at irregular times (07:23, 12:47,
  16:11, 21:38) to avoid the uniform-interval fingerprint that CDN bot detectors
  flag.
- **Telegram alerts** — output routes to the `telegram-ops` notify backend when
  any price or status change is detected. The report is prepended with
  `🚨 PROPERTY ALERT` in that case.
- **Persistent state across runs** — `write_state` overwrites
  `.state/property-state.json` so the next run diffs against the freshest data.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `curl` with HTTP/2 support (`curl --version | grep HTTP2`).
- `bash` 4+.
- Optional: Telegram bot token in `pass` at `telegram/YOUR_BOT`
  (or set `LEATHER_TELEGRAM_BOT_TOKEN` env var). Set `SEND_TELEGRAM=1` to
  enable delivery.

## Run

```bash
LEATHER_LLM_ENDPOINT=http://localhost:8000 \
LEATHER_MODEL=/path/to/your/model \
make 09
```

To also send Telegram alerts:

```bash
SEND_TELEGRAM=1 \
LEATHER_LLM_ENDPOINT=http://localhost:8000 \
LEATHER_MODEL=/path/to/your/model \
make 09
```

The demo copies `sample/properties.txt` to `.state/` and seeds stale Jan 2025
prices from `sample/demo-state.json`. On the first run the agent will almost
certainly detect price or status changes on at least one parcel.

## Continuous monitoring

```bash
# Demo cadence: 4 fetches per hour, 15-minute window (land-tracker.lifecycle.yaml)
leather serve \
  --config config.yaml \
  --mcp-servers-file mcp-servers.yaml

# Production cadence: 4 fetches per day, random times (land-tracker-long.lifecycle.yaml)
# Swap the lifecycle file by renaming or symlinking as needed.
```

## Adding your own properties

Edit `sample/properties.txt` (or `.state/properties.txt` after first run):

```
# One land.com URL per line. # lines are ignored.
https://www.land.com/property/...
```

## CDN bypass details

`scripts/fetch-property.sh` performs a two-step session:

1. `curl --http2` hits `https://www.land.com/` with `Sec-Fetch-Site: none`
   to prime a fresh Akamai session cookie.
2. After a 2-second pause, it fetches the property URL with the cookie jar,
   `Referer: https://www.land.com/`, and `Sec-Fetch-Site: same-origin`.

If Akamai still returns "Access Denied", the script sleeps 5 seconds, opens a
new cookie jar, and retries once. A second block returns
`fetch_error: blocked_by_cdn` and the agent reports it gracefully without
crashing the run.

## Files

| File | Purpose |
|---|---|
| `config.yaml` | leather config — `tool_dir: tools` loads the property skill |
| `mcp-servers.yaml` | registers shell-mcp for property fetch tools |
| `shell-tools.json` | `read_url_list`, `fetch_page`, `read_state`, `write_state` |
| `tools/property.skill.yaml` | skill wiring the four tools with usage guidance |
| `agents/land-tracker.agent.md` | 5-step agent: load → fetch → diff → persist → report |
| `agents/land-tracker.lifecycle.yaml` | demo lifecycle: 4 instances per hour, 15-min window |
| `agents/land-tracker-long.lifecycle.yaml` | production lifecycle: 4 randomised daily instances |
| `sample/properties.txt` | 4 real California land parcels to monitor |
| `sample/demo-state.json` | stale Jan 2025 baseline prices for demo change detection |
| `scripts/run-demo.sh` | seeds state, runs one-shot `leather run`, prints report |
| `scripts/fetch-property.sh` | two-step Akamai CDN bypass + structured data extraction |
