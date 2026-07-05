#!/usr/bin/env bash
# profile-run.sh — run any command while sampling system + GPU metrics.
#
# Usage:
#   scripts/profile/profile-run.sh <command...>
#
#   cd examples && WEBHOOK_COUNT=100 BURST_SIZE=25 BURST_DELAY_MAX=0.5 \
#     ../scripts/profile/profile-run.sh make 11
#
# Samples (1-2 Hz, negligible overhead):
#   vmstat.log    CPU us/sy/id/wa (WAIT), runnable/blocked, memory, swap, block io
#   psi.log       kernel pressure stall info (cpu/io/memory, some+full) — the
#                 true "how stalled are we" signal, sharper than iowait
#   gpu.csv       GPU util, VRAM used/total, temp, power, SM/mem clocks, pstate
#   gpu-procs.csv per-process VRAM (vLLM vs everything else)
#   vllm.prom     vLLM /metrics samples: running/waiting requests, KV-cache
#                 usage, prompt/generation token counters, prefix-cache hits
#                 (override endpoint with VLLM_METRICS_URL)
#   sensors.log   CPU/board temps and fans (lm_sensors)
#   ps-top.log    top processes by CPU every 5s (leather vs vLLM attribution)
#   iostat.log    per-device r/s, w/s, await, %util (sysstat)
#   mpstat.log    per-core CPU breakdown — catches single-core bottlenecks (sysstat)
#   pidstat.log   per-process CPU, memory, and disk IO every 5s (sysstat)
#
# Output dir: .state-profiles/run-<timestamp>/ (override with PROFILE_OUT).
# A summary is printed at the end via profile-summary.py.
set -uo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 <command...>" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${PROFILE_OUT:-./.state-profiles}/run-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"

pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null; done
  wait 2>/dev/null
}
trap cleanup EXIT INT TERM

vmstat -t 1 >"$out/vmstat.log" 2>/dev/null &
pids+=($!)

nvidia-smi \
  --query-gpu=timestamp,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,power.draw,clocks.sm,clocks.mem,pstate \
  --format=csv,noheader,nounits -l 1 >"$out/gpu.csv" 2>/dev/null &
pids+=($!)

nvidia-smi \
  --query-compute-apps=timestamp,pid,process_name,used_gpu_memory \
  --format=csv,noheader,nounits -l 1 >"$out/gpu-procs.csv" 2>/dev/null &
pids+=($!)

(while :; do
  echo "=== $(date '+%F %T')"
  sensors 2>/dev/null
  sleep 2
done) >"$out/sensors.log" &
pids+=($!)

(while :; do
  echo "=== $(date '+%F %T')"
  ps -eo pid,comm,%cpu,%mem,rss --sort=-%cpu | head -12
  sleep 5
done) >"$out/ps-top.log" &
pids+=($!)

(while :; do
  echo "=== $(date +%s)"
  for r in cpu io memory; do
    sed "s|^|$r |" "/proc/pressure/$r" 2>/dev/null
  done
  sleep 2
done) >"$out/psi.log" &
pids+=($!)

# sysstat samplers (skipped gracefully if sysstat is not installed).
# S_TIME_FORMAT=ISO forces 24h single-token timestamps for stable parsing.
if command -v iostat >/dev/null 2>&1; then
  S_TIME_FORMAT=ISO stdbuf -oL iostat -xz 1 >"$out/iostat.log" &
  pids+=($!)
fi
if command -v mpstat >/dev/null 2>&1; then
  S_TIME_FORMAT=ISO stdbuf -oL mpstat -P ALL 2 >"$out/mpstat.log" &
  pids+=($!)
fi
if command -v pidstat >/dev/null 2>&1; then
  S_TIME_FORMAT=ISO stdbuf -oL pidstat -urd 5 >"$out/pidstat.log" &
  pids+=($!)
fi

vllm_url="${VLLM_METRICS_URL:-http://127.0.0.1:8000/metrics}"
(while :; do
  echo "=== $(date +%s)"
  curl -s --max-time 2 "$vllm_url" |
    grep -E '^vllm:(num_requests_running|num_requests_waiting\{|(gpu|kv)_cache_usage_perc|prompt_tokens_total|generation_tokens_total|prefix_cache_(queries|hits)_total)' || true
  sleep 2
done) >"$out/vllm.prom" &
pids+=($!)

echo "profiling -> $out"
start=$(date +%s)
rc=0
"$@" || rc=$?
end=$(date +%s)

cleanup
trap - EXIT INT TERM

{
  echo "command:  $*"
  echo "elapsed:  $((end - start))s"
  echo "exit:     $rc"
} | tee "$out/run.txt"

python3 "$script_dir/profile-summary.py" "$out" | tee -a "$out/run.txt"
exit $rc
