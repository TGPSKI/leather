"""Interaction helpers shared by the project TUIs. Stdlib only.

Extracted from the third copy: `_prompt_search`/`_filtered` existed
verbatim in sh-github-analytics and again in sh-web-security, and `_hbar`
was on its way to a third home. Same admission test as the rest of the
package — none of these knows what a row means.

These operate on a `framework.TuiApp` (they use `_put`, `stdscr`, `curses`
and reset `scroll`) but take the app as an argument rather than living on
the base class, so `framework.py` stays byte-identical to the copies
already vendored across the portfolio.
"""
from __future__ import annotations

import contextlib


def prompt_search(app, max_len=40):
    """Bottom-row `/` prompt; return the stripped input ('' on error).

    Echoes into the footer row with a visible cursor, restores curses
    state whatever happens, and resets app.scroll so the filtered list
    starts at the top. Callers keep the returned needle app-side (the
    convention is `self.search`) and apply it with `filter_rows`.
    """
    curses = app.curses
    max_y, max_x = app.stdscr.getmaxyx()
    app._put(max_y - 1, 0, ("/" + " " * (max_x - 2))[: max_x - 1])
    curses.echo()
    with contextlib.suppress(curses.error):
        curses.curs_set(1)
    try:
        raw = app.stdscr.getstr(max_y - 1, 1, max_len)
        text = raw.decode("utf-8", "replace").strip()
    except curses.error:
        text = ""
    finally:
        curses.noecho()
        with contextlib.suppress(curses.error):
            curses.curs_set(0)
    app.scroll = 0
    return text


def filter_rows(rows, needle, key):
    """Case-insensitive substring filter on rows[i][key]; [] stays []."""
    if not needle:
        return rows
    needle = needle.lower()
    return [r for r in rows if needle in (r.get(key, "") or "").lower()]


def hbar(value, max_value, width):
    """Horizontal bar scaled to max_value, clamped to width cells."""
    if max_value <= 0 or width <= 0:
        return ""
    return "█" * max(0, min(width, int(value / max_value * width)))


def cycle(seq, current, step=1):
    """Next item of seq after current, wrapping; seq[0] if current is absent.

    The v/V view-and-metric cycling every app wrote as
    `VIEWS[(VIEWS.index(self.view) + 1) % len(VIEWS)]`.
    """
    try:
        return seq[(seq.index(current) + step) % len(seq)]
    except ValueError:
        return seq[0]
