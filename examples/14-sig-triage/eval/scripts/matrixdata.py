#!/usr/bin/env python3
"""Shared archive loader for the results table and the matrix TUI.

Extracted from table.py so the two surfaces cannot disagree: one scorer
bridge (sigeval, per task #32), one telemetry reader, one filter grammar.
Cells come from ARCHIVES under results/runs/<tag>/, never from runner logs.

Stdlib only.
"""
from __future__ import annotations

import fnmatch
import glob
import gzip
import json
import os
import re
import subprocess
import sys

EX = os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
ROOT = os.path.normpath(os.path.join(EX, "..", ".."))
RUNS = os.path.join(EX, "eval", "results", "runs")
SIGEVAL = os.environ.get("SIGEVAL")  # optional prebuilt binary; else `go run`

# tag -> (rig, family, draw). "4b-A0-c1" -> ("4b", "A0", "c1");
# "35b-A-2" -> ("35b", "A", "2"); "4b-T2cr" -> ("4b", "T2cr", "").
_DRAW = re.compile(r"-(c\d+|\d+)$")


def split_tag(tag):
    rig = "35b" if tag.startswith("35b") else ("4b" if tag.startswith("4b") else "?")
    rest = tag[len(rig) + 1:] if rig != "?" else tag
    m = _DRAW.search(rest)
    if m:
        return rig, rest[: m.start()], m.group(1)
    return rig, rest, ""


def score(d, pred_path):
    """Accuracy via sigeval — the ONLY scorer. Returns None on failure.

    Fails CLOSED: a crashed scorer never falls back to stale rows and never
    clobbers a good report with empty stdout.
    """
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
            print(f"  !! sigeval failed for {d}: {e}", file=sys.stderr)
            return None
        if not os.path.exists(rp) or os.path.getmtime(rp) < key_mtime:
            print(f"  !! sigeval produced no fresh rows for {d} (rc={rep.returncode}): "
                  f"{rep.stderr.strip()[:200]}", file=sys.stderr)
            return None
        open(os.path.join(d, "sigeval-report.txt"), "w").write(rep.stdout)
    verd = [json.loads(l) for l in open(rp) if l.strip()]
    if not verd:
        return None
    return 100 * sum(1 for v in verd if v["correct"]) / len(verd)


def arms_registry():
    try:
        return {k: v for k, v in
                json.load(open(os.path.join(EX, "eval", "ablation", "arms.json"))).items()
                if not k.startswith("_")}
    except Exception:
        return {}


def cell(d, arms):
    tag = os.path.basename(d.rstrip("/"))
    p = os.path.join(d, "predictions.jsonl")
    if not os.path.exists(p):
        return None
    rows = [json.loads(l) for l in open(p) if l.strip()]
    if not rows:
        return None
    dead = sum(1 for r in rows
               if r.get("predicted") == "unknown" and r.get("confidence") == "no-output")
    acc = score(d, p)
    if acc is None:
        return None
    man = {}
    mp = os.path.join(d, "run-manifest.json")
    if os.path.exists(mp):
        try:
            man = json.load(open(mp))
        except Exception:
            pass
    # Telemetry from the archive, not from runner-log text. Tool EXECUTIONS come
    # from leather's own log (ground truth); calls/issue counts every LLM round
    # the proxy saw across all stages — the honest cost figure for multi-stage arms.
    calls = tools = toks = 0
    lp = os.path.join(d, "logprobs.jsonl.gz")
    if os.path.exists(lp):
        for l in gzip.open(lp, "rt", errors="replace"):
            l = l.strip()
            if not l:
                continue
            try:
                rec = json.loads(l)
            except Exception:
                continue
            calls += 1
            toks += (rec.get("usage") or {}).get("total_tokens") or 0
    # Last log timestamp = when the cell actually finished. File mtimes are
    # NOT usable for this: any git checkout/stash rewrites them, which made
    # every archive report the same bogus end time and 10h+ durations.
    last_log_ts = ""
    ev = os.path.join(d, "run-evidence.log.gz")
    if os.path.exists(ev):
        with gzip.open(ev, "rt", errors="replace") as f:
            for line in f:
                if "executing tool" in line:
                    tools += 1
                if line.startswith("time="):
                    last_log_ts = line[5:].split(None, 1)[0]
    # Wall-clock: `started` is the manifest's own stamp; the end is when the
    # archive's predictions landed. Both come from the archive, so a cell's
    # duration survives long after the runner log is gone.
    import datetime
    started = man.get("started", "")
    ended_ts, dur = 0.0, 0
    for src in (last_log_ts, None):
        if not src:
            continue
        try:
            ended_ts = datetime.datetime.fromisoformat(src).timestamp()
            break
        except (ValueError, TypeError):
            ended_ts = 0.0
    if started and ended_ts:
        try:
            t0 = datetime.datetime.fromisoformat(started).timestamp()
            dur = max(0, int(ended_ts - t0))
        except (ValueError, TypeError):
            dur = 0
    rig, family, draw = split_tag(tag)
    return dict(tag=tag, rig=rig, arm=family, draw=draw, acc=acc, rows=len(rows),
                dead=dead, cpi=(calls / len(rows)) if rows else 0.0, tools=tools,
                ktok=toks / 1000.0,
                var=arms.get(family, {}).get("variable", ""),
                compare_to=arms.get(family, {}).get("compare_to", ""),
                started=started, ended_ts=ended_ts, dur_s=dur)


