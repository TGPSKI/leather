#!/usr/bin/env python3
"""Interactive results matrix — filterable table, arm rankings, contrasts.

  python3 eval/scripts/matrix-tui.py             # all cells
  python3 eval/scripts/matrix-tui.py 4b-*-c*     # start filtered
  SIGEVAL=/path/to/sigeval python3 ...           # skip `go run` per cell

Keys: [tab] view  [f] filter  [F] clear  [s] sort  [r] reload  [↑↓ PgUp/Dn]
scroll  [q] quit.

Filter grammar (shared with table.py, see matrixdata.matches): bare prefix
('4b', '4b-G'), glob ('4b-*-c1'), substring ('T2c'), comma-OR, '!' negates.

Why a TUI and not more columns: with replication, a single cell's accuracy
stopped being the interesting number — the arm's DRAW SPREAD is. The table
view therefore spends the old "variable under test" space on the arm's draw
sparkline, mean and this cell's deviation from it; rankings and contrasts
get their own views rather than being squeezed into a row.

Stdlib only; curses primitives vendored under eval/scripts/tui/.
"""
from __future__ import annotations

import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import matrixdata as md                                    # noqa: E402
from tui.charts import bar_chart                           # noqa: E402
from tui.fmt import sparkline                              # noqa: E402
from tui.framework import TuiApp, curses_main              # noqa: E402

VIEWS = ("table", "rankings", "contrasts")
SORTS = ("acc", "tag", "spread")
RELOAD_SECS = 20


