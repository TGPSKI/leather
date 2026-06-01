#!/usr/bin/env bash
# run-demo.sh — high-volume CI gate load experiment (example 11)
#
# Same fan-out/fan-in pipeline as example 10, but fires WEBHOOK_COUNT webhooks
# (default 40, range 25-100) with randomised timing and parallel bursts.
# Each webhook spawns 3 parallel analysis agents → 1 decision agent → pr-comments.
#
# Env controls:
#   WEBHOOK_COUNT     total webhooks to fire          (default 40,  range 25-100)
#   BURST_SIZE        webhooks per burst              (default 5)
#   BURST_DELAY_MAX   max seconds between bursts      (default 2.0)
#   JITTER_MAX        max per-webhook jitter seconds  (default 0.25)
#   WAIT_TIMEOUT      seconds to wait for completion  (default 300)
#   RUN_DURATION      leather serve --run-duration    (default 600s)
#   API_ADDR          server bind address             (default 127.0.0.1:7749)
#   GITHUB_WEBHOOK_SECRET                             (default ci-gate-demo-secret)
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

source "${EX_DIR}/scripts/pretty.sh"
# shellcheck source=../../scripts/preflight.sh
source "${EX_DIR}/../scripts/preflight.sh"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-ci-gate-demo-secret}"
export API_ADDR="${API_ADDR:-127.0.0.1:7749}"
WEBHOOK_PATH="${WEBHOOK_PATH:-/webhooks/github}"

WEBHOOK_COUNT="${WEBHOOK_COUNT:-40}"
BURST_SIZE="${BURST_SIZE:-5}"
BURST_DELAY_MAX="${BURST_DELAY_MAX:-2.0}"
JITTER_MAX="${JITTER_MAX:-0.25}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-300}"
RUN_DURATION="${RUN_DURATION:-600s}"

# Clamp WEBHOOK_COUNT to 25-100
[ "$WEBHOOK_COUNT" -lt 25 ]  && WEBHOOK_COUNT=25
[ "$WEBHOOK_COUNT" -gt 100 ] && WEBHOOK_COUNT=100

TMPDIR_PAYLOADS="${EX_DIR}/.state/payloads"
mkdir -p "${TMPDIR_PAYLOADS}"

# Preflight: live mode at high volume WILL trigger GitHub rate limits.
if [ "$(lth_demo_mode)" = "live" ]; then
  lth_live_requirements "11-high-volume-ci" \
    "gh CLI installed and authenticated (gh auth login)" \
    "GitHub primary rate limit (5000 req/h auth'd) easily exhausted at WEBHOOK_COUNT=40+" \
    "secondary abuse rate limit may kick in on rapid PR comments" \
    "run against a throwaway repo you own \u2014 every webhook posts a comment + label"
  lth_require_gh_auth || exit $?
  printf '\033[33mreminder: %s webhooks will hit GitHub. Reduce with WEBHOOK_COUNT=25 if needed.\033[0m\n\n' "$WEBHOOK_COUNT"
fi

# ── header ────────────────────────────────────────────────────────────────────

echo ""
lth_step "11" "high-volume-ci  ${WEBHOOK_COUNT} webhooks  burst=${BURST_SIZE}  parallel fan-out/fan-in"
lth_mode_banner "11"
lth_cont "Each webhook → [pr-metadata | pr-diff | pr-context] (3 parallel single-use queues)"
lth_cont "  → analysis/<id> (collect_size: 3) → decision → pr-comments"
lth_cont "max_concurrent_jobs=8  scheduler_tick=500ms"
echo ""

# ── payload generator ─────────────────────────────────────────────────────────
# 6 rotating PR profiles — 4 FULL_EVAL signals, 2 SKIP signals.
# Profile is chosen by (index % 6).

