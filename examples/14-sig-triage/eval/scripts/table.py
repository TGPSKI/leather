#!/usr/bin/env python3
"""Lean leaderboard from ARCHIVES — the renderer behind watch-matrix.sh.

This is a MONITOR: which cells exist, how they scored, what they cost. It
deliberately carries no analysis columns. Arm means, deviations, spreads and
per-arm sparklines all moved to the interactive browser (matrix-tui.py),
because in a live view of running arms they answered a question nobody was
asking, pushed the useful columns off the right edge, and — with a '*'
appended to mixed-draw means — broke column alignment outright.

Scoping (all three compose):
  RIG=4b|35b            one rig
  SCOPE=confirmatory    registered -cN draws only; exploratory = the rest
  FILTER=<pattern>      bare prefix, glob, substring, comma-OR, '!' negates

Loading and scoring live in matrixdata.py, shared with matrix-tui.py so the
two surfaces cannot disagree about a number.
"""
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import matrixdata as md                       # noqa: E402
from tui.fmt import duration                  # noqa: E402

NC = os.environ.get("NOCOLOR")
B, D, R = ("", "", "") if NC else ("\033[1m", "\033[2m", "\033[0m")
GRN, YEL, RED, CYN, BLU, MAG = (("",) * 6 if NC else
                                ("\033[32m", "\033[33m", "\033[31m",
                                 "\033[36m", "\033[34m", "\033[35m"))

FILTER = os.environ.get("FILTER", "")
SCOPE = os.environ.get("SCOPE") or "all"
ONLY = os.environ.get("RIG")
SORT = os.environ.get("SORT") or "acc"
REV = os.environ.get("SORT_REV") == "1"

# Every sort is "most interesting first" by default; SORT_REV=1 flips it.
SORT_KEYS = {
    "acc":   lambda c: -c["acc"],
    "tools": lambda c: -c["tools"],
    "ktok":  lambda c: -c["ktok"],
    "dur":   lambda c: -c["dur_s"],
    "noout": lambda c: -c["dead"],
    "tag":   lambda c: c["tag"],
}
if SORT not in SORT_KEYS:
    sys.exit(f"SORT must be one of {', '.join(SORT_KEYS)} (got {SORT!r})")

if SCOPE not in md.SCOPES:
    sys.exit(f"SCOPE must be one of {', '.join(md.SCOPES)} (got {SCOPE!r})")

cells = [c for c in md.load_cells(FILTER) if md.in_scope(c, SCOPE)]
ARM_W, DRAW_W, TAG_W = md.tag_layout(cells)

for rig in (("35b", "4b") if not ONLY else (ONLY,)):
    rc = sorted([c for c in cells if c["rig"] == rig],
                key=SORT_KEYS[SORT], reverse=REV)
    if not rc:
        continue
    scoping = " · ".join(x for x in (
        f"scope {SCOPE}" if SCOPE != "all" else "",
        f"filter {FILTER}" if FILTER else "",
        f"sort {SORT}{'↑' if REV else '↓'}") if x)
    if not ONLY:
        print(f"\n  {B}{rig}{R}  {D}{len(rc)} cells"
              f"{'  ·  ' + scoping if scoping else ''}{R}")
    # Duration gets its own scale, ranked WITHIN this view rather than against
    # fixed thresholds: what counts as a slow cell depends entirely on which
    # arms you are looking at (P2 runs 9m, T2cr 47m). Blue→magenta, so a slow
    # cell never reads as a bad score — accuracy owns green/yellow/red.
    durs = sorted(c["dur_s"] for c in rc if c["dur_s"])

    def dur_attr(s):
        if not s or len(durs) < 2:
            return D
        q = durs.index(min(durs, key=lambda x: abs(x - s))) / (len(durs) - 1)
        return BLU if q < 0.34 else (CYN if q < 0.67 else MAG)

    print(f"     {D}{md.tag_header(ARM_W, DRAW_W):{TAG_W}s} {'acc':>6s} "
          f"{'no-out':>7s} {'calls/iss':>10s} {'tools':>7s} {'ktok':>7s}"
          f"   {'started':<14s}{'ended':<9s}{'dur':>8s}{R}")
    for c in rc:
        col = GRN if c["acc"] >= 84 else (YEL if c["acc"] >= 74 else RED)
        lost = f"{RED}{c['dead']:7d}{R}" if c["dead"] else f"{D}      -{R}"
        tc = CYN if c["tools"] else D
        ktok = f"{c['ktok']:7.0f}" if c["ktok"] else "      ?"
        start = c["started"][5:16].replace("T", " ") if c["started"] else "?"
        end = (time.strftime("%H:%M", time.localtime(c["ended_ts"]))
               if c["ended_ts"] else "?")
        dur = duration(c["dur_s"]) if c["dur_s"] else "?"
        dcol = dur_attr(c["dur_s"])
        print(f"     {md.fmt_tag(c, ARM_W, DRAW_W):{TAG_W}s} {col}{c['acc']:6.1f}{R} "
              f"{lost} {D}{c['cpi']:10.2f}{R} {tc}{c['tools']:7d}{R} {D}{ktok}{R}"
              f"   {D}{start:<14s}{end:<9s}{R}{dcol}{dur:>8s}{R}")
