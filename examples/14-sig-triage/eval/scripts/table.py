#!/usr/bin/env python3
"""Render the results table from ARCHIVES, not from runner logs.

The log-driven version read $TMP/<prefix>-<rig>.log for a fixed set of
prefixes, so any battery written to a different filename was invisible — T3,
G2 and F2 all completed and none appeared. Logs are also mutable, orphaned
and duplicated across re-runs. The archive under results/runs/<tag>/ is the
source of truth everywhere else in this project; this makes the display
agree. Loading and scoring live in matrixdata.py, shared with matrix-tui.py
so the two surfaces cannot disagree.

Env: RIG=4b|35b limits to one rig. FILTER=<pattern> limits to matching tags
(bare prefix, glob, substring, comma-OR, '!' negates — see matrixdata).

The old "variable under test" column is gone: with replication the useful
per-row facts are the arm's DRAW SPREAD and where this draw sits in it, and
the variable is one lookup away in arms.json. It was also silently blank for
every confirmatory cell, because "A0-c1" is not a key in arms.json. For
rankings and contrasts use matrix-tui.py — that space is a chart, not a
column.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import matrixdata as md                       # noqa: E402

NC = os.environ.get("NOCOLOR")
B, D, R = ("", "", "") if NC else ("\033[1m", "\033[2m", "\033[0m")
GRN, YEL, RED, CYN, BLU = (("", "", "", "", "") if NC else
                           ("\033[32m", "\033[33m", "\033[31m", "\033[36m", "\033[34m"))

FILTER = os.environ.get("FILTER", "")
ONLY = os.environ.get("RIG")

cells = md.load_cells(FILTER)
fam = md.families(cells)
ARM_W, DRAW_W, TAG_W = md.tag_layout(cells)

for rig in (("35b", "4b") if not ONLY else (ONLY,)):
    rc = sorted([c for c in cells if c["rig"] == rig], key=lambda c: -c["acc"])
    if not rc:
        continue
    if not ONLY:
        print(f"\n  {B}{rig}{R}  {D}{len(rc)} cells"
              f"{f' · filter {FILTER}' if FILTER else ''}{R}")
    print(f"     {D}{md.tag_header(ARM_W, DRAW_W):{TAG_W}s} {'acc':>6s} "
          f"{'no-out':>6s} {'calls/iss':>9s} {'tools':>6s} {'ktok':>6s}  "
          f"{'draws':<10s}{'arm mean':>10s}{'Δ':>7s}{R}")
    for c in rc:
        col = GRN if c["acc"] >= 84 else (YEL if c["acc"] >= 74 else RED)
        lost = f"{RED}{c['dead']:6d}{R}" if c["dead"] else f"{D}     -{R}"
        tc = CYN if c["tools"] else D
        ktok = f"{c['ktok']:6.0f}" if c["ktok"] else "     ?"
        f = fam[(c["rig"], c["arm"])]
        if f["n"] > 1:
            spark = f"{BLU}{md.draw_spark(f['accs']):<10}{R}"
            delta = c["acc"] - f["mean"]
            dcol = GRN if delta >= 0 else RED
            meanf = f"{D}{f['mean']:6.1f}×{f['n']}{R}"
            deltaf = f"{dcol}{delta:+7.1f}{R}"
        else:
            spark, meanf, deltaf = f"{D}{'·':<10}{R}", f"{D}{'—':>10}{R}", f"{'':>7}"
        print(f"     {md.fmt_tag(c, ARM_W, DRAW_W):{TAG_W}s} {col}{c['acc']:6.1f}{R} {lost} "
              f"{D}{c['cpi']:9.2f}{R} {tc}{c['tools']:6d}{R} {D}{ktok}{R}  "
              f"{spark}{meanf}{deltaf}")
