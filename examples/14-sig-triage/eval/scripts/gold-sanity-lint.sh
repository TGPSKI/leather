#!/usr/bin/env bash
# gold-sanity-lint.sh — the mirror image of the leakage guard (LEP-0007 §5.3).
#
# The leakage lint asks "is the answer in the question?". This one asks the
# opposite: "does the question contain enough to answer at all?". An input with
# no recoverable signal MUST be gold `unknown` (or accept `unknown`) — otherwise
# a well-calibrated abstention scores as a miss and the junk drags a core SIG's
# recall denominator below what perfect classification could reach.
#
# The rule is a DECLARED GENERAL PREDICATE (a body-length floor), never a
# hand-maintained list of issue numbers. Relabels land in gold.overrides.jsonl —
# a diffable, reasoned overlay — so gold.jsonl stays the pristine fetch output
# and a corpus re-fetch can never clobber or silently drift from them.
#
#   bash eval/scripts/gold-sanity-lint.sh            # check; fail closed on a violation
#   WRITE=1 bash eval/scripts/gold-sanity-lint.sh    # regenerate gold.overrides.jsonl
#   MIN_BODY_CHARS=60 ...                            # tune the predicate
#
# Exit 0 = clean, 1 = a content-free input still carries a concrete gold label.
set -euo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$EVAL_DIR"

CORPUS="${CORPUS:-corpus.jsonl}"
GOLD="${GOLD:-gold.jsonl}"
OVERRIDES="${OVERRIDES:-gold.overrides.jsonl}"
MIN_BODY_CHARS="${MIN_BODY_CHARS:-60}"

python3 - "$CORPUS" "$GOLD" "$OVERRIDES" "$MIN_BODY_CHARS" "${WRITE:-0}" <<'PY'
import json, sys, os

corpus_p, gold_p, ovr_p, min_chars, write = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]), sys.argv[5] == "1"

def load(p):
    if not os.path.exists(p):
        return []
    with open(p) as f:
        return [json.loads(l) for l in f if l.strip()]

corpus = {r["number"]: r for r in load(corpus_p)}
gold   = {r["number"]: r for r in load(gold_p)}
existing = {r["number"]: r for r in load(ovr_p)}

# The predicate: an issue whose body carries fewer than MIN_BODY_CHARS characters
# has no recoverable technical signal. Abstention is the correct answer for it.
def content_free(row):
    return len((row.get("body") or "").strip()) < min_chars

flagged = [n for n, r in sorted(corpus.items()) if content_free(r)]

if write:
    out = []
    for n in flagged:
        g = gold.get(n, {})
        body = (corpus[n].get("body") or "").strip()
        o = {"number": n, "sig": "unknown"}
        # Keep the raw accept-set: the issue's real labels stay acceptable, so a
        # model that guesses one of them is not punished either. Only the demand
        # for a *specific* concrete answer is lifted.
        if g.get("accept"):
            o["accept"] = g["accept"]
        elif g.get("sig") and g["sig"] != "unknown":
            o["accept"] = [g["sig"]]
        o["reason"] = (f"gold-sanity: body {len(body)} chars < {min_chars} "
                       f"(content-free) -> abstention is the correct answer")
        out.append(o)
    # Preserve any non-sanity override a human added by hand, keyed by number.
    for n, o in sorted(existing.items()):
        if n not in flagged:
            out.append(o)
    out.sort(key=lambda o: o["number"])
    with open(ovr_p, "w") as f:
        for o in out:
            f.write(json.dumps(o) + "\n")
    print(f"wrote {ovr_p}: {len(out)} override(s) "
          f"({len(flagged)} by the <{min_chars}-char content-free rule)")
    sys.exit(0)

# check mode: gold-after-overrides must permit abstention on every flagged row
violations = []
for n in flagged:
    g = dict(gold.get(n, {}))
    g.update({k: v for k, v in existing.get(n, {}).items() if k != "reason"})
    ok = g.get("sig") == "unknown" or "unknown" in (g.get("accept") or [])
    if not ok:
        violations.append((n, g.get("sig")))

print(f"gold-sanity: {len(corpus)} rows, predicate body < {min_chars} chars, "
      f"{len(flagged)} content-free, {len(existing)} override(s) on file")
if violations:
    for n, s in violations:
        print(f"  VIOLATION #{n}: content-free but gold demands '{s}' "
              f"(no abstention allowed)")
    print("\nGOLD-SANITY FAILED — run WRITE=1 to regenerate the overrides overlay")
    sys.exit(1)
print("GOLD-SANITY PASSED")
PY
