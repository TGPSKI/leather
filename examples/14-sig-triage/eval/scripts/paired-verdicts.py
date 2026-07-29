#!/usr/bin/env python3
"""Paired per-issue verdicts for every declared arm comparison (v2 protocol, task #33).

Why paired: every cell runs the same 250-issue corpus, so a cross-arm delta does not
have to wait on the run-to-run noise floor — McNemar's exact test on the DISCORDANT
issues (arm X right where Y is wrong, and vice versa) uses within-issue agreement and
resolves at far tighter bounds than comparing two marginal accuracies against a null
band. The noise floor (JOB #1) still matters for replication budgeting; this is the
inference you can do without it.

Scoring: consumes <archive>/sigeval-rows.jsonl — sigeval's per-row verdicts
(-emit-rows), generated here on first touch along with sigeval-report.txt. sigeval is
the only scorer (task #32); this script never derives correctness itself.

Isolation: before printing a delta, diffs the two run manifests. A comparison whose
manifests differ in model, endpoint, corpus, or analyze cache is measuring more than
its declared variable — the delta is printed but flagged CONFOUND, per the rule:
state the number, its comparator, and what is held constant; withhold the causal
claim otherwise. (This check, run by hand on the 0728 archives, caught the A/A-2
repeat pair holding different analyze caches and the C->P1->P2->D chain swapping
caches at every link.)

Verdict vocabulary, per the §1 rule: RESOLVED (exact p < alpha) or unresolved.
Never a story.

Usage (from examples/14-sig-triage):
  python3 eval/scripts/paired-verdicts.py            # all rigs, all declared pairs
  python3 eval/scripts/paired-verdicts.py --rig 35b
  SIGEVAL=/path/to/sigeval python3 eval/scripts/paired-verdicts.py   # skip `go run`
"""
import argparse, json, math, os, subprocess, sys

EX = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
ROOT = os.path.normpath(os.path.join(EX, "..", ".."))
RUNS = os.path.join(EX, "eval", "results", "runs")
SIGEVAL = os.environ.get("SIGEVAL")

# Manifest fields that make two cells a different EXPERIMENT if they differ.
# force_tool and index/index_sha are excluded: they are how several arms (D, H,
# F2, Eauto; E2, G, G2) EXPRESS their declared variable. They are still printed.
CONFOUND_KEYS = ("model", "endpoint", "corpus_sha", "analyze_cache_sha", "concurrency", "logprob")
SHOWN_KEYS = CONFOUND_KEYS + ("force_tool", "index", "index_sha", "analyze_cache", "git_commit")


def rows_for(tag):
    d = os.path.join(RUNS, tag)
    pred = os.path.join(d, "predictions.jsonl")
    if not os.path.exists(pred):
        return None
    rp = os.path.join(d, "sigeval-rows.jsonl")
    # Stale if older than the predictions OR the answer key: an adjudicated
    # override must rescore every cached row file, not just future ones.
    key_mtime = max(os.path.getmtime(pred),
                    os.path.getmtime(os.path.join(EX, "eval", "gold.jsonl")),
                    os.path.getmtime(os.path.join(EX, "eval", "gold.overrides.jsonl")))
    if not os.path.exists(rp) or os.path.getmtime(rp) < key_mtime:
        cmd = [SIGEVAL] if SIGEVAL else ["go", "run", "./examples/14-sig-triage/eval/sigeval.go"]
        cmd += ["-pred", os.path.abspath(pred),
                "-gold", os.path.join(EX, "eval", "gold.jsonl"),
                "-overrides", os.path.join(EX, "eval", "gold.overrides.jsonl"),
                "-split", os.path.join(EX, "eval", "splits.jsonl"),
                "-catalog", os.path.join(EX, "sigs.reference.yaml"),
                "-emit-rows", os.path.abspath(rp)]
        rep = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, timeout=300)
        # Fail CLOSED: a failed scorer must never leave stale rows to be trusted
        # or overwrite a good report with empty stdout. sigeval exits 1 on a red
        # gate but still emits rows; treat "no fresh rows" as the failure signal.
        if not os.path.exists(rp) or os.path.getmtime(rp) < key_mtime:
            sys.exit(f"sigeval failed for {tag} (rc={rep.returncode}): "
                     f"{rep.stderr.strip()[:300]}")
        open(os.path.join(d, "sigeval-report.txt"), "w").write(rep.stdout)
    return {r["number"]: r["correct"] for r in map(json.loads, open(rp)) if r}


