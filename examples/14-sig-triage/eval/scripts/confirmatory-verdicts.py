#!/usr/bin/env python3
"""Confirmatory verdicts for the six registered contrasts (T1, registration 96cc418).

Reuses paired-verdicts.py's scorer bridge (rows_for) and McNemar implementation
verbatim — this script only decides WHICH cells to pair and how to combine the
three registered replications. No independent scoring, no independent statistics.

Registered analysis plan (frozen at main 96cc418):
  - scorer of record: sigeval.go; verdicts: McNemar exact on discordant pairs
  - primary metric: accept-set accuracy on the full 250
  - 3 replications per arm-side; 5x bump if a contrast lands within 1 point of
    its decision boundary
  - Holm-Bonferroni across the six primaries at alpha=0.05

JUDGMENT CALL, flagged for the record: the registration fixed McNemar + Holm +
3x replication but did not state how the three replications combine into one
primary test. This script reports BOTH readings and uses POOLED for the
Holm-adjusted primary:
  - per-wave: McNemar on each (arm-cN vs base-cN) pairing, N=250 each
  - pooled:   the three pairings concatenated, N=750 paired observations
Pooled is the more conservative choice against wave-level drift (it cannot be
gamed by picking the best wave) and is the standard way replicated paired
designs are combined. Both are printed so the alternative reading is visible.

Usage (from examples/14-sig-triage):
  python3 eval/scripts/confirmatory-verdicts.py
  SIGEVAL=/path/to/sigeval python3 eval/scripts/confirmatory-verdicts.py
"""
import importlib.util
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
EX = os.path.normpath(os.path.join(HERE, "..", ".."))

_spec = importlib.util.spec_from_file_location(
    "paired_verdicts", os.path.join(HERE, "paired-verdicts.py"))
pv = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pv)

RIG = "4b"


def waves_for(arm, base):
    """Draws where BOTH sides of the contrast exist, in order.

    Discovered, not hardcoded: Amendment 1 DECISION 4 adds c4/c5 for the
    boundary-triggered contrast only, so a fixed (c1,c2,c3) silently
    discarded exactly the draws the bump was run to produce.
    """
    import re
    out = []
    for d in sorted(os.listdir(pv.RUNS)):
        m = re.fullmatch(rf"{RIG}-{re.escape(arm)}-(c\d+)", d)
        if m and os.path.isdir(os.path.join(pv.RUNS, f"{RIG}-{base}-{m.group(1)}")):
            out.append(m.group(1))
    return sorted(out, key=lambda w: int(w[1:]))

# The six registered contrasts, verbatim from preregistration.md.
CONTRASTS = [
    ("1", "A0", "B",  "hand-written rules (tool absent both sides)"),
    ("2", "G",  "E2", "retrieval payload: full entries vs bare labels"),
    ("3", "P2", "P1", "order: task before reference"),
    ("4", "T3", "T2", "decomposition depth: 3 turns vs 2"),
    ("5a", "S1", "T2", "context bounding: fresh-session stage split"),
    ("5b", "T2c", "T2", "context bounding: per-turn clear (distilled carrier)"),
    ("6", "T2cr", "T2c", "carrier vs clearing: raw notes vs distilled shortlist"),
]
# #5 is registered as one primary with two arms (S1 and T2c vs T2). For Holm it
# counts ONCE: the family is six primaries, not seven. Combined by taking the
# less significant of 5a/5b (conservative — the primary claim is that context
# bounding hurts, so both arms must clear).
HOLM_FAMILY = ["1", "2", "3", "4", "5", "6"]


def holm(pvals):
    """Holm-Bonferroni: returns {key: (adjusted_p, reject)} at alpha=0.05."""
    alpha = 0.05
    order = sorted(pvals.items(), key=lambda kv: kv[1])
    m = len(order)
    out, running = {}, 0.0
    for i, (k, p) in enumerate(order):
        adj = min(1.0, p * (m - i))
        running = max(running, adj)  # enforce monotonicity
        out[k] = (running, running < alpha)
    return out


