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


def _bin_series(series, k):
    """Aggregate every k adjacent buckets into one (counts sum)."""
    out = []
    for i in range(0, len(series), k):
        chunk = series[i:i + k]
        item = {"label": chunk[0]["label"],
                "count": sum(b["count"] for b in chunk)}
        if any(b.get("peak") for b in chunk):
            item["peak"] = True
        segs = [b["segments"] for b in chunk if b.get("segments")]
        if segs:
            item["segments"] = [
                (sum(s[j][0] for s in segs if j < len(s)),
                 next(s[j][1] for s in segs if j < len(s)))
                for j in range(max(len(s) for s in segs))
            ]
        out.append(item)
    return out


def bar_chart(put, curses_mod, top, series, plot_h, max_x, *,
              title=None, title_attr=0, axis_attr=0, color=0,
              fmt=compact_num, right_margin=2,
              bar_w=None, pref_bar_w=None, max_bar_w=6,
              overflow="shrink", value_labels=False, peak_attr=None,
              label_every=1, label_row_offset=1, label_pad=2,
              half_blocks=False, label_fit=False, bin_unit="",
              clip_ratio=None, clip_min_bars=5, clip_max_frac=0.25,
              no_data_text="no data available"):
    """Draw a vertical bar chart of series from row `top`; return next row.

    put(y, x, text, attr) is the caller's bounds-checked writer.
    overflow: 'shrink' fits all bars — narrowing them, then aggregating
    adjacent buckets once even 1-column bars won't fit (github style);
    'slice' keeps bar width and shows only the trailing bars (web style).
    clip_ratio: cap the y-axis at ratio x the median non-zero bucket so a lone
    outlier can't flatten the rest. Over-cap bars run to the top row and are
    labelled there with their real value + '↑'. None disables it.
    """
    A_DIM = curses_mod.A_DIM
    y = top
    if not series:
        if title:
            put(y, 1, title, title_attr)
            y += 1
        put(y, 3, no_data_text, A_DIM)
        return y + 1

    axis_w = 7
    plot_x = axis_w + 1
    avail = max(1, max_x - plot_x - right_margin)
    gap = 1
    # More buckets than columns: aggregate rather than let the tail fall off
    # the right edge. put() clips silently, so an un-binned long series drew
    # only its *oldest* bars while the y-axis still scaled to the invisible
    # newest ones — the whole chart squashed against a max you can't see.
    binned = 1
    if overflow == "shrink" and len(series) > avail:
        binned = -(-len(series) // avail)
        series = _bin_series(series, binned)
    n = len(series)
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

    if title:
        if binned > 1:
            title = f"{title}  ·  {binned}{bin_unit or ''} per bar"
        put(y, 1, title, title_attr)
        y += 1

    max_val = max((b["count"] for b in series), default=0)

    # Outlier clipping: a single scanner flood 29x the median leaves every
    # other bar a 1-cell stub against an axis max nothing else approaches.
    # Cap the scale at a robust bound and mark what ran past it.
    clipped = set()
    if clip_ratio and n >= clip_min_bars:
        nz = sorted(b["count"] for b in series if b["count"] > 0)
        if nz:
            med = nz[len(nz) // 2]
            # Floor the cap at p90 as well as ratio x median. On a broad
            # spread (hourly counts of 1..180 for one small domain) the ratio
            # alone caps just above the median and clips a quarter of the
            # bars, flattening the shape it was meant to reveal.
            p90 = nz[min(len(nz) - 1, int(0.9 * (len(nz) - 1)))]
            cap = max(med * clip_ratio, p90)
            over = [i for i, b in enumerate(series) if b["count"] > cap]
            # Three ways clipping is the wrong call, all seen in real windows:
            #  - it would clip a large share of the bars: that's a second mode,
            #    not an outlier, and the cap would hide real data;
            #  - the axis barely shrinks: nothing was dominating it;
            #  - the typical bar is still a stub afterwards, so the ↑ marks buy
            #    no readability (a domain whose hours run 1..180 is spread out,
            #    not spiked).
            gain = med / cap * plot_h if cap else 0
            if (med > 0 and over
                    and len(over) <= max(1, len(nz) * clip_max_frac)
                    and max_val >= cap * 2
                    and gain >= 2):
                clipped = set(over)
                max_val = max([cap] + [b["count"] for i, b in enumerate(series)
                                       if i not in clipped])

    # y-axis with max / mid / 0 labels
    for i in range(plot_h):
        put(y + i, axis_w, "│", axis_attr)
    for frac, row in ((1.0, y), (0.5, y + plot_h // 2), (0.0, y + plot_h - 1)):
        label = fmt(int(max_val * frac))
        put(row, axis_w - len(label), label, A_DIM)
        put(row, axis_w, "┤", axis_attr)
    slot = bar_w + gap
    span = min(n * bar_w + (n - 1) * gap, avail)
    put(y + plot_h, axis_w, "└" + "─" * span, axis_attr)

    heights = []
    for i, b in enumerate(series):
        count = b["count"]
        half = 0
        if i in clipped:
            # run to the row below the top; that row carries the real value
            heights.append((plot_h - 1, 0))
            continue
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
        heights.append((h, half))

    def _touches_top(j):
        """Whether bar j writes on the top plot row: a clipped bar's label, a
        full-height bar (block or ▄ half-cell), or the ▲ of a peak bar one
        cell short — the peak glyph sits on the row above the bar."""
        if j in clipped:
            return True
        h_j = heights[j][0] + heights[j][1]
        if h_j >= plot_h:
            return True
        return (peak_attr is not None and series[j].get("peak")
                and h_j == plot_h - 1)

    # Left edge of the unwritten part of the top plot row; clipped-bar labels
    # advance it and bars that touch the top row bump it.
    top_free = plot_x

    # Value labels need a blank column after them, or adjacent ones smear
    # into each other ("85" + "60" reads as "8560"). Where they don't fit the
    # y-axis max/mid/0 labels still carry the scale.
    for i, b in enumerate(series):
        x = plot_x + i * slot
        count = b["count"]
        h, half = heights[i]

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

        if i in clipped:
            # Top row states what ran off the scale.
            # ▲ still rides along when the bar is also a peak — dropping it
            # here left the tallest bar unmarked while shorter ones kept it.
            mark = "▲" if (peak_attr is not None and b.get("peak")) else ""
            vs = f"{mark}{fmt(count)}↑"
            # The label may spill into neighbouring columns, but only across
            # top-row space nothing else touches: stop before the next bar
            # that reaches this row (drawn later, it would chop the tail —
            # "▲320↑" -> "▲32▲"), and shift left over free space rather than
            # degrade, without covering an earlier label or top-touching bar.
            limit = min([plot_x + span]
                        + [plot_x + j * slot for j in range(i + 1, n)
                           if _touches_top(j)])
            start = x
            if start + len(vs) > limit:
                start = limit - len(vs)
            if start < top_free:
                vs, start = (mark or "↑") * bar_w, x
            put(y, start, vs, peak_attr if peak_attr is not None else color)
            top_free = start + len(vs)
        else:
            if _touches_top(i):
                top_free = max(top_free, x + bar_w)
            if peak_attr is not None and b.get("peak") and h_eff < plot_h:
                put(y + plot_h - 1 - h_eff, x, "▲" * min(bar_w, 1), peak_attr)

            # value above bar — only when there's a clear row above it, so the
            # tallest bar's label never lands on the title/axis-max line.
            if value_labels and count > 0 and h_eff < plot_h:
                vs = fmt(count)
                # label_fit: write the full value only when it fits before the
                # next bar; a truncated "1.3k"->"1" is worse than no label.
                if not label_fit:
                    put(y + plot_h - 1 - h_eff, x, vs[:slot], A_DIM)
                elif len(vs) <= slot - 1:
                    put(y + plot_h - 1 - h_eff, x, vs, A_DIM)

        if not label_fit and (label_every <= 1 or i % label_every == 0
                              or i == n - 1):
            put(y + plot_h + label_row_offset, x,
                b["label"][: slot + label_pad], A_DIM)

    if label_fit:
        # Sparse x labels: space them by how wide they actually are instead
        # of blanking every one when bars are narrower than a date. Walk
        # newest -> oldest so the most recent bucket always keeps its label.
        widest = max((len(b["label"]) for b in series), default=0)
        step = max(label_every, 1, -(-(widest + 1) // slot))
        wanted = sorted({n - 1} | set(range(0, n, step)), reverse=True)
        leftmost = plot_x + span + 1
        for i in wanted:
            label = series[i]["label"]
            x = plot_x + i * slot
            # keep the rightmost label inside the plot rather than letting
            # put() clip it to "08-0"
            x = min(x, max(plot_x, plot_x + span - len(label)))
            if x + len(label) < leftmost:
                put(y + plot_h + label_row_offset, x, label, A_DIM)
                leftmost = x
    return y + plot_h + 1 + label_row_offset
