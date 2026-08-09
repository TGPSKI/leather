"""Row x column intensity grids. Stdlib only.

The complement to `charts.bar_chart`: a bar chart answers "how much, over
time, for one series", a heatmap answers "which of these many series, on
which day". Once a table has more than a couple of dozen rows and one row
per event, reading it as text stops working — the eye needs the grid.

Domain-free by the same admission test as the rest of the package: this
module knows about intensities, glyphs and columns. What a row is, what
makes a cell hot, and which cells deserve a different glyph are all the
caller's business, supplied as values and callbacks.
"""
from __future__ import annotations

#: Default 5-step intensity ramp, blank -> full. Chosen so that "nothing
#: happened" is genuinely empty space rather than a dim character: a grid
#: of mostly-zero cells should read as a few marks on a page, not as
#: texture you have to look past.
RAMP = " ·▪▮█"


def ramp_glyph(value, ramp=RAMP):
    """Map a 0..1 intensity onto a ramp character.

    Anything above 0 gets at least the first non-blank step, so a small
    but real value can never render as absent — the distinction between
    "zero" and "nearly zero" is the one a heatmap most often has to make.
    """
    if value is None or value <= 0:
        return ramp[0]
    if value >= 1:
        return ramp[-1]
    steps = len(ramp) - 1
    return ramp[max(1, min(steps, int(value * steps) + 1))]


#: Scrollbar track and thumb. Box-drawing rather than blocks so the bar
#: reads as chrome at the edge of a pane and not as data in it.
TRACK, THUMB = "│", "█"


def scrollbar(put, curses_mod, top, height, x, total, offset, *,
              attr=0, track_attr=None, track=TRACK, thumb=THUMB):
    """Draw a vertical scrollbar at column x, from row `top`, `height` tall.

    total is the number of rows the list has; offset is the index of its
    first visible row; `height` is how many are visible. Draws nothing when
    everything fits — a scrollbar that is always full is furniture.

    The thumb is at least one cell tall however long the list is, because a
    thumb rounded to zero is indistinguishable from no overflow at all,
    which is the one thing the bar exists to say.
    """
    if total <= height or height <= 0:
        return
    A_DIM = curses_mod.A_DIM
    span = max(1, min(height, round(height * height / total)))
    reach = max(1, total - height)
    start = round((height - span) * min(offset, reach) / reach)
    for i in range(height):
        in_thumb = start <= i < start + span
        put(top + i, x, thumb if in_thumb else track,
            attr if in_thumb else (track_attr if track_attr is not None else A_DIM))


