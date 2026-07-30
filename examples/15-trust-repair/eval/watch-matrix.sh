#!/usr/bin/env bash
# Live status of trust-repair runs, ranked by outcome.
#   bash eval/watch-matrix.sh              # one snapshot
#   LOOP=1 bash eval/watch-matrix.sh       # live, redraw in place
#   NOCOLOR=1 bash eval/watch-matrix.sh    # plain, for piping
#
# Cells are RANKED, not listed in run order: the question asked of this screen
# is "which arm is passing, and which arms merely talk" — run order buries it.
#
# Two callouts exist because each has already silently changed an experiment:
#   TRUNC  finish_reason=length — the model hit the max_tokens ceiling
#          mid-reasoning and the cell measured the token budget, not the
#          model (see quarantine/pilot-35b-R-pin-v1).
#   narr   a REPAIRED claim over an empty diff — the honest-report failure
#          mode; the conjunction catches it, this column makes it visible.
HERE="$(cd "$(dirname "$0")" && pwd)"
RUNS="$HERE/results/runs"
QUAR="$HERE/results/quarantine"

if [ -z "${NOCOLOR:-}" ]; then
  B=$'\033[1m'; D=$'\033[2m'; R=$'\033[0m'
  GRN=$'\033[32m'; YEL=$'\033[33m'; RED=$'\033[31m'; CYN=$'\033[36m'; MAG=$'\033[35m'
else
  B=; D=; R=; GRN=; YEL=; RED=; CYN=; MAG=
fi

rule() { printf '  %s%s%s\n' "$D" "$(printf '%.0s-' $(seq 1 76))" "$R"; }

