#!/usr/bin/env python3
"""Interactive results matrix — cells, rankings, paired verdicts, detail.

  python3 eval/scripts/matrix-tui.py             # all cells
  python3 eval/scripts/matrix-tui.py 4b-*-c*     # start filtered
  make matrix                                    # same, from the example dir

Views ([tab] cycles):
  cells      every archived draw, ranked; [space] opens its detail card
  rankings   arm means as a baselined chart + per-arm draw list
  pairs      McNemar exact per declared contrast — per wave and pooled

Filter grammar (shared with table.py, see matrixdata.matches): bare prefix
('4b', '4b-G'), glob ('4b-*-c1'), substring ('T2c'), comma-OR, '!' negates.

Everything reads the archives under results/runs/<tag>/ through
matrixdata.py — the same loader and the same sigeval bridge table.py uses,
so no two surfaces can disagree about a number.

Stdlib only; curses primitives vendored under eval/scripts/tui/.
"""
from __future__ import annotations

import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import matrixdata as md                                    # noqa: E402
from tui.charts import bar_chart                           # noqa: E402
from tui.framework import TuiApp, curses_main              # noqa: E402

# One McNemar implementation for the whole project: import the registered
# analysis path's rather than writing a second one that can drift from it.
_spec = importlib.util.spec_from_file_location(
    "paired_verdicts", os.path.join(HERE, "paired-verdicts.py"))
_pv = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_pv)

VIEWS = ("cells", "rankings", "pairs")
VIEW_HELP = {
    "cells": "every archived draw, ranked by accuracy — [space] opens detail",
    "rankings": "arm means (baselined chart) + each arm's draws and spread",
    "pairs": "McNemar exact on discordant issues, per wave and pooled",
}
SORTS = ("acc", "tag", "spread")
NULL_BAND = 2.4          # measured single-run noise floor, in points