gen_payload() {
  local idx="$1"
  local profile=$(( (idx - 1) % 6 ))
  local pr_num=$(( 1000 + idx ))
  # Deterministic-looking SHA from PR number
  local sha
  sha=$(printf '%08x%08x%08x%08x%08x' \
    $((pr_num * 0x7f3a + 0xdead)) \
    $((pr_num * 0xb1b2 + 0xcafe)) \
    $((pr_num * 0x3c4d + 0xf00d)) \
    $((pr_num * 0x9e7a + 0xbeef)) \
    $((pr_num * 0x5f6e + 0xaced)))

  local action title body additions deletions changed_files ref login
  case $profile in
    0)  # EVAL: algorithm / model change
      action="synchronize"
      title="feat(model): increase context window to 8192 tokens"
      body="Extends the maximum sequence length from 4096 to 8192. Adjusts position embeddings and batch packing logic. Benchmarks on 4-GPU cluster show 12% throughput reduction at max context."
      additions=183 deletions=41 changed_files=9
      login="alice-dev" ref="feat/context-window-${pr_num}" ;;
    1)  # SKIP: docs only
      action="opened"
      title="docs: update API reference for v2.3 endpoints"
      body="Updates OpenAPI spec and README with new v2.3 endpoint descriptions. Fixes broken anchor links in contributing guide. No code changes."
      additions=52 deletions=18 changed_files=3
      login="docs-bot" ref="docs/api-ref-${pr_num}" ;;
    2)  # EVAL: bug fix with logic change
      action="synchronize"
      title="fix(decoder): prevent OOM on edge-case long sequences"
      body="Adds early truncation guard in beam search loop. Without this fix, sequences longer than the absolute maximum cause OOM in the worker. Reproducer added to tests."
      additions=27 deletions=3 changed_files=4
      login="bob-eng" ref="fix/decoder-oom-${pr_num}" ;;
    3)  # EVAL: new feature, many files
      action="opened"
      title="feat(pipeline): add async streaming inference endpoint"
      body="Adds /v1/stream endpoint returning Server-Sent Events. Each token emitted as decoded. Includes connection timeout, disconnect detection, and backpressure via bounded channel."
      additions=312 deletions=0 changed_files=14
      login="carol-ml" ref="feat/streaming-${pr_num}" ;;
    4)  # SKIP: minor dependency bump
      action="opened"
      title="chore: bump black and ruff to latest"
      body="Routine formatter updates. No logic changes. CI green on main."
      additions=4 deletions=4 changed_files=1
      login="dependabot" ref="chore/fmt-bump-${pr_num}" ;;
    5)  # EVAL: refactor touching inference path
      action="synchronize"
      title="refactor(cache): replace LRU with ARC eviction policy"
      body="Switches inference result cache from LRU to Adaptive Replacement Cache. ARC better handles mixed access patterns. Cache hit rate improved 8% in production shadow test."
      additions=148 deletions=97 changed_files=7
      login="alice-dev" ref="refactor/arc-cache-${pr_num}" ;;
  esac

  printf '{
  "action": "%s",
  "number": %d,
  "pull_request": {
    "number": %d,
    "title": "%s",
    "body": "%s",
    "head": {"sha": "%s", "ref": "%s", "label": "acme-ai:%s"},
    "base": {"ref": "main", "label": "acme-ai:main"},
    "additions": %d,
    "deletions": %d,
    "changed_files": %d,
    "merged": false,
    "draft": false,
    "html_url": "https://github.com/acme-ai/voice-engine/pull/%d",
    "user": {"login": "%s", "type": "User"}
  },
  "repository": {
    "id": 789123,
    "name": "voice-engine",
    "full_name": "acme-ai/voice-engine",
    "private": true,
    "html_url": "https://github.com/acme-ai/voice-engine",
    "description": "Real-time speech generation with STT/TTS eval pipeline"
  },
  "sender": {"login": "%s", "type": "User"}
}' \
  "$action" "$pr_num" "$pr_num" "$title" "$body" \
  "$sha" "$ref" "$ref" \
  "$additions" "$deletions" "$changed_files" \
  "$pr_num" "$login" "$login"
}