def load_cells(pattern=None):
    """All scoreable cells, newest archive per tag, optionally filtered."""
    arms = arms_registry()
    best = {}
    for d in sorted(glob.glob(os.path.join(RUNS, "*/"))):
        c = cell(d, arms)
        if c:
            best[c["tag"]] = c
    cells = list(best.values())
    if pattern:
        cells = [c for c in cells if matches(c["tag"], pattern)]
    return cells


def matches(tag, pattern):
    """Filter grammar, deliberately forgiving.

    A bare prefix works ('4b', '4b-G'), globs work ('4b-*-c1', '*c[23]'),
    and a substring works ('T2c'). Comma-separated patterns OR together;
    a leading '!' negates the whole expression.
    """
    if not pattern:
        return True
    pattern = pattern.strip()
    neg = pattern.startswith("!")
    if neg:
        pattern = pattern[1:].strip()
    hit = False
    for pat in (p.strip() for p in pattern.split(",") if p.strip()):
        if (fnmatch.fnmatch(tag, pat) or fnmatch.fnmatch(tag, pat + "*")
                or pat.lower() in tag.lower()):
            hit = True
            break
    return (not hit) if neg else hit


def families(cells):
    """(rig, arm) -> {'cells': [...], 'mean', 'spread', 'accs', 'n'} — draws ordered."""
    out = {}
    for c in cells:
        out.setdefault((c["rig"], c["arm"]), []).append(c)
    fam = {}
    for key, cs in out.items():
        cs.sort(key=lambda c: c["tag"])
        accs = [c["acc"] for c in cs]
        mean = sum(accs) / len(accs)
        # A family that mixes confirmatory draws (c1..cN, run under the frozen
        # registration) with exploratory ones (no draw, or a bare -2 repeat)
        # has a mean that is NOT any registered quantity. Displays flag it so
        # a screenshot of this table can't be quoted as the registered figure:
        # 4b-S1 reads 60.9×4 here, 60.4×3 in the registered analysis.
        conf = any(c["draw"].startswith("c") for c in cs)
        expl = any(not c["draw"].startswith("c") for c in cs)
        fam[key] = dict(cells=cs, accs=accs, n=len(accs), mean=mean,
                        mixed=(conf and expl),
                        spread=(max(accs) - min(accs)) if len(accs) > 1 else 0.0)
    return fam


def contrasts(cells):
    """Registered contrasts from arms.json `compare_to`, evaluated on family means.

    Returns rows sorted by |effect| descending: dict(rig, arm, base, effect,
    n_arm, n_base, variable).
    """
    fam = families(cells)
    arms = arms_registry()
    rows = []
    for (rig, arm), f in fam.items():
        base = arms.get(arm, {}).get("compare_to")
        if not base:
            continue
        b = fam.get((rig, base))
        if not b:
            continue
        rows.append(dict(rig=rig, arm=arm, base=base,
                         effect=f["mean"] - b["mean"],
                         arm_mean=f["mean"], base_mean=b["mean"],
                         n_arm=f["n"], n_base=b["n"],
                         variable=arms.get(arm, {}).get("variable", "")))
    rows.sort(key=lambda r: -abs(r["effect"]))
    return rows


SCOPES = ("all", "confirmatory", "exploratory")

