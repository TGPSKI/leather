"""Curses TUI base framework shared by the project TUIs.

Extracted from the two live TUIs (sh-web-analytics, sh-github-analytics):
bounds-checked put, base color pairs, run loop with min-size guard, footer
renderer, scroll indicator, CSV loading, and the curses.wrapper bootstrap.
View bodies and data loading stay app-side. Stdlib only.
"""
from __future__ import annotations

import csv
from pathlib import Path

MIN_ROWS, MIN_COLS = 16, 60


def read_csv(path):
    """List of DictReader rows; [] when the file is missing."""
    path = Path(path)
    if not path.is_file():
        return []
    with path.open("r", newline="", encoding="utf-8") as fh:
        return list(csv.DictReader(fh))


class TuiApp:
    """Base curses app: color init, put, run loop, footer, scroll indicator.

    Subclasses implement render(max_y, max_x) and handle_key(key) -> bool
    (True = quit), and may override init_extra_pairs() to register color
    pairs beyond the base 1-6.
    """

    #: base pairs shared by all apps (fg on default background)
    #: 1 green, 2 cyan, 3 yellow, 4 red, 5 black-on-cyan (active), 6 blue

    def __init__(self, stdscr):
        import curses
        self.curses = curses
        self.stdscr = stdscr
        self.scroll = 0
        curses.start_color()
        curses.use_default_colors()
        try:
            curses.curs_set(0)
        except curses.error:
            pass
        curses.init_pair(1, curses.COLOR_GREEN, -1)
        curses.init_pair(2, curses.COLOR_CYAN, -1)
        curses.init_pair(3, curses.COLOR_YELLOW, -1)
        curses.init_pair(4, curses.COLOR_RED, -1)
        curses.init_pair(5, curses.COLOR_BLACK, curses.COLOR_CYAN)
        curses.init_pair(6, curses.COLOR_BLUE, -1)
        self.init_extra_pairs()

    def init_extra_pairs(self):
        """Hook: register app-specific color pairs (7+)."""

    def _put(self, y, x, text, attr=0):
        max_y, max_x = self.stdscr.getmaxyx()
        if y < 0 or y >= max_y or x >= max_x - 1:
            return
        try:
            self.stdscr.addstr(y, x, text[: max_x - 1 - x], attr)
        except self.curses.error:
            pass

    def run(self):
        while True:
            self.stdscr.erase()
            max_y, max_x = self.stdscr.getmaxyx()
            if max_y < MIN_ROWS or max_x < MIN_COLS:
                self._put(0, 0, f"Terminal too small (need {MIN_COLS}x{MIN_ROWS}).")
                self._put(1, 0, "[q] quit")
            else:
                self.render(max_y, max_x)
            self.stdscr.refresh()
            key = self.stdscr.getch()
            if self.handle_key(key):
                return

    def render(self, max_y, max_x):
        raise NotImplementedError

    def handle_key(self, key) -> bool:
        """Return True to quit. Base handles q/Q/ESC only."""
        return key in (ord("q"), ord("Q"), 27)

    def render_footer_items(self, max_y, items, x=1):
        """Render [(text, attr)] along the bottom row."""
        for text, attr in items:
            self._put(max_y - 1, x, text, attr)
            x += len(text) + 2

    def scroll_indicator(self, y, max_x, total, avail):
        if total > avail:
            shown_end = min(self.scroll + avail, total)
            self._put(y, max_x - 22,
                      f"{self.scroll + 1}-{shown_end}/{total}",
                      self.curses.A_DIM)


def curses_main(app_factory):
    """curses.wrapper bootstrap; swallows KeyboardInterrupt."""
    import curses
    try:
        curses.wrapper(lambda scr: app_factory(scr).run())
    except KeyboardInterrupt:
        pass