# ── fire one webhook (runs in background subshell) ────────────────────────────

fire_one() {
  local idx="$1"
  local payload="${TMPDIR_PAYLOADS}/${idx}.json"
  gen_payload "$idx" > "$payload"

  local sig
  sig=$(openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" -binary < "$payload" \
        | xxd -p -c 256)

  if curl -fsS -o /dev/null \
      -X POST \
      -H 'Content-Type: application/json' \
      -H "X-Hub-Signature-256: sha256=${sig}" \
      -H "X-GitHub-Event: pull_request" \
      --data-binary "@${payload}" \
      "http://${API_ADDR}${WEBHOOK_PATH}" 2>/dev/null; then
    local ptype
    case $(( (idx - 1) % 6 )) in 1|4) ptype="skip" ;; *) ptype="eval" ;; esac
    lth_cont "  sent  PR #$((1000 + idx))  [${idx}/${WEBHOOK_COUNT}]  ${ptype}"
  else
    lth_cont "  FAIL  PR #$((1000 + idx))  [${idx}/${WEBHOOK_COUNT}]  — check .state/serve.log"
  fi
}

# ── start server ──────────────────────────────────────────────────────────────

lth_step "serve" "starting  api=${API_ADDR}  run-duration=${RUN_DURATION}  max-jobs=8"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config  config.yaml \
  --tannery tannery.yaml \
  --mcp-servers-file mcp-servers.yaml \
  --api-addr "${API_ADDR}" \
  --log-file "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration "${RUN_DURATION}" --api) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

lth_step "serve" "waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 40); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    lth_cont "  ready"
    break
  fi
  sleep 0.5
done
echo ""

# ── fire webhooks in bursts ───────────────────────────────────────────────────

lth_step "load" "firing ${WEBHOOK_COUNT} webhooks  burst_size=${BURST_SIZE}  jitter up to ${JITTER_MAX}s"
echo ""

START_TS=$(date +%s)
burst=0
i=1
while [ "$i" -le "$WEBHOOK_COUNT" ]; do
  burst=$(( burst + 1 ))
  batch_end=$(( i + BURST_SIZE - 1 ))
  [ "$batch_end" -gt "$WEBHOOK_COUNT" ] && batch_end="$WEBHOOK_COUNT"
  batch_n=$(( batch_end - i + 1 ))

  lth_step "burst${burst}" "PRs #$((1000+i))–#$((1000+batch_end))  (${batch_n} in parallel)"

  burst_pids=()
  for j in $(seq "$i" "$batch_end"); do
    # Per-webhook jitter so requests don't all land in the same millisecond
    jitter=$(awk -v seed="$((j * 31 + RANDOM))" -v max="$JITTER_MAX" \
             'BEGIN{srand(seed); printf "%.3f", rand()*max}')
    ( sleep "$jitter"; fire_one "$j" ) &
    burst_pids+=($!)
  done
  # Wait only for this burst's subshells — NOT for the long-running SERVE_PID
  for pid in "${burst_pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done

  i=$(( batch_end + 1 ))

  if [ "$i" -le "$WEBHOOK_COUNT" ]; then
    delay=$(awk -v seed="$((burst * 17 + RANDOM))" -v max="$BURST_DELAY_MAX" \
            'BEGIN{srand(seed); printf "%.2f", 0.4 + rand()*max}')
    lth_cont "  next burst in ${delay}s…"
    sleep "$delay"
  fi
done

echo ""
SEND_DONE=$(date +%s)
lth_step "load" "all ${WEBHOOK_COUNT} webhooks sent in $(( SEND_DONE - START_TS ))s"
lth_cont "queue depth will peak then drain as agents process decisions"
echo ""

# ── wait for decision artifacts ───────────────────────────────────────────────

