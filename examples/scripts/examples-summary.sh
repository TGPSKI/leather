#!/usr/bin/env bash
# examples-summary.sh — post-run rollup for `make examples-all`.
#
# Walks examples/NN-*/.state/ directories and produces:
#   1. A pretty-printed runtime overview on stdout.
#   2. A markdown rollup written to examples/.last-run-summary.md (gitignored).
#
# Per-example metrics:
#   - last activity mtime (newest file under .state/)
#   - run records:        count, status breakdown, total tokens, total duration
#   - artifacts:          count under .state/artifacts/
#   - queue depth + DLQ:  jsonl line counts under .state/queues/
#   - serve.log:          ERROR / WARN counts + last ERROR line (if present)
#
# Per-example detail section (printed below the compact table):
#   - agents:     per-agent run count, status, tokens, duration
#   - tool calls: per agent×tool call counts (from serve.log)
#   - queues:     per-queue processed / pending / dlq counts
#   - artifacts:  per-curing artifact file listing
#   - hides:      total count, size, kind breakdown, webhook sources
#   - scheduled:  per-job run_count and last status (from jobs.json)
#   - notify:     sent/failed counts (from serve.log)
#
# Usage:
#   scripts/examples-summary.sh [--no-color] [--brief] [--out PATH]
#     --brief    print compact table only; skip per-example detail section
#
# Exits 0 always — this is a reporting tool, not a gate.

# Note: intentionally NOT using `set -o pipefail`. Internal pipelines like
# `find | sort -rn | head -1` are deliberate and result in SIGPIPE on the
# upstream commands once head closes, which would otherwise abort the
# entire summary even though every value was computed correctly.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EX_DIR="${ROOT}/examples"
OUT="${EX_DIR}/.last-run-summary.md"
USE_COLOR=1
SHOW_DETAIL=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-color) USE_COLOR=0; shift ;;
    --brief)    SHOW_DETAIL=0; shift ;;
    --out)      OUT="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "examples-summary: unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ── color helpers ───────────────────────────────────────────────────────────
if [[ "${USE_COLOR}" -eq 1 && -t 1 ]]; then
  C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'; C_RESET=$'\033[0m'
  C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'
  C_CYAN=$'\033[36m'; C_BLUE=$'\033[34m'
else
  C_DIM=""; C_BOLD=""; C_RESET=""; C_GREEN=""; C_YELLOW=""; C_RED=""; C_CYAN=""; C_BLUE=""
fi

now_iso="$(date +'%Y-%m-%d %H:%M:%S')"
host="$(hostname 2>/dev/null || echo localhost)"

# Discover example dirs (numeric or rpi-NN prefix) in sorted order.
mapfile -t examples < <(find "${EX_DIR}" -maxdepth 1 -mindepth 1 -type d \( -name '[0-9]*-*' -o -name 'rpi-[0-9]*-*' \) | sort)

# Globals (accumulators).
g_examples=0
g_with_state=0
g_runs=0
g_tokens=0
g_duration_ms=0
g_artifacts=0
g_queue_depth=0
g_dlq=0
g_errors=0
g_warns=0

# Per-example arrays (kept in parallel for re-emission to file).
declare -a row_name row_age row_runs row_status row_tokens row_dur row_arts row_qd row_dlq row_errwarn row_note

# ── helpers ─────────────────────────────────────────────────────────────────
# count_jsonl_lines DIR  — sum non-blank lines across *.jsonl files (recursive).
count_jsonl_lines() {
  local d="$1"
  [[ -d "$d" ]] || { echo 0; return; }
  find "$d" -type f -name '*.jsonl' -print0 2>/dev/null \
    | xargs -0 -r awk 'NF { c++ } END { print c+0 }'
}

# count_files DIR EXT — count files with extension under DIR (recursive).
count_files() {
  local d="$1" pat="$2"
  [[ -d "$d" ]] || { echo 0; return; }
  find "$d" -type f -name "$pat" 2>/dev/null | wc -l | tr -d ' '
}

# newest_mtime DIR — epoch seconds of newest regular file under DIR; 0 if empty.
newest_mtime() {
  local d="$1"
  [[ -d "$d" ]] || { echo 0; return; }
  find "$d" -type f -printf '%T@\n' 2>/dev/null \
    | sort -rn | head -1 | cut -d. -f1 | sed 's/^$/0/'
}

# format_age EPOCH — human age string ("12s ago", "4m ago", "2h ago", "—").
format_age() {
  local ts="$1" now diff
  [[ -z "$ts" || "$ts" -eq 0 ]] && { echo "—"; return; }
  now="$(date +%s)"
  diff=$(( now - ts ))
  if   (( diff < 60 ));    then echo "${diff}s ago"
  elif (( diff < 3600 ));  then echo "$(( diff / 60 ))m ago"
  elif (( diff < 86400 )); then echo "$(( diff / 3600 ))h ago"
  else                          echo "$(( diff / 86400 ))d ago"
  fi
}

# format_ms MILLISECONDS — "12.3s" or "450ms"
format_ms() {
  local ms="$1"
  if (( ms >= 1000 )); then
    awk -v m="$ms" 'BEGIN{ printf("%.1fs", m/1000) }'
  else
    echo "${ms}ms"
  fi
}

# format_bytes N — human-readable byte count
format_bytes() {
  awk -v b="$1" 'BEGIN{
    if(b>=1048576) printf "%.1fMB",b/1048576
    else if(b>=1024) printf "%.1fKB",b/1024
    else printf "%dB",b
  }'
}