def diverging_bars(put, curses_mod, top, series, height, max_x, *,
                   title=None, title_attr=0, axis_attr=0,
                   pos_attr=0, neg_attr=0, fmt=str, label_every=None,
                   label_row_offset=1, bar_w=None, max_bar_w=6, gap=1,
                   right_margin=2, zero_glyph="\u2500",
                   no_data_text="no data available"):
    """Signed bars above and below a zero line; return the row below.

    `bar_chart` measures level and cannot draw a negative, because its
    heights grow out of a floor at zero. A change, a delta, a derivative —
    anything whose sign is the point — needs the axis in the middle
    instead, or the reader has to infer direction out of a colour and take
    it on trust.

    series items are {'label', 'value'} where value may be negative. The
    zero line always renders, including for an all-positive series, so
    "nothing went down" is visible rather than merely absent.
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
    avail = max(1, max_x - plot_x - right_margin)
    n = len(series)
    if bar_w is None:
        bar_w = max(1, min(max_bar_w, avail // n - gap))
    if n * (bar_w + gap) > avail:
        gap = 0
        bar_w = max(1, avail // n)
    slot = bar_w + gap

    peak = max((abs(b["value"]) for b in series), default=0)
    # Split the height either side of the zero line, which owns its own row.
    half = max(1, (height - 1) // 2)
    zero_row = y + half

    for i, half_label in ((0, peak), (half * 2, -peak)):
        label = fmt(half_label)
        put(y + i, max(0, axis_w - len(label)), label, A_DIM)
    put(zero_row, max(0, axis_w - 1), "0", A_DIM)
    span = min(n * bar_w + (n - 1) * gap, avail)
    put(zero_row, axis_w, "\u253c" + zero_glyph * span, axis_attr)

    for i, b in enumerate(series):
        value = b["value"]
        x = plot_x + i * slot
        if not value or not peak:
            continue
        cells = max(1, round(abs(value) / peak * half))
        attr = pos_attr if value > 0 else neg_attr
        for c in range(cells):
            row = zero_row - 1 - c if value > 0 else zero_row + 1 + c
            if y <= row <= zero_row + half:
                put(row, x, "\u2588" * bar_w, attr)

    step = label_every or max(1, -(-(max(len(str(b["label"])) for b in series) + 1) // slot))
    label_row = zero_row + half + label_row_offset
    leftmost = plot_x + span + 1
    for i in range(n - 1, -1, -1):
        if (n - 1 - i) % step:
            continue
        label = str(series[i]["label"])
        x = min(plot_x + i * slot, plot_x + span - len(label))
        if x + len(label) < leftmost:
            put(label_row, x, label, A_DIM)
            leftmost = x
    return label_row + 1


def heatmap(put, curses_mod, top, max_x, *, rows, col_labels,
            label_w=None, cell_w=1, gap=0, ramp=RAMP,
            glyph_for=None, attr_for=None, label_attr=0, header_attr=0,
            col_label_every=None, scroll=0, height=None, cursor=None,
            cursor_attr=None, header_gap=0, no_data_text="no data available"):
    """Draw an intensity grid from row `top`; return the row just below it.

    rows        [(label, [intensity, ...])] — one entry per grid row, each
                intensity a 0..1 float or None for "no observation". Rows
                need not be the same length; short rows are left blank.
    col_labels  one label per column; drawn vertically-sparse on the header
                row so long labels (dates) do not overwrite each other.
    glyph_for   (value, r, c) -> str, overriding the ramp. Whatever it
                returns is the WHOLE cell, centred and clipped to cell_w —
                unlike the ramp, which FILLS the cell with its glyph. A
                caller marking a cell 'C' wants one C under the column
                header, not cell_w of them: repeating it turns a five-wide
                column into twenty-five characters and slides every column
                after it out from under its own date.
    attr_for    (value, r, c) -> curses attr for the cell.
    scroll      first row index to draw; height caps how many are drawn.
    cursor      index of the row to highlight with cursor_attr.
    header_gap  blank rows between the column labels and the first row. A
                sparse grid sits directly under its dates and reads as one
                undifferentiated block; one blank row is the difference
                between a header and a first data row.

    Columns are clipped from the left when the grid is wider than the
    terminal: the newest column is the one that must survive, and dropping
    the oldest is the only truncation that keeps "now" on screen.
    """
    A_DIM = curses_mod.A_DIM
    y = top
    if not rows or not col_labels:
        put(y, 3, no_data_text, A_DIM)
        return y + 1

    if label_w is None:
        label_w = min(24, max(8, max(len(str(lbl)) for lbl, _ in rows)))
    grid_x = 3 + label_w + 1
    slot = cell_w + gap
    room = max(1, (max_x - grid_x - 2) // slot)
    first_col = max(0, len(col_labels) - room)
    cols = list(range(first_col, len(col_labels)))

    # Header: sparse column labels, spaced by how wide they actually are.
    # Walked newest -> oldest so the last column always keeps its label, and
    # each one is dropped rather than drawn if it would run into the label to
    # its right. Clamping alone is not enough: pinning the final label inside
    # the grid moves it left, on top of its neighbour, and two dates collide
    # into one unreadable run ("0708-06").
    step = col_label_every or max(1, -(-(max(len(str(c)) for c in col_labels) + 1) // slot))
    rightmost = grid_x + len(cols) * slot
    leftmost = rightmost + 1
    for i in range(len(cols) - 1, -1, -1):
        if (len(cols) - 1 - i) % step:
            continue
        label = str(col_labels[cols[i]])
        x = min(grid_x + i * slot, rightmost - len(label))
        if x + len(label) >= leftmost:
            continue
        put(y, x, label, header_attr or A_DIM)
        leftmost = x
    y += 1 + header_gap

    visible = rows[scroll:scroll + height] if height else rows[scroll:]
    for r_off, (label, values) in enumerate(visible):
        r = scroll + r_off
        row_attr = cursor_attr if (cursor is not None and r == cursor and
                                   cursor_attr is not None) else label_attr
        put(y, 3, str(label)[:label_w].ljust(label_w), row_attr)
        for i, c in enumerate(cols):
            value = values[c] if c < len(values) else None
            if glyph_for:
                text = str(glyph_for(value, r, c))[:cell_w].center(cell_w)
            else:
                text = ramp_glyph(value, ramp) * cell_w
            attr = attr_for(value, r, c) if attr_for else 0
            put(y, grid_x + i * slot, text, attr)
        y += 1
    return y
