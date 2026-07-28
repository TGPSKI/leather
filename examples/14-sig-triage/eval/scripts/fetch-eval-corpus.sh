#!/usr/bin/env bash
# fetch-eval-corpus.sh — build a labeled eval set from real GitHub issues.
#
# Pulls sig-labeled kubernetes/kubernetes issues via the GitHub search API (one
# request per SIG for balance), caches the raw responses, then SPLITS the data:
#   corpus.jsonl  — {number, repo, title, body}   (NO labels; what the model sees)
#   gold.jsonl    — {number, sig, accept[]}        (labels only; the answer key)
# An issue carrying multiple sig/* labels becomes an `accept` set (natural
# multi-label ambiguity), gold = its first sig label.
#
#   PER_SIG=10 bash eval/scripts/fetch-eval-corpus.sh
#   GH_TOKEN=... bash eval/scripts/fetch-eval-corpus.sh   # authenticated = higher rate limit
set -euo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$EVAL_DIR"
CACHE="cache"; mkdir -p "$CACHE"
REPO="${REPO:-kubernetes/kubernetes}"
PER_SIG="${PER_SIG:-10}"
BODY_CAP="${BODY_CAP:-4000}"
TARGET="${TARGET:-100}"
SLEEP="${SLEEP:-7}"   # stay under the unauth search rate limit (~10/min)
# Cache-file suffix. Every search-sig-*.json in cache/ is merged, so fetching a
# wider pull under a new suffix ADDS to the corpus instead of replacing it --
# previously-scored issues stay in, which keeps old runs comparable. Bump the
# suffix (not REFRESH=1) when growing the corpus.
SUFFIX="${SUFFIX:-}"

# balanced across common + long-tail SIGs; trimmed to $TARGET after dedup
SIGS=(network node storage scheduling apps api-machinery auth cli autoscaling instrumentation)

auth=(); [ -n "${GH_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GH_TOKEN")

for sig in "${SIGS[@]}"; do
  out="$CACHE/search-sig-${sig}${SUFFIX}.json"
  if [ -s "$out" ] && [ "${REFRESH:-0}" != 1 ]; then
    echo "cached: sig/$sig" >&2; continue
  fi
  echo "fetch: sig/$sig" >&2
  q="repo:$REPO+is:issue+label:sig/$sig"
  curl -fsS "${auth[@]}" -H "Accept: application/vnd.github+json" \
    "https://api.github.com/search/issues?q=$q&per_page=$PER_SIG&sort=created&order=desc" \
    > "$out" || { echo "  fetch failed for sig/$sig (rate limit?)" >&2; rm -f "$out"; }
  sleep "$SLEEP"
done

# merge cache -> dedup by number -> SCRUB label leakage -> split corpus + gold
python3 - "$CACHE" "$BODY_CAP" "$TARGET" "$REPO" <<'PY'
import json, glob, sys, os, re
cache, cap, target, repo = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]

_SIGNAMES = ("network|node|storage|scheduling|apps|api-machinery|auth|cli|autoscaling|"
             "instrumentation|windows|multicluster|release|testing|cloud-provider|"
             "security|docs|architecture|scalability|contributor-experience")
_SCRUB = [
    re.compile(r"/(remove-)?sig\s+[a-z-]+", re.I),   # prow /sig, /remove-sig commands
    re.compile(r"\bsig[-/][a-z-]+", re.I),           # sig/network, sig-network mentions
    re.compile(r"\bSIG\s+(" + _SIGNAMES + r")\b", re.I),  # "SIG Network" prose
]
def scrub(text):
    for rx in _SCRUB:
        text = rx.sub("[sig-redacted]", text)
    return text
seen = {}
for f in sorted(glob.glob(os.path.join(cache, "search-sig-*.json"))):
    try: d = json.load(open(f))
    except Exception: continue
    for it in d.get("items", []):
        if "pull_request" in it:            # search is:issue already excludes, belt+braces
            continue
        n = it["number"]
        sigs = sorted({l["name"][4:] for l in it["labels"] if l["name"].startswith("sig/")})
        if not sigs:
            continue
        if n not in seen:
            seen[n] = {
                "number": n, "repo": repo,
                "title": scrub(it.get("title") or ""),
                "body": scrub((it.get("body") or "")[:cap]),
                "sigs": sigs,
            }
# Trim to target by ROUND-ROBIN over primary SIG, not by taking the first N
# issue numbers. A plain `[:target]` cut keeps whichever SIGs happen to own the
# lowest-numbered issues and can starve a class below the support its recall
# gate needs; round-robin gives every SIG an equal share of the budget and only
# spills the surplus of over-represented ones.
#
# Multi-SIG issues are the ambiguous tail and the honest part of the
# distribution (they become accept-sets), so they are retained at their NATURAL
# RATE rather than either dropped or preferred. Drawing them first would double
# the ambiguity rate and make the corpus harder than the population it claims to
# sample; dropping them would make it easier. Within each SIG the two strata are
# interleaved in proportion to that SIG's own multi-label rate, most recent
# first, so any prefix of the budget preserves the mix.
by_sig = {}
for r in seen.values():
    by_sig.setdefault(r["sigs"][0], []).append(r)
for s, rows_s in by_sig.items():
    multi = sorted((r for r in rows_s if len(r["sigs"]) > 1),
                   key=lambda r: -r["number"])
    single = sorted((r for r in rows_s if len(r["sigs"]) == 1),
                    key=lambda r: -r["number"])
    rate = len(multi) / len(rows_s) if rows_s else 0
    merged, mi, si = [], 0, 0
    while mi < len(multi) or si < len(single):
        want_multi = (mi + si) * rate >= mi  # keep the running share near `rate`
        if want_multi and mi < len(multi):
            merged.append(multi[mi]); mi += 1
        elif si < len(single):
            merged.append(single[si]); si += 1
        elif mi < len(multi):
            merged.append(multi[mi]); mi += 1
    by_sig[s] = merged

picked, sigs_cycle = [], sorted(by_sig)
while len(picked) < target and any(by_sig[s] for s in sigs_cycle):
    for s in sigs_cycle:
        if not by_sig[s] or len(picked) >= target:
            continue
        picked.append(by_sig[s].pop(0))
rows = sorted(picked, key=lambda r: r["number"])

with open("corpus.jsonl", "w") as c, open("gold.jsonl", "w") as g:
    for r in rows:
        c.write(json.dumps({"number": r["number"], "repo": r["repo"],
                            "title": r["title"], "body": r["body"]}) + "\n")
        sigs = ["sig-"+s for s in r["sigs"]]
        gold = {"number": r["number"], "sig": sigs[0]}
        if len(sigs) > 1:
            gold["accept"] = sigs      # multi-sig issue -> any of its labels is acceptable
        g.write(json.dumps(gold) + "\n")

# distribution report
from collections import Counter
dist = Counter(r["sigs"][0] for r in rows)
multi = sum(1 for r in rows if len(r["sigs"]) > 1)
print(f"corpus: {len(rows)} issues (target {target})")
print(f"multi-SIG (ambiguous, accept-set): {multi}")
print("gold distribution (primary SIG):")
for s, c in dist.most_common():
    print(f"  sig-{s:20s} {c}")
PY
echo >&2
echo "wrote corpus.jsonl (blind) + gold.jsonl (answer key); raw in $CACHE/" >&2
