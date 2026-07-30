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
GRN, YEL, RED, CYN = (("", "", "", "") if NC else
                      ("\033[32m", "\033[33m", "\033[31m", "\033[36m"))

FILTER = os.environ.get("FILTER", "")
SCOPE = os.environ.get("SCOPE") or "all"
ONLY = os.environ.get("RIG")

if SCOPE not in md.SCOPES:
    sys.exit(f"SCOPE must be one of {', '.join(md.SCOPES)} (got {SCOPE!r})")

cells = [c for c in md.load_cells(FILTER) if md.in_scope(c, SCOPE)]
ARM_W, DRAW_W, TAG_W = md.tag_layout(cells)

for rig in (("35b", "4b") if not ONLY else (ONLY,)):
    rc = sorted([c for c in cells if c["rig"] == rig], key=lambda c: -c["acc"])
    if not rc:
        continue
    scoping = " · ".join(x for x in (
        f"scope {SCOPE}" if SCOPE != "all" else "",
        f"filter {FILTER}" if FILTER else "") if x)
    if not ONLY:
        print(f"\n  {B}{rig}{R}  {D}{len(rc)} cells"
              f"{'  ·  ' + scoping if scoping else ''}{R}")
    print(f"     {D}{md.tag_header(ARM_W, DRAW_W):{TAG_W}s} {'acc':>6s} "
          f"{'no-out':>7s} {'calls/iss':>10s} {'tools':>7s} {'ktok':>7s}"
          f"  {'started':<12s}{'ended':<7s}{'dur':>7s}{R}")
    for c in rc:
        col = GRN if c["acc"] >= 84 else (YEL if c["acc"] >= 74 else RED)
        lost = f"{RED}{c['dead']:7d}{R}" if c["dead"] else f"{D}      -{R}"
        tc = CYN if c["tools"] else D
        ktok = f"{c['ktok']:7.0f}" if c["ktok"] else "      ?"
        start = c["started"][5:16].replace("T", " ") if c["started"] else "?"
        end = time.strftime("%H:%M", time.localtime(c["ended_ts"]))
        dur = duration(c["dur_s"]) if c["dur_s"] else "?"
        print(f"     {md.fmt_tag(c, ARM_W, DRAW_W):{TAG_W}s} {col}{c['acc']:6.1f}{R} "
              f"{lost} {D}{c['cpi']:10.2f}{R} {tc}{c['tools']:7d}{R} {D}{ktok}{R}"
              f"  {D}{start:<12s}{end:<7s}{dur:>7s}{R}")
