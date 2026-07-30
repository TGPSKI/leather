"""Trailing-window helpers shared by the project TUIs. Stdlib only."""
from __future__ import annotations

from datetime import timedelta

TREND_THRESHOLD = 0.10  # ±10% -> ↑/↓


def trailing_hours(latest_dt, n):
    """(bucket_key, short_label) pairs for the n hours ending at latest_dt,
    oldest first. Keys use the hourly CSV wire format."""
    out = []
    for i in range(n - 1, -1, -1):
        dt = latest_dt - timedelta(hours=i)
        out.append((dt.strftime("%Y-%m-%dT%H:00:00Z"), dt.strftime("%H")))
    return out


def trailing_days(latest_dt, n):
    """(bucket_key, short_label) pairs for the n days ending at latest_dt,
    oldest first. Keys are %Y-%m-%d dates."""
    out = []
    for i in range(n - 1, -1, -1):
        d = (latest_dt - timedelta(days=i)).date()
        out.append((d.strftime("%Y-%m-%d"), d.strftime("%m-%d")))
    return out


def trend(buckets, metric="requests", threshold=TREND_THRESHOLD):
    """Compare first half vs second half of window -> ↑ / ↓ / →"""
    n = len(buckets)
    if n < 2:
        return "→"
    half = n // 2
    first = sum(b[metric] for b in buckets[:half])
    second = sum(b[metric] for b in buckets[half:])
    if first == 0:
        return "↑" if second > 0 else "→"
    delta = (second - first) / first
    if delta > threshold:
        return "↑"
    if delta < -threshold:
        return "↓"
    return "→"


def stack_cells(parts, total_cells):
    """Largest-remainder allocation of total_cells across parts."""
    s = sum(parts)
    if s <= 0 or total_cells <= 0:
        return [0] * len(parts)
    raw = [p * total_cells / s for p in parts]
    cells = [int(x) for x in raw]
    order = sorted(range(len(parts)), key=lambda i: raw[i] - cells[i],
                   reverse=True)
    for i in order[: total_cells - sum(cells)]:
        cells[i] += 1
    return cells
