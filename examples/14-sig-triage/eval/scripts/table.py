#!/usr/bin/env python3
"""Render the results table from ARCHIVES, not from runner logs.

The log-driven version read $TMP/<prefix>-<rig>.log for a fixed set of prefixes, so any
battery written to a different filename was invisible — T3, G2 and F2 all completed and
none appeared. Logs are also mutable, orphaned and duplicated across re-runs. The archive
under results/runs/<tag>/ is the source of truth everywhere else in this project; this makes
the display agree.
"""
import gzip, json, os, sys, glob

EX = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
RUNS = os.path.join(EX, "eval", "results", "runs")
NC = os.environ.get("NOCOLOR")
B, D, R = ("", "", "") if NC else ("\033[1m", "\033[2m", "\033[0m")
GRN, YEL, RED, CYN = ("", "", "", "") if NC else ("\033[32m", "\033[33m", "\033[31m", "\033[36m")

# Scoring: sigeval is the ONLY scorer (task #32). This script previously carried its own
# accept-set membership test, which silently disagreed with sigeval wherever a model emitted
# slash-notation labels (35b-F: 81.6 naive vs 84.4 sigeval — 7 `sig/apps` rows normSIG folds
# and a raw string compare does not). Two scorers, two numbers; the table showed the wrong one.
# Now: read <archive>/sigeval-rows.jsonl, generating it (plus sigeval-report.txt) on first
# touch, so the table can never disagree with the scorer of record.
import subprocess

ROOT = os.path.normpath(os.path.join(EX, "..", ".."))
SIGEVAL = os.environ.get("SIGEVAL")  # optional prebuilt binary; else `go run`

def score(d, pred_path, rows):
    rp = os.path.join(d, "sigeval-rows.jsonl")
    key_mtime = max(os.path.getmtime(pred_path),
                    os.path.getmtime(os.path.join(EX, "eval", "gold.jsonl")),
                    os.path.getmtime(os.path.join(EX, "eval", "gold.overrides.jsonl")))
    if not os.path.exists(rp) or os.path.getmtime(rp) < key_mtime:
        cmd = [SIGEVAL] if SIGEVAL else ["go", "run", "./examples/14-sig-triage/eval/sigeval.go"]
        cmd += ["-pred", os.path.abspath(pred_path),
                "-gold", os.path.join(EX, "eval", "gold.jsonl"),
                "-overrides", os.path.join(EX, "eval", "gold.overrides.jsonl"),
                "-split", os.path.join(EX, "eval", "splits.jsonl"),
                "-catalog", os.path.join(EX, "sigs.reference.yaml"),
                "-emit-rows", os.path.abspath(rp)]
        try:
            rep = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, timeout=300)
        except Exception as e:
            print(f"  !! sigeval failed for {d}: {e} — refusing to fall back to a second scorer",
                  file=sys.stderr)
            return None
        # Fail CLOSED: if no FRESH rows exist after the attempt (scorer crashed,
        # emitted nothing), never fall back to stale rows or clobber the report.
        if not os.path.exists(rp) or os.path.getmtime(rp) < key_mtime:
            print(f"  !! sigeval produced no fresh rows for {d} (rc={rep.returncode}): "
                  f"{rep.stderr.strip()[:200]}", file=sys.stderr)
            return None
        open(os.path.join(d, "sigeval-report.txt"), "w").write(rep.stdout)
    verd = [json.loads(l) for l in open(rp) if l.strip()]
    if not verd: return None
    return 100 * sum(1 for v in verd if v["correct"]) / len(verd)
try:
    arms = {k: v for k, v in json.load(open(os.path.join(EX, "eval", "ablation", "arms.json"))).items()
            if not k.startswith("_")}
except Exception:
    arms = {}

def cell(d):
    tag = os.path.basename(d.rstrip("/"))
    p = os.path.join(d, "predictions.jsonl")
    if not os.path.exists(p): return None
    rows = [json.loads(l) for l in open(p) if l.strip()]
    if not rows: return None
    dead = sum(1 for r in rows if r.get("predicted") == "unknown" and r.get("confidence") == "no-output")
    acc = score(d, p, rows)
    if acc is None: return None
    man = {}
    mp = os.path.join(d, "run-manifest.json")
    if os.path.exists(mp):
        try: man = json.load(open(mp))
        except Exception: pass
    # Telemetry from the archive, not from runner-log text. Tool EXECUTIONS come from
    # leather's own log (ground truth); calls/issue counts every LLM round the proxy saw,
    # across all stages, which is the honest cost figure for multi-stage arms.
    calls = issues = tools = toks = 0
    lp = os.path.join(d, "logprobs.jsonl.gz")
    if os.path.exists(lp):
        seen = set()
        for l in gzip.open(lp, "rt", errors="replace"):
            l = l.strip()
            if not l: continue
            try: rec = json.loads(l)
            except Exception: continue
            calls += 1
            if rec.get("issue") is not None: seen.add(rec["issue"])
            u = rec.get("usage") or {}
            toks += u.get("total_tokens") or 0
        issues = len(seen)
    ev = os.path.join(d, "run-evidence.log.gz")
    if os.path.exists(ev):
        with gzip.open(ev, "rt", errors="replace") as f:
            tools = sum(1 for line in f if "executing tool" in line)
    rig = "35b" if tag.startswith("35b") else ("4b" if tag.startswith("4b") else "?")
    arm = tag[len(rig)+1:] if rig != "?" else tag
    arm = arm.rsplit("-", 1)[0] if arm.rsplit("-", 1)[-1].isdigit() else arm
    return dict(tag=tag, rig=rig, arm=arm, acc=acc, rows=len(rows), dead=dead,
                cpi=(calls / len(rows)) if rows else 0.0, tools=tools,
                ktok=toks / 1000.0,
                var=arms.get(arm, {}).get("variable", ""), started=man.get("started", ""))

cells = [c for c in (cell(d) for d in sorted(glob.glob(os.path.join(RUNS, "*/")))) if c]
# a re-run supersedes an earlier archive of the same tag
best = {}
for c in cells: best[c["tag"]] = c
ONLY = os.environ.get("RIG")
for rig in (("35b", "4b") if not ONLY else (ONLY,)):
    rc = sorted([c for c in best.values() if c["rig"] == rig], key=lambda c: -c["acc"])
    if not rc: continue
    if not ONLY: print(f"\n  {B}{rig}{R}  {D}{len(rc)} cells{R}")
    print(f"     {D}{'cell':11s} {'acc':>6s} {'no-out':>6s} {'calls/iss':>9s} {'tools':>6s} {'ktok':>6s}  {'variable under test':38s}{R}")
    print(f"     {D}{'':11s} {'':>6s} {'':>6s} {'':>9s} {'':>6s} {'? = not':>6s}  {'captured (archive predates usage log)':38s}{R}")
    for c in rc:
        col = GRN if c["acc"] >= 84 else (YEL if c["acc"] >= 74 else RED)
        lost = f"{RED}{c['dead']:6d}{R}" if c["dead"] else f"{D}     -{R}"
        tc = CYN if c["tools"] else D
        ktok = f"{c['ktok']:6.0f}" if c["ktok"] else "     ?"
        print(f"     {c['tag']:11s} {col}{c['acc']:6.1f}{R} {lost} "
              f"{D}{c['cpi']:9.2f}{R} {tc}{c['tools']:6d}{R} {D}{ktok}{R}  "
              f"{D}{c['var'][:38]:38s}{R}")
