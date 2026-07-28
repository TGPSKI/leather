#!/usr/bin/env bash
# make-splits.sh — build the committed tier manifest (eval/splits.jsonl).
#
# Three tiers, disjoint, stratified per primary SIG so each tier carries the same
# class mix as the corpus (a tier that accidentally starved a class would make
# its per-class numbers unreadable):
#
#   smoke       ~20%  the fast iteration slice. THE ONLY TIER TUNING MAY LOOK AT.
#   acceptance  ~60%  the rest of the gate of record. Not tuned on.
#   holdout     ~20%  never tuned on and never gated on -- the generalization
#                     check that says whether a rule generalized or memorized.
#
# Assignment is deterministic (round-robin over each SIG's issues in number
# order), so the manifest is stable and auditable across re-fetches instead of
# depending on a random seed. Membership is committed for the same reason.
#
#   bash eval/scripts/make-splits.sh
set -euo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$EVAL_DIR"

python3 - "${CORPUS:-corpus.jsonl}" "${GOLD:-gold.jsonl}" "${OUT:-splits.jsonl}" <<'PY'
import json, sys
from collections import Counter, defaultdict

corpus_p, gold_p, out_p = sys.argv[1], sys.argv[2], sys.argv[3]
gold = {json.loads(l)["number"]: json.loads(l) for l in open(gold_p) if l.strip()}
nums = [json.loads(l)["number"] for l in open(corpus_p) if l.strip()]

# Cycle of 5 -> 1 smoke, 3 acceptance, 1 holdout = 20/60/20, applied within each
# SIG so the ratio holds per class and not just in aggregate.
CYCLE = ["smoke", "acceptance", "acceptance", "acceptance", "holdout"]

by_sig = defaultdict(list)
for n in nums:
    by_sig[gold.get(n, {}).get("sig", "unknown")].append(n)

tier_of = {}
for sig in sorted(by_sig):
    for i, n in enumerate(sorted(by_sig[sig])):
        tier_of[n] = CYCLE[i % len(CYCLE)]

with open(out_p, "w") as f:
    for n in nums:
        f.write(json.dumps({"number": n, "tier": tier_of[n]}) + "\n")

counts = Counter(tier_of.values())
print(f"wrote {out_p}: {len(nums)} rows -> " +
      ", ".join(f"{t} {counts[t]}" for t in ("smoke", "acceptance", "holdout")))
print("\nper-SIG tier balance (primary gold SIG):")
print(f"  {'SIG':<24}{'smoke':>7}{'accept':>8}{'holdout':>9}{'total':>7}")
for sig in sorted(by_sig, key=lambda s: -len(by_sig[s])):
    c = Counter(tier_of[n] for n in by_sig[sig])
    print(f"  {sig:<24}{c['smoke']:>7}{c['acceptance']:>8}{c['holdout']:>9}{len(by_sig[sig]):>7}")
PY
