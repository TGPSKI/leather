"""Vertical bar-chart primitive shared by the project TUIs.

One renderer, two modes, matching the two live implementations it was
extracted from:

- single-series (sh-github-analytics `_bar_chart`): one color, optional
  value labels above bars, x label under every bar, title row.
- stacked-segment (sh-web-analytics `_render_chart`): per-item `segments`
  [(value, attr), ...], optional `peak` markers, sparse x labels on the
  axis row.

Series items are dicts with at least {'label', 'count'}; stacked mode adds
'segments' and optionally 'peak'. Returns the row index just below the
chart so callers can keep composing downward.
"""
from __future__ import annotations

from .fmt import compact_num


def bar_chart(put, curses_mod, top, series, plot_h, max_x, *,
              title=None, title_attr=0, axis_attr=0, color=0,
              fmt=compact_num, right_margin=2,
              bar_w=None, pref_bar_w=None, max_bar_w=6,
              overflow="shrink", value_labels=False, peak_attr=None,
              label_every=1, label_row_offset=1, label_pad=2,
              half_blocks=False, label_fit=False,
              no_data_text="no data available"):
    """Draw a vertical bar chart of series from row `top`; return next row.

    put(y, x, text, attr) is the caller's bounds-checked writer.
    overflow: 'shrink' recomputes bar width to fit all bars (github style);
    'slice' keeps bar width and shows only the trailing bars (web style).
    """
    A_DIM = curses_mod.A_DIM
    y = top
    if title:
        put(y, 1, title, title_attr)
        y += 1
    if not series:
        put(y, 3, no_data_text, A_DIM)
        return y + 1

    axis_w = 7
    plot_x = axis_w + 1
    avail = max_x - plot_x - right_margin
    n = len(series)
    gap = 1
    if bar_w is None:
        cap = pref_bar_w if pref_bar_w is not None else max_bar_w
        bar_w = max(1, min(cap, avail // n - gap))
    if n * (bar_w + gap) > avail:
        gap = 0
        if overflow == "slice":
            bar_w = 1
            series = series[-(avail // max(1, bar_w + gap)):]
            n = len(series)
        else:
            bar_w = max(1, avail // n)

    max_val = max((b["count"] for b in series), default=0)

    # y-axis with max / mid / 0 labels
    for i in range(plot_h):
        put(y + i, axis_w, "│", axis_attr)
    for frac, row in ((1.0, y), (0.5, y + plot_h // 2), (0.0, y + plot_h - 1)):
        label = fmt(int(max_val * frac))
        put(row, axis_w - len(label), label, A_DIM)
        put(row, axis_w, "┤", axis_attr)
    put(y + plot_h, axis_w, "└" + "─" * min(n * (bar_w + gap), avail), axis_attr)

    for i, b in enumerate(series):
        x = plot_x + i * (bar_w + gap)
        count = b["count"]
        half = 0
        if max_val > 0 and count > 0:
            if half_blocks:
                # Double the vertical resolution: a trailing half-cell is
                # drawn as a lower-half block on the row above the bar.
                h2 = max(1, round(count / max_val * plot_h * 2))
                h, half = h2 // 2, h2 % 2
                if h == 0:
                    h, half = 0, 1
            else:
                h = max(1, round(count / max_val * plot_h))
        else:
            h = 0

        segments = b.get("segments")
        top_attr = color
        if segments:
            from .windows import stack_cells
            seg = stack_cells([v for v, _ in segments], h)
            cell = 0
            for (value, attr), cells in zip(segments, seg):
                for _ in range(cells):
                    put(y + plot_h - 1 - cell, x, "█" * bar_w, attr)
                    cell += 1
                if cells:
                    top_attr = attr
        else:
            for cell in range(h):
                put(y + plot_h - 1 - cell, x, "█" * bar_w, color)

        if half and h < plot_h:
            put(y + plot_h - 1 - h, x, "▄" * bar_w, top_attr)
        h_eff = h + half

        if peak_attr is not None and b.get("peak") and h_eff < plot_h:
            put(y + plot_h - 1 - h_eff, x, "▲" * min(bar_w, 1), peak_attr)

        # value above bar — only when there's a clear row above it, so the
        # tallest bar's label never lands on the title/axis-max line.
        if value_labels and count > 0 and h_eff < plot_h:
            vs = fmt(count)
            # label_fit: write the full value only when it fits before the
            # next bar; a truncated "1.3k"->"1" is worse than no label.
            if not label_fit or len(vs) <= bar_w + gap:
                put(y + plot_h - 1 - h_eff, x, vs[: bar_w + gap], A_DIM)

        if label_every <= 1 or i % label_every == 0 or i == n - 1:
            label = b["label"]
            if label_fit and len(label) > bar_w + gap + label_pad:
                label = ""
            put(y + plot_h + label_row_offset, x,
                label[: bar_w + gap + label_pad], A_DIM)
    return y + plot_h + 1 + label_row_offset
