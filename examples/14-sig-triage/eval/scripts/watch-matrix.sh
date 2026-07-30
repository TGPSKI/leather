#!/usr/bin/env bash
# Live status across every battery on both rigs, ranked by accuracy.
#   LOOP=1 bash eval/scripts/watch-matrix.sh  # live, redraw in place
#   NOCOLOR=1 bash eval/scripts/watch-matrix.sh    # plain, for piping
#   RIG=4b bash eval/scripts/watch-matrix.sh              # one rig only
#   SCOPE=confirmatory bash eval/scripts/watch-matrix.sh  # registered -cN draws only
#   FILTER=4b-T2* bash eval/scripts/watch-matrix.sh       # arbitrary pattern
# The three compose. SCOPE separates the registered ablation matrix from the
# exploratory atlas — watching both at once was the thing that made this
# screen unreadable.
#
# For interactive filtering, arm rankings and contrast views:
#   python3 eval/scripts/matrix-tui.py            ([f] filters, [tab] cycles)
#
# Cells are RANKED, not listed in run order: the question asked of this screen is
# always "which arm is winning", and run order buries that.
#
# Three callouts exist because each has already silently changed an experiment:
#   PAGINATED  an oversized hide puts leather into reflection mode (paging
#              preamble, tools stripped per turn, N+1 alternating turns). An arm
#              that believes it is single-turn but paginates is measuring a
#              different delivery mechanism than its name claims.
#   UNATTRIB   LLM calls the proxy could not attribute to a stage — context
#              summarization fires silently, so unexplained calls are its only trace.
#   STALE      artifact files persist after a run ends, so counting them
#              unconditionally reports progress for a run that is not running.
# resolved BEFORE snapshot() cd's into $EX, or dirname "$0" points nowhere
HERE="$(cd "$(dirname "$0")" && pwd)"
EX="$(cd "$HERE/../.." && pwd)"
# confirmatory-battery was missing here, so the registered battery — the one
# most likely to be running — reported "idle" for its entire 12-hour run.
BATTERIES="confirmatory-battery:confirmatory run-battery:fin noise-battery:noise overnight-battery:overnight"

if [ -z "${NOCOLOR:-}" ]; then
  B=$'\033[1m'; D=$'\033[2m'; R=$'\033[0m'
  GRN=$'\033[32m'; YEL=$'\033[33m'; RED=$'\033[31m'; CYN=$'\033[36m'; MAG=$'\033[35m'
else
  B=; D=; R=; GRN=; YEL=; RED=; CYN=; MAG=
fi

rule() { printf '  %s%s%s\n' "$D" "$(printf '%.0s-' $(seq 1 68))" "$R"; }