# roll_runs JSONL_FILES — emit "RUNS TOK DUR_MS STATUSBREAKDOWN" by summing
# tokens.total, time.duration_ms, and counting status across all jsonl lines.
roll_runs() {
  local files=("$@")
  if [[ ${#files[@]} -eq 0 ]]; then echo "0 0 0 -"; return; fi
  awk '
    /"tokens"/ {
      n=0
      if (match($0, /"total"[[:space:]]*:[[:space:]]*[0-9]+/)) {
        s=substr($0, RSTART, RLENGTH); sub(/.*:[[:space:]]*/, "", s); n=s+0
      }
      tot += n
    }
    /"duration_ms"/ {
      if (match($0, /"duration_ms"[[:space:]]*:[[:space:]]*[0-9]+/)) {
        s=substr($0, RSTART, RLENGTH); sub(/.*:[[:space:]]*/, "", s); dur += (s+0)
      }
    }
    /"status"/ {
      runs++
      if (match($0, /"status"[[:space:]]*:[[:space:]]*"[a-z_]+"/)) {
        s=substr($0, RSTART, RLENGTH); sub(/.*"status"[[:space:]]*:[[:space:]]*"/, "", s); sub(/".*/, "", s)
        status[s]++
      }
    }
    END {
      sb=""
      for (k in status) { sb = sb (sb?",":"") k "=" status[k] }
      if (sb=="") sb="-"
      printf "%d %d %d %s\n", runs+0, tot+0, dur+0, sb
    }
  ' "${files[@]}"
}

# ── detail helpers ────────────────────────────────────────────────────────────

# queue_detail STATE_DIR — emit pipe-delimited rows per queue.
# Row types:
#   flat|<name>|<pending_lines>
#   subdir|<name>|<processed>|<pending>|<dlq_items>
#   dlq|<queue_name>|<items>
queue_detail() {
  local qdir="${1}/queues"
  [[ -d "$qdir" ]] || return 0
  # Flat .jsonl files directly in queues/
  for f in "${qdir}"/*.jsonl; do
    [[ -f "$f" ]] || continue
    local bname; bname="$(basename "${f%.jsonl}")"
    if [[ "$bname" == *-dlq ]]; then
      local n; n="$(awk 'NF{c++}END{print c+0}' "$f")"
      printf 'dlq|%s|%s\n' "${bname%-dlq}" "$n"
    else
      local lines; lines="$(awk 'NF{c++}END{print c+0}' "$f")"
      printf 'flat|%s|%s\n' "$bname" "$lines"
    fi
  done
  # Subdirectory queues
  for d in "${qdir}"/*/; do
    [[ -d "$d" ]] || continue
    local qname; qname="$(basename "$d")"
    local total=0 pending=0 sub_dlq=0
    for f in "${d}"*.jsonl; do
      [[ -f "$f" ]] || continue
      if [[ "$(basename "$f")" == *-dlq.jsonl ]]; then
        local n; n="$(awk 'NF{c++}END{print c+0}' "$f")"
        sub_dlq=$(( sub_dlq + n ))
      else
        total=$(( total + 1 ))
        [[ -s "$f" ]] && pending=$(( pending + 1 ))
      fi
    done
    printf 'subdir|%s|%s|%s|%s\n' "$qname" "$(( total - pending ))" "$pending" "$sub_dlq"
  done
}

# hide_detail STATE_DIR — emit pipe-delimited rows about buffered hides.
# Row types: count|<total>|<size_bytes>  kind|<kind>|<count>  webhook|<name>|<count>
hide_detail() {
  local hdir="${1}/hides"
  [[ -d "$hdir" ]] || return 0
  local count=0 size_total=0
  declare -A kinds webhooks
  for meta in "${hdir}"/*/meta.json; do
    [[ -f "$meta" ]] || continue
    count=$(( count + 1 ))
    local kind; kind="$(grep -oP '"kind"\s*:\s*"\K[^"]+' "$meta" 2>/dev/null || true)"
    local webhook; webhook="$(grep -oP '"webhook"\s*:\s*"\K[^"]+' "$meta" 2>/dev/null || true)"
    local size; size="$(grep -oP '"size_bytes"\s*:\s*\K[0-9]+' "$meta" 2>/dev/null || true)"
    [[ -z "$kind" ]]    && kind="unknown"
    [[ -z "$size" ]]    && size=0
    kinds["$kind"]=$(( ${kinds["$kind"]:-0} + 1 ))
    [[ -n "$webhook" ]] && webhooks["$webhook"]=$(( ${webhooks["$webhook"]:-0} + 1 ))
    size_total=$(( size_total + size ))
  done
  [[ $count -eq 0 ]] && return 0
  printf 'count|%s|%s\n' "$count" "$size_total"
  for k in "${!kinds[@]}";    do printf 'kind|%s|%s\n'    "$k" "${kinds[$k]}";    done
  for w in "${!webhooks[@]}"; do printf 'webhook|%s|%s\n' "$w" "${webhooks[$w]}"; done
}

# artifact_detail STATE_DIR — emit "curing|count|id1,id2,..." rows.
artifact_detail() {
  local adir="${1}/artifacts"
  [[ -d "$adir" ]] || return 0
  for d in "${adir}"/*/; do
    [[ -d "$d" ]] || continue
    local curing; curing="$(basename "$d")"
    local -a files=()
    mapfile -t files < <(find "$d" -maxdepth 1 -name 'artifact_*.json' -type f 2>/dev/null | sort)
    [[ ${#files[@]} -eq 0 ]] && continue
    local ids=""
    for f in "${files[@]}"; do
      local id; id="$(basename "${f%.json}")"
      ids="${ids}${ids:+,}${id}"
    done
    printf '%s|%s|%s\n' "$curing" "${#files[@]}" "$ids"
  done
}

# jobs_detail STATE_DIR — emit "agent_name|run_count|status" rows from jobs.json.
jobs_detail() {
  local jobs="${1}/jobs.json"
  [[ -f "$jobs" ]] || return 0
  awk '
    BEGIN { aname=""; rc=0; st="" }
    /"agent_name"/ {
      s=$0; sub(/.*"agent_name"[^:]*:[^"]*"/,"",s); sub(/".*/,"",s); aname=s
    }
    /"run_count"/ {
      s=$0; sub(/.*"run_count"[^:]*:[[:space:]]*/,"",s); sub(/[^0-9].*/,"",s); rc=s+0
    }
    /"status"/ {
      s=$0; sub(/.*"status"[^:]*:[^"]*"/,"",s); sub(/".*/,"",s); st=s
    }
    /\}/ { if (aname!="") { printf "%s|%s|%s\n", aname, rc, st; aname=""; rc=0; st="" } }
  ' "$jobs" 2>/dev/null
}

# tool_detail SERVE_LOG — emit "agent|tool|count" rows.
tool_detail() {
  local log="$1"
  [[ -f "$log" ]] || return 0
  grep -E 'msg="executing tool"' "$log" 2>/dev/null \
    | sed -E 's/.*agent=([^[:space:]]+).*tool=([^[:space:]]+).*/\1|\2/' \
    | sort | uniq -c \
    | awk '{printf "%s|%s\n", $2, $1}'
}

# notify_detail SERVE_LOG — emit "type|count" rows for notify events.
notify_detail() {
  local log="$1"
  [[ -f "$log" ]] || return 0
  local sent; sent="$(grep -cE 'notify.*sent|notify.*success' "$log" 2>/dev/null || true)"
  local failed; failed="$(grep -cE 'notify.*fail|notify.*error' "$log" 2>/dev/null || true)"
  local init_fail; init_fail="$(grep -cE 'notify backend init failed' "$log" 2>/dev/null || true)"
  [[ "${sent:-0}"       -gt 0 ]] && printf 'sent|%s\n'         "$sent"
  [[ "${failed:-0}"     -gt 0 ]] && printf 'failed|%s\n'       "$failed"
  [[ "${init_fail:-0}"  -gt 0 ]] && printf 'init_failed|%s\n'  "$init_fail"
}

# print_ex_detail EX_DIR — print the per-example detail block to stdout.
print_ex_detail() {
  local ex="$1"
  local state="${ex}/.state"
  local name; name="$(basename "$ex")"
  [[ ! -d "$state" ]] && return 0

  # Sub-header
  local padlen=$(( 76 - ${#name} - 4 ))
  (( padlen < 2 )) && padlen=2
  local pad="" i; for ((i=0; i<padlen; i++)); do pad="${pad}─"; done
  printf '\n%s── %s %s%s\n' "${C_DIM}" "$name" "$pad" "${C_RESET}"

  # agents
  local -a run_files=()
  mapfile -t run_files < <(find "${state}/runs" -maxdepth 1 -name '*.jsonl' -type f 2>/dev/null | sort)
  if [[ ${#run_files[@]} -gt 0 ]]; then
    printf '%s  agents:%s\n' "${C_BOLD}" "${C_RESET}"
    for f in "${run_files[@]}"; do
      local aname; aname="$(basename "${f%.jsonl}")"
      read -r aruns atok adur_ms asb <<<"$(roll_runs "$f")"
      local adur_h; adur_h="$(format_ms "$adur_ms")"
      local s_plural="s"; [[ "$aruns" -eq 1 ]] && s_plural=""
      local sc=""
      case "$asb" in *error*|*fail*) sc="${C_RED}" ;; *success*) sc="${C_GREEN}" ;; esac
      printf '    %-22s  %3s run%s  %s%-22s%s  %6s tok  %s\n' \
        "$aname" "$aruns" "$s_plural" "$sc" "$asb" "${C_RESET}" "$atok" "$adur_h"
    done
  fi

  # tool calls
  if [[ -f "${state}/serve.log" ]]; then
    local -a tool_lines=()
    mapfile -t tool_lines < <(tool_detail "${state}/serve.log")
    if [[ ${#tool_lines[@]} -gt 0 ]]; then
      printf '%s  tool calls:%s\n' "${C_BOLD}" "${C_RESET}"
      for line in "${tool_lines[@]}"; do
        local lagent; lagent="${line%%|*}"
        local rest="${line#*|}"
        local ltool; ltool="${rest%%|*}"
        local lcount; lcount="${rest##*|}"
        printf '    %-22s / %-26s  x%s\n' "$lagent" "$ltool" "$lcount"
      done
    fi
  fi

  # queues
  local -a q_lines=()
  mapfile -t q_lines < <(queue_detail "$state")
  if [[ ${#q_lines[@]} -gt 0 ]]; then
    printf '%s  queues:%s\n' "${C_BOLD}" "${C_RESET}"
    for line in "${q_lines[@]}"; do
      IFS='|' read -ra qf <<<"$line"
      case "${qf[0]}" in
        flat)
          local depth_label
          if [[ "${qf[2]}" -eq 0 ]]; then depth_label="depth=0  (consumed)"
          else depth_label="depth=${qf[2]}  (pending)"; fi
          printf '    %-26s  %s\n' "${qf[1]}" "$depth_label" ;;
        subdir)
          local dlq_part=""
          [[ "${qf[4]:-0}" -gt 0 ]] && dlq_part="${C_RED}  dlq=${qf[4]}${C_RESET}"
          printf '    %-26s  processed=%-4s  pending=%-4s%s\n' \
            "${qf[1]}/" "${qf[2]}" "${qf[3]}" "$dlq_part" ;;
        dlq)
          printf '    %-26s  %s[DLQ] items=%s%s\n' "${qf[1]}" "${C_RED}" "${qf[2]}" "${C_RESET}" ;;
      esac
    done
  fi

  # artifacts
  local -a art_lines=()
  mapfile -t art_lines < <(artifact_detail "$state")
  if [[ ${#art_lines[@]} -gt 0 ]]; then
    printf '%s  artifacts:%s\n' "${C_BOLD}" "${C_RESET}"
    for line in "${art_lines[@]}"; do
      IFS='|' read -r acuring acount aids <<<"$line"
      local s_plural="s"; [[ "$acount" -eq 1 ]] && s_plural=""
      local shown_ids="" shown=0 remaining=0
      IFS=',' read -ra id_arr <<<"$aids"
      for id in "${id_arr[@]}"; do
        if (( shown < 2 )); then
          shown_ids="${shown_ids}${shown_ids:+, }${id}"
          shown=$(( shown + 1 ))
        else
          remaining=$(( remaining + 1 ))
        fi
      done
      [[ $remaining -gt 0 ]] && shown_ids="${shown_ids}  …+${remaining} more"
      printf '    %-26s  %2s file%s   %s\n' "${acuring}/" "$acount" "$s_plural" "$shown_ids"
    done
  fi

  # hides
  local -a hide_lines=()
  mapfile -t hide_lines < <(hide_detail "$state")
  if [[ ${#hide_lines[@]} -gt 0 ]]; then
    local hide_count=0 hide_size=0
    local kind_parts=() webhook_parts=()
    for line in "${hide_lines[@]}"; do
      IFS='|' read -r htype hkey hval <<<"$line"
      case "$htype" in
        count)   hide_count="$hkey"; hide_size="$hval" ;;
        kind)    kind_parts+=("${hkey}×${hval}") ;;
        webhook) webhook_parts+=("${hkey}×${hval}") ;;
      esac
    done
    local size_h; size_h="$(format_bytes "$hide_size")"
    local kinds_str="${kind_parts[*]:-}"
    local wb_str=""
    [[ ${#webhook_parts[@]} -gt 0 ]] && wb_str="  webhooks: ${webhook_parts[*]}"
    printf '%s  hides:%s        %s total  %s  kinds: %s%s\n' \
      "${C_BOLD}" "${C_RESET}" "$hide_count" "$size_h" "$kinds_str" "$wb_str"
  fi

  # scheduled jobs
  local -a job_lines=()
  mapfile -t job_lines < <(jobs_detail "$state")
  if [[ ${#job_lines[@]} -gt 0 ]]; then
    printf '%s  scheduled:%s\n' "${C_BOLD}" "${C_RESET}"
    for line in "${job_lines[@]}"; do
      IFS='|' read -r jagent jruns jstatus <<<"$line"
      local jsc=""
      case "$jstatus" in success) jsc="${C_GREEN}" ;; error|fail*) jsc="${C_RED}" ;; esac
      printf '    %-26s  run_count=%-4s  status=%s%s%s\n' \
        "$jagent" "$jruns" "$jsc" "$jstatus" "${C_RESET}"
    done
  fi

  # notify
  if [[ -f "${state}/serve.log" ]]; then
    local -a notify_lines=()
    mapfile -t notify_lines < <(notify_detail "${state}/serve.log")
    if [[ ${#notify_lines[@]} -gt 0 ]]; then
      printf '%s  notify:%s' "${C_BOLD}" "${C_RESET}"
      for line in "${notify_lines[@]}"; do
        IFS='|' read -r ntype ncount <<<"$line"
        printf '   %s=%s' "$ntype" "$ncount"
      done
      printf '\n'
    fi
  fi
}

# ex_detail_md EX_DIR — emit the per-example detail block as markdown.
ex_detail_md() {
  local ex="$1"
  local state="${ex}/.state"
  local name; name="$(basename "$ex")"
  [[ ! -d "$state" ]] && return 0

  echo "### ${name}"
  echo

  # agents
  local -a run_files=()
  mapfile -t run_files < <(find "${state}/runs" -maxdepth 1 -name '*.jsonl' -type f 2>/dev/null | sort)
  if [[ ${#run_files[@]} -gt 0 ]]; then
    echo "**agents**"
    echo
    echo "| agent | runs | status | tokens | duration |"
    echo "|---|---:|---|---:|---:|"
    for f in "${run_files[@]}"; do
      local aname; aname="$(basename "${f%.jsonl}")"
      read -r aruns atok adur_ms asb <<<"$(roll_runs "$f")"
      local adur_h; adur_h="$(format_ms "$adur_ms")"
      printf '| %s | %s | %s | %s | %s |\n' "$aname" "$aruns" "$asb" "$atok" "$adur_h"
    done
    echo
  fi

  # tool calls
  if [[ -f "${state}/serve.log" ]]; then
    local -a tool_lines=()
    mapfile -t tool_lines < <(tool_detail "${state}/serve.log")
    if [[ ${#tool_lines[@]} -gt 0 ]]; then
      echo "**tool calls** (from serve.log)"
      echo
      echo "| agent | tool | calls |"
      echo "|---|---|---:|"
      for line in "${tool_lines[@]}"; do
        local lagent; lagent="${line%%|*}"
        local rest="${line#*|}"
        local ltool; ltool="${rest%%|*}"
        local lcount; lcount="${rest##*|}"
        printf '| %s | %s | %s |\n' "$lagent" "$ltool" "$lcount"
      done
      echo
    fi
  fi

  # queues
  local -a q_lines=()
  mapfile -t q_lines < <(queue_detail "$state")
  if [[ ${#q_lines[@]} -gt 0 ]]; then
    echo "**queues**"
    echo
    echo "| queue | type | processed | pending | dlq |"
    echo "|---|---|---:|---:|---:|"
    for line in "${q_lines[@]}"; do
      IFS='|' read -ra qf <<<"$line"
      case "${qf[0]}" in
        flat)   printf '| %s | flat | — | %s | 0 |\n'              "${qf[1]}" "${qf[2]}" ;;
        subdir) printf '| %s/ | subdir | %s | %s | %s |\n'         "${qf[1]}" "${qf[2]}" "${qf[3]}" "${qf[4]:-0}" ;;
        dlq)    printf '| %s | dlq | — | — | %s |\n'               "${qf[1]}" "${qf[2]}" ;;
      esac
    done
    echo
  fi

  # artifacts
  local -a art_lines=()
  mapfile -t art_lines < <(artifact_detail "$state")
  if [[ ${#art_lines[@]} -gt 0 ]]; then
    echo "**artifacts**"
    echo
    echo "| curing | count | ids |"
    echo "|---|---:|---|"
    for line in "${art_lines[@]}"; do
      IFS='|' read -r acuring acount aids <<<"$line"
      local shown_ids="" shown=0 remaining=0
      IFS=',' read -ra id_arr <<<"$aids"
      for id in "${id_arr[@]}"; do
        if (( shown < 3 )); then
          shown_ids="${shown_ids}${shown_ids:+, }\`${id}\`"
          shown=$(( shown + 1 ))
        else
          remaining=$(( remaining + 1 ))
        fi
      done
      [[ $remaining -gt 0 ]] && shown_ids="${shown_ids} …+${remaining}"
      printf '| %s/ | %s | %s |\n' "$acuring" "$acount" "$shown_ids"
    done
    echo
  fi

  # hides
  local -a hide_lines=()
  mapfile -t hide_lines < <(hide_detail "$state")
  if [[ ${#hide_lines[@]} -gt 0 ]]; then
    local hide_count=0 hide_size=0
    local kind_parts=() webhook_parts=()
    for line in "${hide_lines[@]}"; do
      IFS='|' read -r htype hkey hval <<<"$line"
      case "$htype" in
        count)   hide_count="$hkey"; hide_size="$hval" ;;
        kind)    kind_parts+=("${hkey}×${hval}") ;;
        webhook) webhook_parts+=("${hkey}×${hval}") ;;
      esac
    done
    local size_h; size_h="$(format_bytes "$hide_size")"
    echo "**hides** — ${hide_count} total, ${size_h}"
    echo
    echo "| dimension | values |"
    echo "|---|---|"
    [[ ${#kind_parts[@]}    -gt 0 ]] && printf '| kinds    | %s |\n' "${kind_parts[*]}"
    [[ ${#webhook_parts[@]} -gt 0 ]] && printf '| webhooks | %s |\n' "${webhook_parts[*]}"
    echo
  fi

  # scheduled
  local -a job_lines=()
  mapfile -t job_lines < <(jobs_detail "$state")
  if [[ ${#job_lines[@]} -gt 0 ]]; then
    echo "**scheduled** (from jobs.json)"
    echo
    echo "| agent | run_count | last status |"
    echo "|---|---:|---|"
    for line in "${job_lines[@]}"; do
      IFS='|' read -r jagent jruns jstatus <<<"$line"
      printf '| %s | %s | %s |\n' "$jagent" "$jruns" "$jstatus"
    done
    echo
  fi

  # notify
  if [[ -f "${state}/serve.log" ]]; then
    local -a notify_lines=()
    mapfile -t notify_lines < <(notify_detail "${state}/serve.log")
    if [[ ${#notify_lines[@]} -gt 0 ]]; then
      echo "**notify**"
      echo
      for line in "${notify_lines[@]}"; do
        IFS='|' read -r ntype ncount <<<"$line"
        echo "- ${ntype}: ${ncount}"
      done
      echo
    fi
  fi
}

# ── per-example pass ────────────────────────────────────────────────────────
for ex in "${examples[@]}"; do
  name="$(basename "$ex")"
  state="${ex}/.state"
  g_examples=$(( g_examples + 1 ))

  if [[ ! -d "$state" ]]; then
    row_name+=("$name"); row_age+=("—"); row_runs+=("0"); row_status+=("-")
    row_tokens+=("0"); row_dur+=("0ms"); row_arts+=("0"); row_qd+=("0")
    row_dlq+=("0"); row_errwarn+=("0/0"); row_note+=("no .state (CLI-only example)")
    continue
  fi
  g_with_state=$(( g_with_state + 1 ))

  newest="$(newest_mtime "$state")"
  age="$(format_age "$newest")"

  # Runs roll-up.
  mapfile -t run_files < <(find "${state}/runs" -type f -name '*.jsonl' 2>/dev/null)
  read -r runs tokens dur_ms status_brk <<<"$(roll_runs "${run_files[@]}")"
  g_runs=$(( g_runs + runs ))
  g_tokens=$(( g_tokens + tokens ))
  g_duration_ms=$(( g_duration_ms + dur_ms ))

  # Artifacts.
  arts="$(count_files "${state}/artifacts" 'artifact_*.json')"
  g_artifacts=$(( g_artifacts + arts ))

  # Queues split into live vs DLQ (flat files and subdir DLQ items).
  qd=0; dlq=0
  if [[ -d "${state}/queues" ]]; then
    for f in "${state}"/queues/*.jsonl; do
      [[ -f "$f" ]] || continue
      lines="$(awk 'NF{c++} END{print c+0}' "$f")"
      case "$(basename "$f")" in
        *-dlq.jsonl) dlq=$(( dlq + lines )) ;;
        *)           qd=$(( qd + lines ))   ;;
      esac
    done
    # Subdir queues: pending non-empty files + DLQ
    for f in "${state}"/queues/*/*.jsonl; do
      [[ -f "$f" ]] || continue
      case "$(basename "$f")" in
        *-dlq.jsonl)
          lines="$(awk 'NF{c++} END{print c+0}' "$f")"
          dlq=$(( dlq + lines )) ;;
        *)
          [[ -s "$f" ]] && qd=$(( qd + 1 )) ;;
      esac
    done
  fi
  g_queue_depth=$(( g_queue_depth + qd ))
  g_dlq=$(( g_dlq + dlq ))

  # serve.log error/warn counts (whole file — these accumulate across runs).
  errs=0; warns=0; last_err=""
  if [[ -f "${state}/serve.log" ]]; then
    # grep -c outputs the count even when zero matches but exits 1; capture
    # the count and suppress the non-zero exit so `set -e` doesn't trip.
    errs="$(grep -cE 'level=ERROR' "${state}/serve.log" 2>/dev/null || true)"
    warns="$(grep -cE 'level=WARN'  "${state}/serve.log" 2>/dev/null || true)"
    errs="${errs:-0}"; warns="${warns:-0}"
    last_err="$(grep -E 'level=ERROR' "${state}/serve.log" 2>/dev/null | tail -1 \
                 | sed -E 's/.*msg="([^"]{1,72}).*/\1/' || true)"
  fi
  g_errors=$(( g_errors + errs ))
  g_warns=$(( g_warns + warns ))

  # Format duration.
  dur_h="$(format_ms "$dur_ms")"

  note=""
  (( dlq > 0 ))   && note+="${note:+; }dlq=${dlq}"
  (( errs > 0 )) && note+="${note:+; }err=${errs}"
  (( runs == 0 && arts == 0 )) && note+="${note:+; }(no run records found)"
  [[ -z "$note" ]] && note="ok"

  row_name+=("$name"); row_age+=("$age"); row_runs+=("$runs"); row_status+=("$status_brk")
  row_tokens+=("$tokens"); row_dur+=("$dur_h"); row_arts+=("$arts"); row_qd+=("$qd")
  row_dlq+=("$dlq"); row_errwarn+=("${errs}/${warns}"); row_note+=("$note")
done

# ── totals formatting ───────────────────────────────────────────────────────
g_dur_h="$(format_ms "$g_duration_ms")"
g_avg_tok=0
(( g_runs > 0 )) && g_avg_tok=$(( g_tokens / g_runs ))

# ── pretty console output ───────────────────────────────────────────────────
hr() { printf '%s%s%s\n' "${C_DIM}" "────────────────────────────────────────────────────────────────────────────────" "${C_RESET}"; }
echo
printf '%s%sleather examples — run summary%s   %s%s on %s%s\n' \
  "${C_BOLD}" "${C_CYAN}" "${C_RESET}" "${C_DIM}" "${now_iso}" "${host}" "${C_RESET}"
hr
printf '  examples:%s%d total / %d with .state%s\n' "${C_BOLD}" "${g_examples}" "${g_with_state}" "${C_RESET}"
printf '  runs:    %s%d%s   tokens=%s%s%s   duration=%s%s%s   avg_tokens/run=%s%s%s\n' \
  "${C_BOLD}" "${g_runs}" "${C_RESET}" \
  "${C_BOLD}" "${g_tokens}" "${C_RESET}" \
  "${C_BOLD}" "${g_dur_h}" "${C_RESET}" \
  "${C_BOLD}" "${g_avg_tok}" "${C_RESET}"
printf '  artifacts:%s%d%s   queue=%s%d%s   dlq=' "${C_BOLD}" "${g_artifacts}" "${C_RESET}" "${C_BOLD}" "${g_queue_depth}" "${C_RESET}"
if (( g_dlq > 0 )); then printf '%s%d%s' "${C_RED}" "${g_dlq}" "${C_RESET}"; else printf '%s%d%s' "${C_BOLD}" "${g_dlq}" "${C_RESET}"; fi
printf '   serve.log: errors='
if (( g_errors > 0 )); then printf '%s%d%s' "${C_RED}" "${g_errors}" "${C_RESET}"; else printf '%s%d%s' "${C_BOLD}" "${g_errors}" "${C_RESET}"; fi
printf '  warns='
if (( g_warns > 0 )); then printf '%s%d%s' "${C_YELLOW}" "${g_warns}" "${C_RESET}"; else printf '%s%d%s' "${C_BOLD}" "${g_warns}" "${C_RESET}"; fi
echo
hr

# Per-example table.
printf '%s%-22s %-9s %5s %-22s %7s %8s %5s %5s %5s %-7s %s%s\n' \
  "${C_BOLD}" "example" "last act" "runs" "status" "tokens" "duration" "arts" "queue" "dlq" "err/wrn" "notes" "${C_RESET}"
for i in "${!row_name[@]}"; do
  status_color=""
  case "${row_status[$i]}" in
    *error*|*timeout*|*fail*) status_color="${C_RED}" ;;
    *success*|*ok*)           status_color="${C_GREEN}" ;;
    *)                        status_color="${C_DIM}" ;;
  esac
  dlq_color=""
  [[ "${row_dlq[$i]}" != "0" ]] && dlq_color="${C_RED}"
  err_color=""
  [[ "${row_errwarn[$i]%/*}" != "0" ]] && err_color="${C_YELLOW}"
  printf '%-22s %-9s %5s %s%-22s%s %7s %8s %5s %5s %s%5s%s %s%-7s%s %s\n' \
    "${row_name[$i]}" "${row_age[$i]}" "${row_runs[$i]}" \
    "${status_color}" "${row_status[$i]}" "${C_RESET}" \
    "${row_tokens[$i]}" "${row_dur[$i]}" "${row_arts[$i]}" "${row_qd[$i]}" \
    "${dlq_color}" "${row_dlq[$i]}" "${C_RESET}" \
    "${err_color}" "${row_errwarn[$i]}" "${C_RESET}" \
    "${row_note[$i]}"
done
hr

# ── per-example detail section ───────────────────────────────────────────────
if [[ "${SHOW_DETAIL}" -eq 1 ]]; then
  printf '\n%s%s  per-example detail  %s\n' "${C_BOLD}" "${C_CYAN}" "${C_RESET}"
  for ex in "${examples[@]}"; do
    print_ex_detail "$ex"
  done
  echo
fi

printf '%sfull rollup written to %s%s\n' "${C_DIM}" "${OUT/#${ROOT}\//}" "${C_RESET}"
printf '%snote:%s all per-example serve processes have stopped. The DevTools URL\n' "${C_DIM}" "${C_RESET}"
printf '      printed during the run is no longer reachable. To browse state in\n'
printf '      a live UI, start a viewer against one example:\n'
printf '        %scd examples && make view EX=10%s   %s# any example with .state/%s\n\n' "${C_BOLD}" "${C_RESET}" "${C_DIM}" "${C_RESET}"

# ── markdown file output ────────────────────────────────────────────────────
{
  echo "# leather examples — run summary"
  echo
  echo "_Generated ${now_iso} on ${host}_"
  echo
  echo "## Totals"
  echo
  echo "| metric | value |"
  echo "|---|---|"
  echo "| examples discovered | ${g_examples} |"
  echo "| with .state/        | ${g_with_state} |"
  echo "| total run records   | ${g_runs} |"
  echo "| total tokens        | ${g_tokens} |"
  echo "| total run duration  | ${g_dur_h} |"
  echo "| avg tokens per run  | ${g_avg_tok} |"
  echo "| artifacts produced  | ${g_artifacts} |"
  echo "| queue depth (live)  | ${g_queue_depth} |"
  echo "| DLQ entries         | ${g_dlq} |"
  echo "| serve.log ERRORs    | ${g_errors} |"
  echo "| serve.log WARNs     | ${g_warns} |"
  echo
  echo "## Per-example summary"
  echo
  echo "| example | last activity | runs | status breakdown | tokens | duration | artifacts | queue | dlq | err / warn | notes |"
  echo "|---|---|---:|---|---:|---:|---:|---:|---:|---:|---|"
  for i in "${!row_name[@]}"; do
    printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
      "${row_name[$i]}" "${row_age[$i]}" "${row_runs[$i]}" "${row_status[$i]}" \
      "${row_tokens[$i]}" "${row_dur[$i]}" "${row_arts[$i]}" "${row_qd[$i]}" \
      "${row_dlq[$i]}" "${row_errwarn[$i]}" "${row_note[$i]}"
  done
  echo
  echo "## Per-example detail"
  echo
  for ex in "${examples[@]}"; do
    ex_detail_md "$ex"
  done
  echo "## Notes"
  echo
  echo "- Counts are computed from current contents of \`examples/<NN>-*/.state/\` and"
  echo "  include carry-over from prior runs that were not reset. Use \`make reset\`"
  echo "  inside \`examples/\` before \`make all\` to scope counts to a single run."
  echo "- Token totals and durations are summed across every run record in"
  echo "  \`.state/runs/*.jsonl\` (status is per-run, not per-curing-item)."
  echo "- \`status breakdown\` shows \`success=N,error=N\` collected from each run record."
  echo "- DLQ entries that pre-date the latest run are still counted; inspect"
  echo "  \`.state/queues/<name>-dlq.jsonl\` per example for details."
  echo "- Queue \`processed\` counts for subdir-pattern queues are inferred from"
  echo "  consumed (empty) item files; flat queues show current pending depth."
  echo "- Tool call counts are extracted from serve.log \`msg=\"executing tool\"\` lines."
  echo "- Hide sizes are summed from \`meta.json size_bytes\` fields."
} > "${OUT}"