# Column glossary — one definition per column, shared by every surface so a
# reader never has to guess what a header means or find it in a docstring.
COLUMN_HELP = [
    ("rig",       "which model served the cell: 4b (Qwen3-4B) or 35b (Qwen3-35B-A3B)"),
    ("arm",       "the harness configuration under test — its row in eval/ablation/arms.json"),
    ("draw",      "which replication: cN = registered confirmatory draw, -N = exploratory repeat,"),
    ("",          "blank = the original single exploratory draw"),
    ("acc",       "accept-set accuracy over all 250 issues, scored by sigeval (the only scorer)"),
    ("dur",       "wall-clock for the cell, from the manifest's start to the last evidence-log line"),
    ("no-out",    "rows that produced NO usable answer. For most arms this is noise; for S1 it is"),
    ("",          "the mechanism — a fresh-session boundary makes ~11% of rows unanswerable"),
    ("calls/iss", "LLM round-trips per issue across all stages — the honest cost of a multi-stage arm"),
    ("tools",     "tool EXECUTIONS counted from leather's own log, not inferred from prompts"),
    ("ktok",      "total tokens (prompt + completion) the proxy saw, in thousands"),
    ("started",   "manifest timestamp when the cell began"),
    ("ended",     "last timestamp INSIDE run-evidence.log.gz — file mtimes lie after a git checkout"),
]


def legend(nocolor=False):
    """Two-line color key: what the accuracy and duration palettes mean."""
    if nocolor:
        return ["  acc   low ......... high        dur   short ......... long (ranked in view)"]
    d, r = "\033[2m", "\033[0m"
    acc = f"{d}acc{r}  \033[31m██ <74{r}  \033[33m██ 74-84{r}  \033[32m██ >=84{r}"
    dur = f"{d}dur{r}  \033[34m██ short{r} \033[36m██ mid{r} \033[35m██ long{r} {d}(ranked within this view){r}"
    return [f"  {acc}     {dur}"]


def fmt_duration(s):
    """Fixed-width duration with the minute and second fields column-aligned.

    'compact' formats like '9m 37s' / '54m 22s' put the unit boundary in a
    different place on every row, which is unreadable in a column.
    """
    if not s:
        return "       ?"
    if s < 3600:
        return f"{s // 60:3d}m {s % 60:02d}s"
    return f"{s // 3600:3d}h {(s % 3600) // 60:02d}m"


def in_scope(c, scope):
    """Experiment scope, so a watcher can show ONE campaign.

    confirmatory = the registered draws (-c1, -c2, ... under registration
    96cc418). exploratory = everything else: the original single-draw atlas
    and the -2..-7 repeat cells. Mixing them in one live view is what made
    the watcher unreadable — they answer different questions.
    """
    if not scope or scope == "all":
        return True
    conf = c["draw"].startswith("c")
    if scope == "confirmatory":
        return conf
    if scope == "exploratory":
        return not conf
    return True


def tag_widths(cells):
    """(arm_w, draw_w) so tags can be rendered in aligned segment columns."""
    if not cells:
        return 4, 2
    return (max(len(c["arm"]) for c in cells),
            max((len(c["draw"]) for c in cells), default=0))


def fmt_tag(c, arm_w, draw_w):
    """'4b T2cr c3' — rig, arm and draw in adjacent aligned columns.

    Aligned columns already separate the three fields; the hyphens that used
    to sit between them added width without information and pushed the parts
    far enough apart to read as unrelated. Rig stays right-aligned so '35b'
    and ' 4b' share an edge.
    """
    return f"{c['rig']:>3} {c['arm']:>{arm_w}} {c['draw']:<{draw_w}}"


def tag_header(arm_w, draw_w):
    return f"{'rig':>3} {'arm':>{arm_w}} {'draw':<{draw_w}}"


def tag_layout(cells):
    """(arm_w, draw_w, tag_column_width) — width reserves room for the
    'draw' header even when draws are two characters, so the next column
    never clips the label."""
    aw, dw = tag_widths(cells)
    return aw, dw, 3 + 1 + aw + 1 + max(dw, 4)


def draw_spark(accs):
    """Sparkline of an arm's draws, scaled to ITS OWN range.

    The stock sparkline normalizes against zero, which renders every draw of
    a 3-point spread as a full block — exactly the variation a replicated
    campaign exists to show. Rescaling to [min, max] makes shape visible;
    the magnitude lives in the adjacent spread/mean columns.
    """
    from tui.fmt import sparkline
    if not accs:
        return ""
    if len(accs) < 2:
        return "·"
    lo, hi = min(accs), max(accs)
    if hi - lo < 1e-9:
        return "▄" * len(accs)
    return sparkline([(a - lo) / (hi - lo) * 7 + 1 for a in accs], max_value=8)


def newest_mtime():
    """Cheap change detector for live refresh: newest predictions.jsonl mtime."""
    newest = 0.0
    for p in glob.glob(os.path.join(RUNS, "*", "predictions.jsonl")):
        try:
            newest = max(newest, os.path.getmtime(p))
        except OSError:
            pass
    return newest