lth_step "wait" "waiting for decision artifacts  (timeout ${WAIT_TIMEOUT}s)"
lth_cont "target: ${WEBHOOK_COUNT} decisions  (1 per webhook)"

waited=0
last_count=-1
while [ "$waited" -lt "$WAIT_TIMEOUT" ]; do
  count=$(find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null | wc -l)
  count="${count//[[:space:]]/}"
  count="${count:-0}"
  if [ "$count" -ne "$last_count" ]; then
    lth_cont "  decisions: ${count} / ${WEBHOOK_COUNT}"
    last_count="$count"
  fi
  if [ "$count" -ge "$WEBHOOK_COUNT" ]; then
    break
  fi
  sleep 2
  waited=$(( waited + 2 ))
done

echo ""
DONE_TS=$(date +%s)
final_count=$(find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null | wc -l)
final_count="${final_count//[[:space:]]/}"

lth_step "results" "pipeline summary  elapsed=$(( DONE_TS - START_TS ))s"
lth_cont ""
lth_cont "  webhooks fired:      ${WEBHOOK_COUNT}"
lth_cont "  decisions written:   ${final_count:-0}"
lth_cont "  send phase:          $(( SEND_DONE - START_TS ))s"
lth_cont "  total elapsed:       $(( DONE_TS - START_TS ))s"
echo ""

# ── artifact tally ────────────────────────────────────────────────────────────

lth_step "tally" "decision breakdown (last 10 of ${final_count:-0}):"
find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null \
  | sort | tail -10 \
  | while read -r f; do
      verdict=$(jq -r '
        .content
        | if test("FULL_EVAL") then "FULL_EVAL"
          elif test("SKIP") then "SKIP"
          else "?"
          end' "$f" 2>/dev/null || echo "?")
      pr=$(jq -r '.content | capture("PR_NUMBER: *(?P<n>[0-9]+)") | .n' "$f" 2>/dev/null || echo "?")
      lth_cont "  PR #${pr}  →  ${verdict}  ($(basename "$f"))"
    done

echo ""
lth_step "done" "artifacts at .state/artifacts/  |  logs at .state/serve.log"
lth_cont ""
lth_cont "Tune with:"
lth_cont "  WEBHOOK_COUNT=100 BURST_SIZE=10 make 11"
lth_cont "  WEBHOOK_COUNT=25  BURST_SIZE=3  BURST_DELAY_MAX=0.5 make 11"
echo ""

# ── shut down server now that the pipeline has drained ───────────────────────
# All decisions written (or WAIT_TIMEOUT reached). Before SIGTERM we wait for
# the queue jsonl files to drain so in-flight tool/LLM calls finish naturally,
# otherwise SIGTERM cancels their context.Context mid-call and floods serve.log
# with "context canceled" errors.
QUEUE_DIR="${EX_DIR}/.state/queues"
DRAIN_TIMEOUT="${DRAIN_TIMEOUT:-30}"
drained=0
for ((i=0; i<DRAIN_TIMEOUT; i++)); do
  pending=$(find "${QUEUE_DIR}" -maxdepth 2 -name '*.jsonl' -not -empty 2>/dev/null | wc -l)
  pending="${pending//[[:space:]]/}"
  if [ "${pending:-0}" -eq 0 ]; then drained=1; break; fi
  sleep 1
done
if [ "$drained" -eq 1 ]; then
  lth_step "serve" "queues drained, shutting down server"
else
  lth_cont "  queues still have ${pending} pending file(s) after ${DRAIN_TIMEOUT}s; shutting down anyway"
  lth_step "serve" "drain timeout reached, shutting down server"
fi
# Brief settle so the last in-flight tool/LLM call can return before SIGTERM.
sleep 1
kill -TERM "$SERVE_PID" 2>/dev/null || true
wait $SERVE_PID 2>/dev/null || true
trap - EXIT