snapshot() {
  cd "$EX" || return
  printf '\n  %sEVAL BATTERIES%s %s%s%s\n' "$B" "$R" "$D" "$(date +%H:%M:%S)" "$R"
  rule
  local grand_tok=0 grand_calls=0
  for r in ${RIG:-35b 4b}; do
    local S="eval/.state-eval-$r" livename=""
    for b in $BATTERIES; do
      local script="${b%%:*}"
      pgrep -f "${script}.sh $r" >/dev/null 2>&1 && livename="$script"
    done
    if [ -n "$livename" ]; then
      printf '  %s%-4s%s %sRUNNING %s%s\n' "$B" "$r" "$R" "$GRN" "$livename" "$R"
    else
      printf '  %s%-4s%s %sidle%s\n' "$B" "$r" "$R" "$D" "$R"
    fi

    local an mt tools pages err age stale tok calls other tag bar pct
    an=$(ls "$S/artifacts/analyze" 2>/dev/null | wc -l)
    mt=$(ls "$S/artifacts/match"   2>/dev/null | wc -l)
    tools=$(grep -c 'executing tool' "$S/run.log" 2>/dev/null); tools=${tools:-0}
    pages=$(grep -cE 'tool=hide_(next|jump)' "$S/run.log" 2>/dev/null); pages=${pages:-0}
    reflect=$(grep -c 'reflection mode active' "$S/run.log" 2>/dev/null); reflect=${reflect:-0}
    err=$(grep -c 'hide missing' "$S/run.log" 2>/dev/null); err=${err:-0}
    read -r calls tok <<<"$(grep -oE 'agent=[a-z]+ tokens=[0-9]+' "$S/run.log" 2>/dev/null |
      awk -F'tokens=' '{s+=$2; n++} END {print (n?n:0), (s?s:0)}')"
    other=$(python3 -c "
import json
try: print(sum(1 for l in open('$S/logprobs.jsonl') if l.strip() and json.loads(l).get('stage')=='other'))
except Exception: print(0)" 2>/dev/null)
    age=999999; [ -f "$S/run.log" ] && age=$(( $(date +%s) - $(command stat -c %Y "$S/run.log") ))
    stale=""; [ "$age" -gt 90 ] && stale="${D} (STALE ${age}s)${R}"
    tag=$(python3 -c "import json;print(json.load(open('$S/run-manifest.json'))['run_tag'])" 2>/dev/null)

    if [ -n "$livename" ] || [ "$age" -le 90 ]; then
      pct=$(( mt * 24 / 250 ))
      bar="$(printf '%*s' "$pct" '' | tr ' ' '#')$(printf '%*s' "$((24-pct))" '' | tr ' ' '.')"
      printf '     %s%-11s%s %s%s%s %s%3d/250%s %sanalyze %-4s tools %-5s %s tok%s%s\n' \
             "$MAG" "${tag:-?}" "$R" "$CYN" "$bar" "$R" "$B" "$mt" "$R" \
             "$D" "$an" "$tools" "${tok:-0}" "$R" "$stale"
      [ "${err:-0}"   -gt 0 ] && printf '       %sx %s hide-missing%s\n' "$RED" "$err" "$R"
      [ "${reflect:-0}" -gt 0 ] && printf '       %sx REFLECTION MODE - hide paginated (%s nav calls)%s\n' "$RED$B" "$pages" "$R"
      [ "${reflect:-0}" = 0 ] && [ "${pages:-0}" -gt 0 ] && printf '       %s~ %s spurious hide-nav call(s) on single-page hides (wasted rounds)%s\n' "$YEL" "$pages" "$R"
      [ "${other:-0}" -gt 0 ] && printf '       %s! %s unattributed LLM calls (summarization is silent)%s\n' "$YEL" "$other" "$R"
    fi

    printf '\n'
    # Table LAST: the in-flight cell is the only thing on this screen that
    # changes second to second, so it belongs where the eye lands first —
    # not buried under 30 finished rows that will not move again.
    RIG="$r" NOCOLOR="${NOCOLOR:-}" FILTER="${FILTER:-}" SCOPE="${SCOPE:-all}" \
      SORT="${SORT:-acc}" SORT_REV="${SORT_REV:-0}" LEGEND=0 \
      python3 "$HERE/table.py"
    grand_tok=$(( grand_tok + ${tok:-0} )); grand_calls=$(( grand_calls + ${calls:-0} ))
    printf '\n'
  done
  rule
  printf '  %sprocs %s - archived %s - live cells %s calls / %s tok%s\n' \
    "$D" "$(pgrep -f 'run-eval.sh' | grep -cv '^$')" \
    "$(ls -d eval/results/runs/*/ 2>/dev/null | wc -l)" "$grand_calls" "$grand_tok" "$R"
  # Glossary lives at the BOTTOM: it is reference material you consult, not
  # a banner you read past on every redraw.
  if [ "${HELP:-0}" = "1" ]; then
    NOCOLOR="${NOCOLOR:-}" python3 -c "
import os, sys
sys.path.insert(0, '$HERE')
import matrixdata as md
d, r = ('', '') if os.environ.get('NOCOLOR') else ('\033[2m', '\033[0m')
print(f'  {d}column definitions  [?] hides{r}')
for name, text in md.COLUMN_HELP:
    print(f'     {d}{name:<10} {text}{r}')
"
  fi
  NOCOLOR="${NOCOLOR:-}" python3 -c "
import os, sys
sys.path.insert(0, '$HERE')
import matrixdata as md
for l in md.legend(bool(os.environ.get('NOCOLOR'))):
    print(l)
"
}