snapshot() {
  printf '\n  %sTRUST-REPAIR PILOT%s %s%s%s\n' "$B" "$R" "$D" "$(date +%H:%M:%S)" "$R"
  rule
  printf '  %s%-24s %-6s %-7s %3s %3s %6s %6s  %s%s\n' \
         "$D" cell pass fails wr sc diff ktok "model/arm/instance" "$R"

  local grand_tok=0 live=0 done_n=0 pass_n=0

  # Completed cells: verdict.json is the source of truth, ranked pass-first
  # then by write activity (the axis this pilot measures).
  if [ -d "$RUNS" ]; then
    for d in "$RUNS"/*/; do
      d="${d%/}"; [ -d "$d" ] || continue
      [ -f "$d/verdict.json" ] || continue
      python3 - "$d" <<'PYEOF'
import json, os, re, sys
d = sys.argv[1]
v = json.load(open(os.path.join(d, "verdict.json")))
letters = {"targeted_removed":"T","exploit_blocked":"X","behavior_kept":"B",
           "repo_tests":"P","no_new_findings":"N","no_suppression":"S"}
fails = "".join(l for k, l in letters.items() if not v["checks"].get(k, True)) or "-"
log = ""
try: log = open(os.path.join(d, "run.log"), errors="replace").read()
except Exception: pass
wr = len(re.findall(r"tool=write_file", log))
sc = len(re.findall(r"tool=scan_repo", log))
tok = sum(int(m) for m in re.findall(r"tokens=(\d+)", log))
trunc = "finish_reason=length" in log
try:
    m = json.load(open(os.path.join(d, "run-manifest.json")))
    model = m.get("model") or m.get("agent_mode", "?")
    short = "35b" if "35b" in model else ("4b" if "4b" in model else model[:8])
    who = f"{short}/{m.get('arm','?')}/{m.get('instance','?').split('/')[1] if '/' in m.get('instance','') else '?'}"
except Exception:
    who = "?"
diff = os.path.getsize(os.path.join(d, "patch.diff")) if os.path.exists(os.path.join(d, "patch.diff")) else 0
narr = (not v["pass"]) and diff == 0 and wr == 0
# sortkey rank: passes first, then most-active fails, then the silent ones
key = (0 if v["pass"] else 1, 9999 - wr, 999999 - diff, os.path.basename(d))
print("\t".join(str(x) for x in (
    f"{key[0]}|{key[1]:04d}|{key[2]:06d}|{key[3]}",
    os.path.basename(d), "PASS" if v["pass"] else "fail", fails,
    wr, sc, diff, round(tok/1000, 1), who,
    int(trunc), int(narr), tok, int(v["pass"]))))
PYEOF
    done | sort -t'|' -k1,1 | sort -s -t$'\t' -k1,1 | while IFS=$'\t' read -r _ tag pass fails wr sc diff ktok who trunc narr tok is_pass; do
      local pc fc
      if [ "$pass" = PASS ]; then pc="$GRN$B"; else pc="$D"; fi
      fc=""; [ "$fails" != "-" ] && fc="$RED"
      printf '  %-24s %s%-6s%s %s%-7s%s %3s %3s %6s %6s  %s%s%s' \
             "$tag" "$pc" "$pass" "$R" "$fc" "$fails" "$R" "$wr" "$sc" "$diff" "$ktok" "$D" "$who" "$R"
      [ "$trunc" = 1 ] && printf ' %sTRUNC%s' "$RED$B" "$R"
      [ "$narr" = 1 ] && printf ' %snarr%s' "$YEL" "$R"
      printf '\n'
    done
  fi

  # In-flight cells: run dir without a verdict; progress from the live log.
  if [ -d "$RUNS" ]; then
    for d in "$RUNS"/*/; do
      d="${d%/}"; [ -f "$d/verdict.json" ] && continue; [ -d "$d" ] || continue
      local rounds wr sc age stale
      rounds=$(grep -c "calling LLM" "$d/run.log" 2>/dev/null | head -1); rounds=${rounds:-0}
      wr=$(grep -c "tool=write_file" "$d/run.log" 2>/dev/null | head -1); wr=${wr:-0}
      sc=$(grep -c "tool=scan_repo" "$d/run.log" 2>/dev/null | head -1); sc=${sc:-0}
      age=999999; [ -f "$d/run.log" ] && age=$(( $(date +%s) - $(command stat -c %Y "$d/run.log") ))
      stale=""; [ "$age" -gt 120 ] && stale=" ${D}(STALE ${age}s)${R}"
      printf '  %s%-24s %sRUN%s    round %-2s          wr %-3s sc %-3s%s%s\n' \
             "$CYN" "$(basename "$d")" "$GRN$B" "$R$CYN" "$rounds" "$wr" "$sc" "$R" "$stale"
      live=$((live+1))
    done
  fi

  # Footer tallies re-derived in shell (the while above ran in a subshell).
  done_n=$(ls "$RUNS"/*/verdict.json 2>/dev/null | wc -l)
  pass_n=$(grep -l '"pass": true' "$RUNS"/*/verdict.json 2>/dev/null | wc -l)
  grand_tok=$(cat "$RUNS"/*/run.log 2>/dev/null | grep -oE 'tokens=[0-9]+' | awk -F= '{s+=$2} END{print s+0}')
  rule
  printf '  %s%s passed / %s scored - %s in flight - %s grid procs - %sk tok' \
    "$D" "$pass_n" "$done_n" "$live" "$(pgrep -fc 'grid.*\.sh' 2>/dev/null | head -1)" "$(( ${grand_tok:-0} / 1000 ))"
  [ -d "$QUAR" ] && printf ' - quarantined %s' "$(ls "$QUAR" 2>/dev/null | wc -l)"
  printf '%s\n' "$R"
}

if [ "${LOOP:-0}" = "1" ]; then
  # In-place redraw, ex-14 style: park the cursor at home, overwrite per row,
  # erase line tails with \033[K and leftover rows with \033[J — no clear(1)
  # flash, no scrollback churn.
  printf '\033[?25l'
  trap 'printf "\033[?25h\033[J\n"; exit 0' INT TERM
  while :; do
    out="$(snapshot)"
    printf '\033[H'
    printf '%s\n' "$out" | sed $'s/$/\033[K/'
    printf '\033[J'
    sleep "${INTERVAL:-5}"
  done
else
  snapshot
fi