class MatrixTui(TuiApp):
    def __init__(self, stdscr, pattern=""):
        super().__init__(stdscr)
        self.pattern = pattern
        self.view = 0
        self.sort = 0
        self.editing = False
        self.buf = ""
        self.cells = []
        self.stamp = 0.0
        self.seen_mtime = 0.0
        self.reload()
        self.curses.halfdelay(20)   # 2s: getch returns -1 so we can refresh

    # ---- data ------------------------------------------------------------
    def reload(self):
        self.cells = md.load_cells()          # unfiltered; filter at render
        self.stamp = time.time()
        self.seen_mtime = md.newest_mtime()

    def visible(self):
        cs = [c for c in self.cells if md.matches(c["tag"], self.pattern)]
        key = SORTS[self.sort]
        fam = md.families(cs)
        if key == "acc":
            cs.sort(key=lambda c: -c["acc"])
        elif key == "tag":
            cs.sort(key=lambda c: c["tag"])
        else:
            cs.sort(key=lambda c: -fam[(c["rig"], c["arm"])]["spread"])
        return cs, fam

    # ---- chrome ----------------------------------------------------------
    def header(self, max_x):
        C = self.curses
        title = f" RESULTS MATRIX — {VIEWS[self.view]} "
        self._put(0, 1, title, C.A_BOLD)
        filt = self.pattern or "all"
        self._put(0, len(title) + 2, f"filter: {filt}",
                  C.color_pair(2) if self.pattern else C.A_DIM)
        age = int(time.time() - self.stamp)
        self._put(0, max_x - 26, f"sort:{SORTS[self.sort]}  {age}s ago", C.A_DIM)

    def footer(self, max_y, max_x, total=0, avail=0):
        C = self.curses
        if self.editing:
            self._put(max_y - 1, 1, f"filter> {self.buf}_", C.color_pair(3) | C.A_BOLD)
            return
        self.render_footer_items(max_y, [
            ("[tab] view", C.A_DIM), ("[f] filter", C.A_DIM),
            ("[F] clear", C.A_DIM), ("[s] sort", C.A_DIM),
            ("[r] reload", C.A_DIM), ("[q] quit", C.A_DIM)])
        # Right-aligned in the footer, never over a column header.
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
            self._put(2, 3, "no cells match this filter", self.curses.A_DIM)
        elif VIEWS[self.view] == "table":
            total, avail = self.view_table(cs, fam, body, max_x)
        elif VIEWS[self.view] == "rankings":
            self.view_rankings(cs, fam, body, max_y, max_x)
        else:
            total, avail = self.view_contrasts(cs, body, max_x)
        self.footer(max_y, max_x, total, avail)

    def view_table(self, cs, fam, body, max_x):
        # Column offsets are computed, not hardcoded: the tag renders as
        # aligned rig/arm/draw segments whose width depends on the selection
        # (35b-Eauto is wider than 4b-B), so everything right of it shifts.
        C = self.curses
        aw, dw, tagw = md.tag_layout(cs)
        x_acc = 1 + tagw + 1
        x_dead, x_cpi, x_tools = x_acc + 7, x_acc + 14, x_acc + 21
        x_ktok, x_spark = x_acc + 28, x_acc + 36
        x_mean, x_delta, x_var = x_acc + 47, x_acc + 56, x_acc + 64
        self._put(1, 1, md.tag_header(aw, dw), C.A_DIM)
        for x, lbl in ((x_acc, f"{'acc':>6}"), (x_dead, f"{'no-out':>6}"),
                       (x_cpi, f"{'c/iss':>6}"), (x_tools, f"{'tools':>6}"),
                       (x_ktok, f"{'ktok':>6}"), (x_spark, f"{'draws':<9}"),
                       (x_mean, f"{'arm mean':>8}"), (x_delta, f"{'Δ':>6}"),
                       (x_var, "variable")):
            if x < max_x - 2:
                self._put(1, x, lbl, C.A_DIM)
        rows = body - 1
        self.scroll = max(0, min(self.scroll, max(0, len(cs) - rows)))
        for i, c in enumerate(cs[self.scroll:self.scroll + rows]):
            y = 2 + i
            f = fam[(c["rig"], c["arm"])]
            self._put(y, 1, md.fmt_tag(c, aw, dw))
            self._put(y, x_acc, f"{c['acc']:6.1f}", self.acc_attr(c["acc"]))
            if c["dead"]:
                self._put(y, x_dead, f"{c['dead']:6d}", C.color_pair(4))
            else:
                self._put(y, x_dead, f"{'-':>6}", C.A_DIM)
            self._put(y, x_cpi, f"{c['cpi']:6.2f}", C.A_DIM)
            self._put(y, x_tools, f"{c['tools']:6d}",
                      C.color_pair(2) if c["tools"] else C.A_DIM)
            self._put(y, x_ktok, f"{c['ktok']:6.0f}" if c["ktok"] else f"{'?':>6}",
                      C.A_DIM)
            if f["n"] > 1:
                self._put(y, x_spark, f"{md.draw_spark(f['accs']):<9}", C.color_pair(6))
                self._put(y, x_mean, f"{f['mean']:5.1f}×{f['n']}", C.A_DIM)
                delta = c["acc"] - f["mean"]
                self._put(y, x_delta, f"{delta:+6.1f}",
                          C.color_pair(1) if delta >= 0 else C.color_pair(4))
            else:
                self._put(y, x_spark, f"{'·':<9}", C.A_DIM)
                self._put(y, x_mean, f"{'—':>8}", C.A_DIM)
            if max_x > x_var + 12:
                self._put(y, x_var, c["var"][: max_x - x_var - 2], C.A_DIM)
        return len(cs), rows

    def view_rankings(self, cs, fam, body, max_y, max_x):
        """Arm families ranked by mean accuracy — the 'which arm wins' view."""
        C = self.curses
        keys = sorted(fam.items(), key=lambda kv: -kv[1]["mean"])
        means = [v["mean"] for _, v in keys]
        floor = max(0.0, (min(means) - 2.0)) if means else 0.0
        series = [{"label": f"{k[1]}", "count": v["mean"] - floor} for k, v in keys]
        plot_h = max(5, min(14, body - 6))
        nxt = bar_chart(self._put, C, 1, series, plot_h, max_x,
                        title=f"arm mean accuracy — {len(series)} families"
                              f"{' (filtered)' if self.pattern else ''}"
                              f"   [baseline {floor:.0f}%]",
                        title_attr=C.A_BOLD, axis_attr=C.A_DIM,
                        color=C.color_pair(2),
                        fmt=lambda n, _f=floor: f"{n + _f:.0f}",
                        pref_bar_w=4, value_labels=True, label_fit=True,
                        no_data_text="no cells match this filter")
        self._put(nxt, 1, f"{'arm':<10}{'mean':>7}{'draws':>7}  {'spread':>7}  "
                          f"{'sparkline':<10} cells", C.A_DIM)
        rows = max_y - nxt - 3
        for i, (k, v) in enumerate(keys[:max(0, rows)]):
            y = nxt + 1 + i
            spread_attr = (C.color_pair(4) if v["spread"] > 4.8 else
                           C.color_pair(3) if v["spread"] > 2.4 else C.A_DIM)
            self._put(y, 1, f"{k[0]}-{k[1]:<7}")
            self._put(y, 11, f"{v['mean']:6.1f}", self.acc_attr(v["mean"]))
            self._put(y, 18, f"{v['n']:6d}", C.A_DIM)
            self._put(y, 26, f"{v['spread']:6.1f}", spread_attr)
            self._put(y, 35, md.draw_spark(v['accs']), C.color_pair(6))
            self._put(y, 46, ", ".join(c["draw"] or "—" for c in v["cells"])[: max_x - 48],
                      C.A_DIM)

    def view_contrasts(self, cs, body, max_x):
        """arms.json compare_to pairs, on family means. Band = ±2.4 points."""
        C = self.curses
        rows_ = md.contrasts(cs)
        self._put(1, 1, f"{'contrast':<18}{'effect':>8}{'arm':>8}{'base':>8}  "
                        f"{'draws':<9}{'band':<6} variable", C.A_DIM)
        if not rows_:
            self._put(3, 3, "no contrast pairs in this selection "
                            "(both sides must be present)", C.A_DIM)
            return 0, 0
        avail = body - 1
        self.scroll = max(0, min(self.scroll, max(0, len(rows_) - avail)))
        for i, r in enumerate(rows_[self.scroll:self.scroll + avail]):
            y = 2 + i
            eff = r["effect"]
            inband = abs(eff) <= 2.4
            attr = (C.A_DIM if inband else
                    C.color_pair(1) if eff > 0 else C.color_pair(4))
            self._put(y, 1, f"{r['rig']}-{r['arm']} vs {r['base']:<8}"[:18])
            self._put(y, 19, f"{eff:+8.1f}", attr)
            self._put(y, 27, f"{r['arm_mean']:8.1f}", C.A_DIM)
            self._put(y, 35, f"{r['base_mean']:8.1f}", C.A_DIM)
            self._put(y, 45, f"{r['n_arm']}×{r['n_base']:<6}", C.A_DIM)
            self._put(y, 54, "in-band" if inband else "clear ",
                      C.A_DIM if inband else C.color_pair(2))
            if max_x > 70:
                self._put(y, 62, r["variable"][: max_x - 64], C.A_DIM)
        return len(rows_), avail

    # ---- input -----------------------------------------------------------
    def handle_key(self, key):
        C = self.curses
        if key == -1:                       # halfdelay tick
            if md.newest_mtime() != self.seen_mtime:
                self.reload()
            return False
        if self.editing:
            if key in (10, 13):             # enter
                self.pattern, self.editing, self.scroll = self.buf.strip(), False, 0
            elif key == 27:                 # esc
                self.editing = False
            elif key in (C.KEY_BACKSPACE, 127, 8):
                self.buf = self.buf[:-1]
            elif 32 <= key < 127:
                self.buf += chr(key)
            return False
        if key in (ord("q"), ord("Q")):
            return True
        if key == ord("\t"):
            self.view = (self.view + 1) % len(VIEWS)
            self.scroll = 0
        elif key == ord("f"):
            self.editing, self.buf = True, self.pattern
        elif key == ord("F"):
            self.pattern, self.scroll = "", 0
        elif key == ord("s"):
            self.sort = (self.sort + 1) % len(SORTS)
        elif key == ord("r"):
            self.reload()
        elif key == C.KEY_UP:
            self.scroll = max(0, self.scroll - 1)
        elif key == C.KEY_DOWN:
            self.scroll += 1
        elif key == C.KEY_PPAGE:
            self.scroll = max(0, self.scroll - 10)
        elif key == C.KEY_NPAGE:
            self.scroll += 10
        elif key in (C.KEY_HOME, ord("g")):
            self.scroll = 0
        return False


if __name__ == "__main__":
    pat = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("FILTER", "")
    curses_main(lambda scr: MatrixTui(scr, pat))