class MatrixTui(TuiApp):
    def __init__(self, stdscr, pattern=""):
        super().__init__(stdscr)
        self.pattern = pattern
        self.view = 0
        self.sort = 0
        self.editing = False
        self.buf = ""
        self.cursor = 0
        self.detail = False
        self.cells = []
        self.stamp = 0.0
        self.seen_mtime = 0.0
        self._pairs_cache = (None, None)
        self.reload()
        self.curses.halfdelay(20)   # 2s tick: getch returns -1 so we refresh

    # ---- data ------------------------------------------------------------
    def reload(self):
        self.cells = md.load_cells()      # unfiltered; filtering happens at render
        self.stamp = time.time()
        self.seen_mtime = md.newest_mtime()
        self._pairs_cache = (None, None)

    def visible(self):
        cs = [c for c in self.cells if md.matches(c["tag"], self.pattern)]
        fam = md.families(cs)
        key = SORTS[self.sort]
        if key == "acc":
            cs.sort(key=lambda c: -c["acc"])
        elif key == "tag":
            cs.sort(key=lambda c: c["tag"])
        else:
            cs.sort(key=lambda c: -fam[(c["rig"], c["arm"])]["spread"])
        return cs, fam

    def pairs(self, cs):
        """Per-contrast McNemar: each (arm-cN vs base-cN) plus the pooled test.

        Cached per (pattern, load) because it reads every cell's verdict rows.
        """
        keycache, val = self._pairs_cache
        if keycache == self.pattern and val is not None:
            return val
        fam = md.families(cs)
        arms = md.arms_registry()
        rows = []
        for (rig, arm), f in sorted(fam.items()):
            base = arms.get(arm, {}).get("compare_to")
            if not base or (rig, base) not in fam:
                continue
            bcells = {c["draw"]: c for c in fam[(rig, base)]["cells"]}
            waves, px, py = [], {}, {}
            for c in f["cells"]:
                b = bcells.get(c["draw"])
                if not b:
                    continue
                a_rows = _pv.rows_for(c["tag"])
                b_rows = _pv.rows_for(b["tag"])
                if not a_rows or not b_rows:
                    continue
                n01, n10, p = _pv.mcnemar_exact(a_rows, b_rows)
                waves.append((c["draw"] or "—", c["acc"] - b["acc"], n01, n10, p))
                for k, v in a_rows.items():
                    px[f"{c['draw']}:{k}"] = v
                for k, v in b_rows.items():
                    py[f"{c['draw']}:{k}"] = v
            if not waves:
                continue
            pn01, pn10, pp = _pv.mcnemar_exact(px, py)
            eff = (100 * sum(px.values()) / len(px)) - (100 * sum(py.values()) / len(py))
            rows.append(dict(rig=rig, arm=arm, base=base, waves=waves,
                             pooled=(eff, pn01, pn10, pp, len(px)),
                             variable=arms.get(arm, {}).get("variable", "")))
        rows.sort(key=lambda r: -abs(r["pooled"][0]))
        self._pairs_cache = (self.pattern, rows)
        return rows

    # ---- chrome ----------------------------------------------------------
    def header(self, max_x):
        C = self.curses
        view = VIEWS[self.view]
        self._put(0, 1, "RESULTS MATRIX", C.A_BOLD)
        self._put(0, 16, f"▸ {view}", C.color_pair(5) | C.A_BOLD)
        self._put(0, 18 + len(view) + 2, VIEW_HELP[view][: max_x - 40], C.A_DIM)
        right = f"filter:{self.pattern or 'all'}  sort:{SORTS[self.sort]}  {int(time.time() - self.stamp)}s"
        self._put(0, max(20, max_x - len(right) - 2), right,
                  C.color_pair(2) if self.pattern else C.A_DIM)

    def footer(self, max_y, max_x, total=0, avail=0):
        """Fixed control legend — every key, and what it does."""
        C = self.curses
        if self.editing:
            self._put(max_y - 1, 1,
                      f"filter> {self.buf}_    (enter applies · esc cancels · "
                      f"empty = show all)", C.color_pair(3) | C.A_BOLD)
            return
        if self.detail:
            keys = [("[space/esc] close card", C.A_DIM),
                    ("[↑↓] other draw", C.A_DIM), ("[q] quit", C.A_DIM)]
        else:
            keys = [("[tab] next view", C.A_DIM), ("[f] set filter", C.A_DIM),
                    ("[F] clear filter", C.A_DIM), ("[s] cycle sort", C.A_DIM),
                    ("[space] detail", C.A_DIM), ("[r] reload", C.A_DIM),
                    ("[q] quit", C.A_DIM)]
        self.render_footer_items(max_y, keys)
        if total > avail > 0:
            end = min(self.scroll + avail, total)
            self._put(max_y - 1, max_x - 14, f"{self.scroll + 1}-{end}/{total}", C.A_DIM)

    def acc_attr(self, acc):
        C = self.curses
        return (C.color_pair(1) if acc >= 84 else
                C.color_pair(3) if acc >= 74 else C.color_pair(4))

    # ---- views -----------------------------------------------------------
    def render(self, max_y, max_x):
        self.header(max_x)
        body = max_y - 3
        cs, fam = self.visible()
        total = avail = 0
        if not cs:
            self._put(2, 3, "no cells match this filter — [F] clears it",
                      self.curses.A_DIM)
        elif self.detail and VIEWS[self.view] == "cells":
            self.cursor = max(0, min(self.cursor, len(cs) - 1))
            self.view_detail(cs[self.cursor], fam, max_y, max_x)
        elif VIEWS[self.view] == "cells":
            total, avail = self.view_cells(cs, fam, body, max_x)
        elif VIEWS[self.view] == "rankings":
            self.view_rankings(cs, fam, body, max_y, max_x)
        else:
            total, avail = self.view_pairs(cs, body, max_x)
        self.footer(max_y, max_x, total, avail)

    def view_cells(self, cs, fam, body, max_x):
        C = self.curses
        aw, dw, tagw = md.tag_layout(cs)
        x_acc = 1 + tagw + 1
        x_dead, x_cpi, x_tools = x_acc + 7, x_acc + 14, x_acc + 21
        x_ktok, x_mean = x_acc + 28, x_acc + 36
        x_delta, x_spread, x_var = x_acc + 46, x_acc + 54, x_acc + 62
        self._put(1, 1, md.tag_header(aw, dw), C.A_DIM)
        for x, lbl in ((x_acc, f"{'acc':>6}"), (x_dead, f"{'no-out':>6}"),
                       (x_cpi, f"{'c/iss':>6}"), (x_tools, f"{'tools':>6}"),
                       (x_ktok, f"{'ktok':>6}"), (x_mean, f"{'arm mean':>9}"),
                       (x_delta, f"{'Δ mean':>6}"), (x_spread, f"{'spread':>6}"),
                       (x_var, "variable under test")):
            if x < max_x - 2:
                self._put(1, x, lbl, C.A_DIM)
        rows = body - 1
        self.cursor = max(0, min(self.cursor, len(cs) - 1))
        if self.cursor < self.scroll:
            self.scroll = self.cursor
        elif self.cursor >= self.scroll + rows:
            self.scroll = self.cursor - rows + 1
        self.scroll = max(0, min(self.scroll, max(0, len(cs) - rows)))
        for i, c in enumerate(cs[self.scroll:self.scroll + rows]):
            y = 2 + i
            sel = (self.scroll + i) == self.cursor
            base = C.A_REVERSE if sel else 0
            f = fam[(c["rig"], c["arm"])]
            self._put(y, 1, f"{md.fmt_tag(c, aw, dw):<{tagw}}", base)
            self._put(y, x_acc, f"{c['acc']:6.1f}", self.acc_attr(c["acc"]) | base)
            self._put(y, x_dead, f"{c['dead']:6d}" if c["dead"] else f"{'-':>6}",
                      (C.color_pair(4) if c["dead"] else C.A_DIM) | base)
            self._put(y, x_cpi, f"{c['cpi']:6.2f}", C.A_DIM | base)
            self._put(y, x_tools, f"{c['tools']:6d}",
                      (C.color_pair(2) if c["tools"] else C.A_DIM) | base)
            self._put(y, x_ktok, f"{c['ktok']:6.0f}" if c["ktok"] else f"{'?':>6}",
                      C.A_DIM | base)
            if f["n"] > 1:
                self._put(y, x_mean, f"{f['mean']:5.1f}×{f['n']}"
                                     f"{'*' if f['mixed'] else ' '}", C.A_DIM | base)
                d = c["acc"] - f["mean"]
                self._put(y, x_delta, f"{d:+6.1f}",
                          (C.color_pair(1) if d >= 0 else C.color_pair(4)) | base)
                self._put(y, x_spread, f"{f['spread']:6.1f}", C.A_DIM | base)
            else:
                self._put(y, x_mean, f"{'single draw':>11}", C.A_DIM | base)
            if max_x > x_var + 12:
                self._put(y, x_var, c["var"][: max_x - x_var - 2], C.A_DIM | base)
        return len(cs), rows

    def view_detail(self, c, fam, max_y, max_x):
        """Everything the archive knows about one draw."""
        C = self.curses
        arms = md.arms_registry()
        a = arms.get(c["arm"], {})
        man = {}
        mp = os.path.join(md.RUNS, c["tag"], "run-manifest.json")
        if os.path.exists(mp):
            try:
                man = json.load(open(mp))
            except Exception:
                pass
        f = fam[(c["rig"], c["arm"])]
        y = 1
        self._put(y, 1, f"{c['tag']}", C.A_BOLD | C.color_pair(2))
        self._put(y, len(c["tag"]) + 3,
                  f"rig {c['rig']} · arm {c['arm']} · draw {c['draw'] or '—'}", C.A_DIM)
        y += 2

        def section(title):
            nonlocal y
            self._put(y, 1, title, C.A_BOLD)
            y += 1

        def kv(k, v, attr=0):
            nonlocal y
            if y >= max_y - 2 or v in (None, ""):
                return
            self._put(y, 3, f"{k:<20}", C.A_DIM)
            self._put(y, 24, str(v)[: max_x - 26], attr)
            y += 1

        section("what was under test")
        kv("variable", a.get("variable", "—"))
        kv("compare_to", a.get("compare_to", "—"))
        for i, line in enumerate(_wrap(a.get("description", ""), max_x - 24)[:6]):
            kv("why" if i == 0 else "", line)
        kv("stages", a.get("stages"))
        kv("turns", a.get("turns"))
        kv("catalog", a.get("catalog"))
        kv("rules", a.get("rules"))
        y += 1

        section("result")
        kv("accuracy", f"{c['acc']:.1f}%  ({c['rows']} rows)", self.acc_attr(c["acc"]))
        kv("unanswerable", f"{c['dead']} rows"
                           f"{'  ← mechanism, not noise' if c['dead'] > 20 else ''}",
           C.color_pair(4) if c["dead"] else 0)
        kv("llm calls/issue", f"{c['cpi']:.2f}")
        kv("tool executions", c["tools"])
        kv("tokens", f"{c['ktok']:.0f}k" if c["ktok"] else "?")
        if f["n"] > 1:
            kv("arm mean", f"{f['mean']:.1f}% over {f['n']} draws"
                           f"{'  (MIXED exploratory + confirmatory)' if f['mixed'] else ''}",
               C.color_pair(3) if f["mixed"] else 0)
            kv("this draw vs mean", f"{c['acc'] - f['mean']:+.1f}")
            kv("draws", ", ".join(f"{x['draw'] or '—'}={x['acc']:.1f}" for x in f["cells"]))
        y += 1

        section("provenance (run-manifest.json)")
        for k in ("model", "endpoint", "started", "git_commit", "agent_sha",
                  "index", "index_sha", "analyze_cache_sha", "corpus_sha",
                  "force_tool", "concurrency", "logprob"):
            kv(k, man.get(k))
        y += 1

        section("archive")
        d = os.path.join(md.RUNS, c["tag"])
        try:
            for fn in sorted(os.listdir(d)):
                kv(fn, f"{os.path.getsize(os.path.join(d, fn)) / 1024:.0f} KB")
        except OSError:
            pass

    def view_rankings(self, cs, fam, body, max_y, max_x):
        C = self.curses
        keys = sorted(fam.items(), key=lambda kv: -kv[1]["mean"])
        means = [v["mean"] for _, v in keys]
        floor = max(0.0, (min(means) - 2.0)) if means else 0.0
        series = [{"label": f"{k[1]}", "count": v["mean"] - floor} for k, v in keys]
        plot_h = max(5, min(12, body - 8))
        nxt = bar_chart(self._put, C, 1, series, plot_h, max_x,
                        title=f"arm mean accuracy — {len(series)} families"
                              f"{' (filtered)' if self.pattern else ''}"
                              f"   [baseline {floor:.0f}%, bars are means]",
                        title_attr=C.A_BOLD, axis_attr=C.A_DIM,
                        color=C.color_pair(2),
                        fmt=lambda n, _f=floor: f"{n + _f:.0f}",
                        pref_bar_w=4, value_labels=True, label_fit=True,
                        no_data_text="no cells match this filter")
        self._put(nxt, 1, f"{'arm':<10}{'mean':>7}{'draws':>7}{'spread':>8}   "
                          f"per-draw accuracy", C.A_DIM)
        rows = max_y - nxt - 3
        for i, (k, v) in enumerate(keys[:max(0, rows)]):
            y = nxt + 1 + i
            spread_attr = (C.color_pair(4) if v["spread"] > 2 * NULL_BAND else
                           C.color_pair(3) if v["spread"] > NULL_BAND else C.A_DIM)
            self._put(y, 1, f"{k[0]}-{k[1]:<7}")
            self._put(y, 11, f"{v['mean']:6.1f}"
                             f"{'*' if v['mixed'] else ''}", self.acc_attr(v["mean"]))
            self._put(y, 19, f"{v['n']:5d}", C.A_DIM)
            self._put(y, 26, f"{v['spread']:7.1f}", spread_attr)
            self._put(y, 36, "  ".join(f"{x['draw'] or '—'}:{x['acc']:.1f}"
                                       for x in v["cells"])[: max_x - 38], C.A_DIM)

    def view_pairs(self, cs, body, max_x):
        C = self.curses
        rows_ = self.pairs(cs)
        self._put(1, 1, f"{'contrast':<16}{'draw':>6}{'Δ acc':>8}"
                        f"{'disc a/b':>11}{'p (exact)':>12}  verdict / variable",
                  C.A_DIM)
        if not rows_:
            self._put(3, 3, "no paired cells here — a contrast needs BOTH arms "
                            "at the same draw", C.A_DIM)
            return 0, 0
        lines = []
        for r in rows_:
            for w, d, n01, n10, p in r["waves"]:
                lines.append(("wave", r, w, d, n01, n10, p))
            eff, n01, n10, p, n = r["pooled"]
            lines.append(("pooled", r, f"×{len(r['waves'])}", eff, n01, n10, p))
        avail = body - 1
        self.scroll = max(0, min(self.scroll, max(0, len(lines) - avail)))
        for i, (kind, r, w, d, n01, n10, p) in enumerate(
                lines[self.scroll:self.scroll + avail]):
            y = 2 + i
            pooled = kind == "pooled"
            attr = C.A_BOLD if pooled else C.A_DIM
            self._put(y, 1, f"{r['arm']} vs {r['base']:<8}"[:16] if pooled else "",
                      attr)
            self._put(y, 17, f"{w:>5}", attr)
            self._put(y, 23, f"{d:+7.1f}",
                      (C.color_pair(1) if d > 0 else C.color_pair(4)) |
                      (C.A_BOLD if pooled else 0))
            self._put(y, 32, f"{n01:4d}/{n10:<4d}", C.A_DIM)
            self._put(y, 45, f"{p:11.2e}" if p < 0.001 else f"{p:11.4f}", attr)
            if pooled:
                verdict = ("RESOLVED" if p < 0.05 and abs(d) > NULL_BAND
                           else "unresolved")
                self._put(y, 58, verdict,
                          C.color_pair(1) if verdict == "RESOLVED" else C.color_pair(3))
                self._put(y, 68, r["variable"][: max_x - 70], C.A_DIM)
            elif abs(d) <= NULL_BAND:
                self._put(y, 58, "in band", C.A_DIM)
        return len(lines), avail

    # ---- input -----------------------------------------------------------
    def handle_key(self, key):
        C = self.curses
        if key == -1:
            if md.newest_mtime() != self.seen_mtime:
                self.reload()
            return False
        if self.editing:
            if key in (10, 13):
                self.pattern, self.editing = self.buf.strip(), False
                self.scroll = self.cursor = 0
                self._pairs_cache = (None, None)
            elif key == 27:
                self.editing = False
            elif key in (C.KEY_BACKSPACE, 127, 8):
                self.buf = self.buf[:-1]
            elif 32 <= key < 127:
                self.buf += chr(key)
            return False
        if key in (ord("q"), ord("Q")):
            return True
        if key == ord(" "):
            self.detail = not self.detail and VIEWS[self.view] == "cells"
        elif key == 27:
            self.detail = False
        elif key == ord("\t"):
            self.view = (self.view + 1) % len(VIEWS)
            self.scroll = 0
            self.detail = False
        elif key == ord("f"):
            # Start EMPTY, not pre-filled: a pre-filled buffer silently
            # appends what you type onto the old pattern.
            self.editing, self.buf = True, ""
        elif key == ord("F"):
            self.pattern, self.scroll, self.cursor = "", 0, 0
            self._pairs_cache = (None, None)
        elif key == ord("s"):
            self.sort = (self.sort + 1) % len(SORTS)
        elif key == ord("r"):
            self.reload()
        elif key == C.KEY_UP:
            self.cursor = max(0, self.cursor - 1)
            self.scroll = max(0, self.scroll - 1) if not self.detail else self.scroll
        elif key == C.KEY_DOWN:
            self.cursor += 1
            if not self.detail:
                self.scroll += 1
        elif key == C.KEY_PPAGE:
            self.scroll = max(0, self.scroll - 10)
            self.cursor = max(0, self.cursor - 10)
        elif key == C.KEY_NPAGE:
            self.scroll += 10
            self.cursor += 10
        elif key in (C.KEY_HOME, ord("g")):
            self.scroll = self.cursor = 0
        return False


def _wrap(text, width):
    out, line = [], ""
    for word in (text or "").split():
        if len(line) + len(word) + 1 > width:
            out.append(line)
            line = word
        else:
            line = f"{line} {word}".strip()
    if line:
        out.append(line)
    return out


if __name__ == "__main__":
    pat = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("FILTER", "")
    curses_main(lambda scr: MatrixTui(scr, pat))