def manifest(tag):
    p = os.path.join(RUNS, tag, "run-manifest.json")
    try:
        return json.load(open(p))
    except Exception:
        return {}


def mcnemar_exact(a, b):
    """Two-sided exact binomial on discordant pairs. Returns (n01, n10, p)."""
    common = a.keys() & b.keys()
    n01 = sum(1 for n in common if a[n] and not b[n])
    n10 = sum(1 for n in common if b[n] and not a[n])
    n = n01 + n10
    if n == 0:
        return n01, n10, 1.0
    k = min(n01, n10)
    p = sum(math.comb(n, i) for i in range(k + 1)) * 2 / 2 ** n
    return n01, n10, min(1.0, p)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--rig", default=None, help="limit to one rig prefix (35b, 4b)")
    ap.add_argument("--alpha", type=float, default=0.05)
    args = ap.parse_args()

    arms = {k: v for k, v in
            json.load(open(os.path.join(EX, "eval", "ablation", "arms.json"))).items()
            if not k.startswith("_")}
    tags = sorted(os.listdir(RUNS)) if os.path.isdir(RUNS) else []
    rigs = sorted({t.split("-", 1)[0] for t in tags})
    if args.rig:
        rigs = [r for r in rigs if r == args.rig]

    for rig in rigs:
        print(f"\n== {rig} ==")
        # Repeat pairs first: <arm> vs <arm>-2 is the empirical draw-noise line.
        for arm in sorted(arms):
            x, y = f"{rig}-{arm}-2", f"{rig}-{arm}"
            if os.path.isdir(os.path.join(RUNS, x)) and os.path.isdir(os.path.join(RUNS, y)):
                emit(x, y, "REPEAT — config held; discordance here IS the draw noise",
                     args.alpha, repeat=True)
        for arm in sorted(arms):
            base = arms[arm].get("compare_to")
            if not base:
                continue
            x, y = f"{rig}-{arm}", f"{rig}-{base}"
            if os.path.isdir(os.path.join(RUNS, x)) and os.path.isdir(os.path.join(RUNS, y)):
                emit(x, y, arms[arm].get("variable", ""), args.alpha,
                     allow=frozenset(arms[arm].get("allow_diff") or ()))


def emit(x, y, variable, alpha, repeat=False, allow=frozenset()):
    # allow: manifest keys the arm's declared variable legitimately changes
    # (arms.json `allow_diff`); excluded from the confound check, still printed.
    a, b = rows_for(x), rows_for(y)
    if a is None or b is None:
        return
    ax = 100 * sum(a.values()) / len(a)
    ay = 100 * sum(b.values()) / len(b)
    n01, n10, p = mcnemar_exact(a, b)
    mx, my = manifest(x), manifest(y)
    # A missing manifest must not make the confound check vacuously pass
    # (None == None for every key would print RESOLVED on unreadable
    # provenance — the exact inversion of "withhold the causal claim").
    if not mx or not my:
        missing = [t for t, m in ((x, mx), (y, my)) if not m]
        print(f"{x:11s} vs {y:11s} {'':21s} NO-MANIFEST  provenance unreadable: {', '.join(missing)}")
        return
    diffs = [(k, mx.get(k), my.get(k)) for k in SHOWN_KEYS if mx.get(k) != my.get(k)]
    confounds = [k for k, _, _ in diffs if k in CONFOUND_KEYS and k not in allow]

    if repeat:
        # A "-2" tag is only a repeat if the prompt is byte-identical; a run
        # relaunched with an edited agent file is a wording comparison.
        verdict = "NOISE" if mx.get("agent_sha") == my.get("agent_sha") else "PROMPT-DIFF"
    elif confounds:
        verdict = "CONFOUND"
    elif p < alpha:
        verdict = "RESOLVED"
    else:
        verdict = "unresolved"
    print(f"{x:11s} vs {y:11s} {ax:5.1f} vs {ay:5.1f}  d={ax - ay:+5.1f}  "
          f"disc {n01:2d}/{n10:2d}  p={p:.4f}  {verdict:10s} {variable}")
    for k, vx, vy in diffs:
        marker = ("≈" if k in allow else
                  "!!" if k in CONFOUND_KEYS else ("~" if k == "git_commit" else " "))
        print(f"    {marker:2s} {k}: {vx!r} vs {vy!r}")


if __name__ == "__main__":
    main()