def main():
    missing = []
    per_contrast = {}

    print(f"CONFIRMATORY VERDICTS — registration 96cc418, rig {RIG}")
    print("=" * 78)

    for cid, arm, base, variable in CONTRASTS:
        waves_out = []
        pooled_x, pooled_y = {}, {}
        for w in waves_for(arm, base):
            xt, yt = f"{RIG}-{arm}-{w}", f"{RIG}-{base}-{w}"
            if not (os.path.isdir(os.path.join(pv.RUNS, xt))
                    and os.path.isdir(os.path.join(pv.RUNS, yt))):
                missing.append(f"{xt} vs {yt}")
                continue
            a, b = pv.rows_for(xt), pv.rows_for(yt)
            if a is None or b is None:
                missing.append(f"{xt} vs {yt} (unscoreable)")
                continue
            ax = 100 * sum(a.values()) / len(a)
            ay = 100 * sum(b.values()) / len(b)
            n01, n10, p = pv.mcnemar_exact(a, b)
            waves_out.append((w, ax, ay, n01, n10, p))
            for k, v in a.items():
                pooled_x[f"{w}:{k}"] = v
            for k, v in b.items():
                pooled_y[f"{w}:{k}"] = v

        if not waves_out:
            print(f"\ncontrast {cid}  {arm} vs {base}: NO CELLS")
            continue

        pn01, pn10, pp = pv.mcnemar_exact(pooled_x, pooled_y)
        pax = 100 * sum(pooled_x.values()) / len(pooled_x)
        pay = 100 * sum(pooled_y.values()) / len(pooled_y)
        per_contrast[cid] = dict(arm=arm, base=base, variable=variable,
                                 waves=waves_out, pooled=(pax, pay, pn01, pn10, pp),
                                 n_waves=len(waves_out))

        print(f"\ncontrast {cid}  {arm} vs {base}  — {variable}")
        for w, ax, ay, n01, n10, p in waves_out:
            print(f"   {w}      {ax:5.1f} vs {ay:5.1f}   d={ax-ay:+5.1f}   "
                  f"disc {n01:3d}/{n10:3d}   p={p:.4g}")
        print(f"   POOLED  {pax:5.1f} vs {pay:5.1f}   d={pax-pay:+5.1f}   "
              f"disc {pn01:3d}/{pn10:3d}   p={pp:.4g}   (n={len(pooled_x)} paired)")

    # --- Holm across the six primaries ---------------------------------------
    fam = {}
    for cid in HOLM_FAMILY:
        if cid == "5":
            a5, b5 = per_contrast.get("5a"), per_contrast.get("5b")
            if not (a5 and b5):
                continue
            fam["5"] = max(a5["pooled"][4], b5["pooled"][4])  # conservative
        elif cid in per_contrast:
            fam[cid] = per_contrast[cid]["pooled"][4]

    print("\n" + "=" * 78)
    print("HOLM-BONFERRONI across the six registered primaries (alpha=0.05, pooled p)")
    adj = holm(fam)
    for cid in HOLM_FAMILY:
        if cid not in adj:
            print(f"   contrast {cid}: INCOMPLETE — cells missing")
            continue
        a, rej = adj[cid]
        raw = fam[cid]
        label = ("S1/T2c vs T2" if cid == "5"
                 else f"{per_contrast[cid]['arm']} vs {per_contrast[cid]['base']}")
        print(f"   contrast {cid}  {label:14s} raw p={raw:.4g}  "
              f"Holm-adj p={a:.4g}  {'RESOLVED' if rej else 'unresolved'}")

    # --- boundary trigger (signed: 5x if within 1 point of the boundary) ------
    print("\n" + "-" * 78)
    print("BOUNDARY TRIGGER CHECK (signed: bump to 5x if a contrast lands within")
    print("1 point of its decision boundary; measured null band = +-2.4 points)")
    for cid, d in sorted(per_contrast.items()):
        eff = d["pooled"][0] - d["pooled"][1]
        dist = abs(abs(eff) - 2.4)
        flag = "TRIGGER — 5x replication called for" if dist <= 1.0 else ""
        print(f"   contrast {cid}  {d['arm']:5s} vs {d['base']:4s}  "
              f"pooled d={eff:+5.1f}  |d|-band={dist:4.1f}  {flag}")

    if missing:
        print("\n" + "-" * 78)
        print("MISSING PAIRINGS (contrast strength reduced accordingly):")
        for m in missing:
            print(f"   {m}")

    print("\nNOTE: pooled p is the primary per this script's stated judgment call;")
    print("per-wave p is printed so the alternative combination is auditable.")


if __name__ == "__main__":
    main()
