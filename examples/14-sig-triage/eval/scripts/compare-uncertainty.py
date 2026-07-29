#!/usr/bin/env python3
"""compare-uncertainty.py — which uncertainty signal actually separates right from wrong?

Stage 3's confidence router is only worth building if SOMETHING predicts
correctness well enough to route on. Two candidates, both measured on the same
run so the comparison is apples to apples:

  verbalized  the model's own `CONFIDENCE: high|medium|low` -- a prompted
              self-report. Cheap, but the cascade-routing literature is
              consistent that self-reports are poorly calibrated.
  sig_margin  logprob(chosen) - logprob(best alternative) at the token that
              decides WHICH SIG. Read off the same forward pass, no extra cost.

Reported per signal:
  AUROC       P(a random correct row scores more confident than a random wrong
              one). 0.5 = the signal is noise; 1.0 = perfect separation. This is
              threshold-free, so it does not flatter a signal by cherry-picking
              a cutoff.
  coverage/risk at the escalation rates a router would actually use: if you
              escalate the least-confident X%, what is the error rate in the
              remaining (auto-accepted) rows? That is the number that decides
              whether escalation buys anything.

  bash eval/scripts/compare-uncertainty.py            # defaults to eval/*.jsonl
"""
import json, os, sys

EVAL = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
pred_p = sys.argv[1] if len(sys.argv) > 1 else os.path.join(EVAL, "predictions.jsonl")
gold_p = sys.argv[2] if len(sys.argv) > 2 else os.path.join(EVAL, "gold.jsonl")
ovr_p = sys.argv[3] if len(sys.argv) > 3 else os.path.join(EVAL, "gold.overrides.jsonl")


def load(p):
    if not os.path.exists(p):
        return []
    return [json.loads(l) for l in open(p) if l.strip()]


def norm(s):
    s = (s or "").strip().lower()
    return "sig-" + s[4:] if s.startswith("sig/") else s


gold = {g["number"]: g for g in load(gold_p)}
for o in load(ovr_p):
    if o["number"] in gold:
        g = gold[o["number"]]
        if o.get("sig") is not None:
            g["sig"] = o["sig"]
        if o.get("accept") is not None:
            g["accept"] = o["accept"]

rows = []
for p in load(pred_p):
    g = gold.get(p["number"])
    if not g:
        continue
    ok = norm(p.get("predicted")) in {norm(g["sig"])} | {norm(a) for a in g.get("accept", [])}
    rows.append({
        "n": p["number"], "ok": ok,
        "verbalized": (p.get("confidence") or "").lower(),
        "sig_margin": p.get("sig_margin"),
        "commit_margin": p.get("commit_margin"),
    })

if not rows:
    sys.exit("no scored rows -- did the run write predictions.jsonl?")

VERB_RANK = {"low": 0, "medium": 1, "high": 2}


def auroc(scored):
    """Mann-Whitney AUROC with ties counted as half, over (score, correct) pairs."""
    pos = [s for s, ok in scored if ok]
    neg = [s for s, ok in scored if not ok]
    if not pos or not neg:
        return None, len(pos), len(neg)
    wins = sum((1.0 if a > b else 0.5 if a == b else 0.0) for a in pos for b in neg)
    return wins / (len(pos) * len(neg)), len(pos), len(neg)


def risk_coverage(scored, rates=(0.10, 0.20, 0.30)):
    """Escalate the least-confident X%; report the error rate left behind."""
    out = []
    s = sorted(scored, key=lambda r: r[0])
    for rate in rates:
        k = int(round(len(s) * rate))
        kept = s[k:]
        if not kept:
            out.append((rate, None, 0))
            continue
        err = sum(1 for _, ok in kept if not ok) / len(kept)
        out.append((rate, err, len(kept)))
    return out


base_err = sum(1 for r in rows if not r["ok"]) / len(rows)
print(f"rows={len(rows)}  baseline error={100*base_err:.1f}%  "
      f"({sum(1 for r in rows if not r['ok'])} wrong)\n")

signals = {
    "verbalized (self-report)": [(VERB_RANK.get(r["verbalized"], -1), r["ok"]) for r in rows],
    "sig_margin (logprob)": [(r["sig_margin"], r["ok"]) for r in rows if r["sig_margin"] is not None],
    "commit_margin (logprob)": [(r["commit_margin"], r["ok"]) for r in rows if r["commit_margin"] is not None],
}

print(f"{'signal':<28}{'n':>6}{'AUROC':>8}   {'error @ escalate 10/20/30%':<32}")
print("-" * 78)
for name, scored in signals.items():
    if not scored:
        print(f"{name:<28}{0:>6}{'   n/a':>8}   (not captured)")
        continue
    a, npos, nneg = auroc(scored)
    astr = "  n/a" if a is None else f"{a:.3f}"
    rc = risk_coverage(scored)
    rcs = "  ".join("  n/a" if e is None else f"{100*e:4.1f}%" for _, e, _ in rc)
    print(f"{name:<28}{len(scored):>6}{astr:>8}   {rcs:<32}")

print()
# Distribution matters as much as AUROC: a signal that emits one value cannot
# route anything, however well it correlates in principle.
vb = {}
for r in rows:
    v = r["verbalized"] or "(none)"
    b = vb.setdefault(v, [0, 0])
    b[0] += 1
    b[1] += 1 if r["ok"] else 0
print("verbalized buckets:")
for v, (n, ok) in sorted(vb.items(), key=lambda kv: -kv[1][0]):
    print(f"  {v:<10} n={n:<5} acc={100*ok/n:5.1f}%  share={100*n/len(rows):4.1f}%")
if len(vb) == 1:
    print("  -> DEGENERATE: one bucket, nothing to route on.")

ms = [r["sig_margin"] for r in rows if r["sig_margin"] is not None]
if ms:
    ms_sorted = sorted(ms)
    q = lambda f: ms_sorted[min(len(ms_sorted) - 1, int(f * len(ms_sorted)))]
    print(f"\nsig_margin distribution: min={min(ms):.2f} p10={q(.10):.2f} "
          f"p25={q(.25):.2f} median={q(.50):.2f} p75={q(.75):.2f} max={max(ms):.2f}")
    okm = [r["sig_margin"] for r in rows if r["ok"] and r["sig_margin"] is not None]
    bad = [r["sig_margin"] for r in rows if not r["ok"] and r["sig_margin"] is not None]
    if okm and bad:
        print(f"  mean margin  correct={sum(okm)/len(okm):.2f}   wrong={sum(bad)/len(bad):.2f}")