if [ "${LOOP:-0}" = "1" ]; then
  # `clear` erases the screen and then redraws, which flashes on every tick and
  # churns scrollback. Instead: park the cursor at home, redraw in place, and
  # erase to end-of-line per row so shorter lines do not leave tails. \033[J at
  # the end drops any rows left over when the layout shrinks. Only the clock, the
  # live progress row and the totals actually change -- but a completing cell
  # ADDS a row and shifts everything below it, so full in-place redraw is simpler
  # and cheaper than tracking which rows moved.
  printf '\033[?25l'                       # hide cursor
  trap 'printf "\033[?25h\033[J\n"; exit 0' INT TERM
  # `read -t` IS the tick: one call both waits out the redraw interval and
  # collects a keystroke, so sorting is interactive without a second thread,
  # a curses dependency, or any change to the redraw model. A bare timeout
  # (no key) falls through and redraws exactly as the old `sleep` did.
  SCROLL=${SCROLL:-0}
  while :; do
    mapfile -t VIEW < <(snapshot)
    total=${#VIEW[@]}
    term_rows=$(tput lines 2>/dev/null || echo 40)
    term_cols=$(tput cols  2>/dev/null || echo 100)
    vh=$(( term_rows - 2 ))            # one row for the key line, one spare
    [ "$vh" -lt 5 ] && vh=5
    max_off=0; [ "$total" -gt "$vh" ] && max_off=$(( total - vh ))
    [ "$SCROLL" -gt "$max_off" ] && SCROLL=$max_off
    [ "$SCROLL" -lt 0 ] && SCROLL=0

    printf '\033[H'
    for (( i = 0; i < vh; i++ )); do
      printf '%s\033[K\n' "${VIEW[$(( SCROLL + i ))]:-}"
    done

    # Scrollbar only when the content actually overflows: a thumb sized to
    # the visible fraction, positioned by scroll offset, drawn in the last
    # column so it never collides with a row that happens to be full width.
    if [ "$total" -gt "$vh" ]; then
      thumb=$(( vh * vh / total )); [ "$thumb" -lt 1 ] && thumb=1
      tpos=0; [ "$max_off" -gt 0 ] && tpos=$(( SCROLL * (vh - thumb) / max_off ))
      for (( i = 0; i < vh; i++ )); do
        if [ "$i" -ge "$tpos" ] && [ "$i" -lt $(( tpos + thumb )) ]; then
          printf '\033[%d;%dH%s█%s' $(( i + 1 )) "$term_cols" "$CYN" "$R"
        else
          printf '\033[%d;%dH%s│%s' $(( i + 1 )) "$term_cols" "$D" "$R"
        fi
      done
    fi

    printf '\033[%d;1H' $(( vh + 1 ))
    printf '  %s[a]cc [t]ools [K]tok [d]ur [n]o-out [c]ell  [r]ev  [?]cols  ' "$D"
    if [ "$total" -gt "$vh" ]; then
      printf '%s[jk/↑↓ PgUp/Dn g/G] %d-%d/%d%s  ' \
             "$CYN" $(( SCROLL + 1 )) $(( SCROLL + vh )) "$total" "$D"
    fi
    printf '[q]uit%s\033[K' "$R"
    printf '\033[J'

    if read -rsn1 -t "${INTERVAL:-5}" key 2>/dev/null; then
      # Arrow keys arrive as ESC [ A/B/5~/6~; grab the tail without blocking.
      if [ "$key" = $'\033' ]; then
        read -rsn2 -t 0.05 rest 2>/dev/null
        case "$rest" in
          '[A') key=k ;; '[B') key=j ;;
          '[5') read -rsn1 -t 0.05 _ 2>/dev/null; key=u ;;
          '[6') read -rsn1 -t 0.05 _ 2>/dev/null; key=f ;;
          *) key="" ;;
        esac
      fi
      case "$key" in
        a) SORT=acc ;; t) SORT=tools ;; d) SORT=dur ;;
        n) SORT=noout ;; c) SORT=tag ;;
        # 'k' is scroll-up (vi), so ktok sorts on 'K'.
        K) SORT=ktok ;;
        r) [ "${SORT_REV:-0}" = 1 ] && SORT_REV=0 || SORT_REV=1 ;;
        '?'|h) [ "${HELP:-0}" = 1 ] && HELP=0 || HELP=1 ;;
        j) SCROLL=$(( SCROLL + 1 )) ;;
        k) SCROLL=$(( SCROLL - 1 )) ;;
        f) SCROLL=$(( SCROLL + vh )) ;;
        u) SCROLL=$(( SCROLL - vh )) ;;
        g) SCROLL=0 ;;
        G) SCROLL=$max_off ;;
        q|Q) break ;;
      esac
      export SORT SORT_REV HELP
    fi
  done
  printf '\033[?25h\033[J\n'
else
  snapshot
fi
