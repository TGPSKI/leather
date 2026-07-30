"""Value-formatting helpers shared by the project TUIs. Stdlib only."""
from __future__ import annotations

from datetime import datetime


def human_bytes(n):
    n = float(n)
    if abs(n) < 1024:
        return f"{int(n)} B"
    for unit in ("KB", "MB", "GB", "TB"):
        n /= 1024
        if abs(n) < 1024:
            return f"{n:,.1f} {unit}"
    return f"{n / 1024:,.1f} PB"


def compact_num(n):
    n = int(n)
    if n >= 1_000_000:
        return f"{n / 1_000_000:.1f}M"
    if n >= 10_000:
        return f"{n / 1000:.0f}k"
    if n >= 1_000:
        return f"{n / 1000:.1f}k"
    return str(n)


def to_int(row, key, default=0):
    try:
        return int(row.get(key) or default)
    except (ValueError, TypeError):
        return default


def to_float(row, key, default=0.0):
    try:
        return float(row.get(key) or default)
    except (ValueError, TypeError):
        return default


def pct(part, whole):
    return f"{part / whole * 100:.0f}%" if whole else "0%"


def duration(seconds):
    """Compact human duration: '2d 4h', '3h 12m', '5m 2s', '42s'."""
    s = int(seconds)
    if s >= 86400:
        return f"{s // 86400}d {(s % 86400) // 3600}h"
    if s >= 3600:
        return f"{s // 3600}h {(s % 3600) // 60}m"
    if s >= 60:
        return f"{s // 60}m {s % 60}s"
    return f"{s}s"


_SPARK = "▁▂▃▄▅▆▇█"


def sparkline(values, max_value=None):
    """Return a unicode sparkline string for a list of numbers.

    max_value, if given, normalizes against a caller-supplied ceiling
    instead of this series' own max (e.g. to keep multiple series
    comparable to one another).
    """
    if not values:
        return ""
    mx = max_value if max_value and max_value > 0 else max(values)
    if mx <= 0:
        return _SPARK[0] * len(values)
    out = []
    for v in values:
        if v <= 0:
            out.append(_SPARK[0])
        else:
            idx = int(round(v / mx * (len(_SPARK) - 1)))
            out.append(_SPARK[max(1, idx)])
    return "".join(out)


def short_date(ts):
    """'2026-07-04T00:00:00Z' -> '07-04'."""
    if not ts:
        return ""
    try:
        return datetime.fromisoformat(ts.replace("Z", "+00:00")).strftime("%m-%d")
    except (ValueError, TypeError):
        return ts[5:10] if len(ts) >= 10 else ts
