/**
 * leather-timeline.js — Timeline tab: D3 Gantt chart of agent run history.
 * Keyboard: ↑ ↓ select lane, ← → move between runs in lane, Escape deselects.
 * Depends on: leather-common.js, D3 global (d3)
 */

'use strict';

// ── State ─────────────────────────────────────────────────────────────────────

let _tlData        = { history: [], jobs: [], agentNames: [] };
let _customDomain  = null;   // {start: Date, end: Date} or null
let _focusedLaneIdx = -1;   // index into agentNames; -1 = none
let _focusedRunIdx  = -1;   // index into sorted lane runs; -1 = none

// ── Info strip ───────────────────────────────────────────────────────────────
// Updates #timeline-info fixed below the chart; replaces floating tooltip.

let _chartW    = 0;    // chart pixel width, updated each draw; used by drag math
let _dragStart = null; // { x, domainMs: {start, end}, committed } or null

const _infoEl = document.getElementById('timeline-info');

function _setInfo(r) {
  if (!_infoEl) return;
  const statusStr = r.status === 'error'
    ? (r.error ? `error: ${r.error.slice(0, 80)}` : 'error')
    : r.status;
  _infoEl.textContent =
    `${r.agent_name}  ·  ${ltFormatTs(r.time.start_ts)}  ·  ${ltFormatDuration(r.time.duration_ms)}` +
    `  ·  ↑${ltFormatTokens(r.tokens.prompt)} ↓${ltFormatTokens(r.tokens.response)} =${ltFormatTokens(r.tokens.total)}` +
    `  ·  ${statusStr}`;
  _infoEl.style.color = r.status === 'error' ? 'var(--red)' : 'var(--muted)';
}
function _clearInfo() {
  if (!_infoEl || _focusedRunIdx >= 0) return;
  _infoEl.textContent = '';
  _infoEl.style.color = '';
}

// ── Time range helpers ────────────────────────────────────────────────────────

function _getWindowRange() {
  if (_customDomain) return { start: _customDomain.start, end: _customDomain.end };
  const windowSecs = parseInt(document.getElementById('timeline-window').value, 10);
  const nowMs = Date.now();
  return windowSecs > 0
    ? { start: new Date(nowMs - windowSecs * 1000), end: new Date(nowMs) }
    : null; // null → use data extent
}

// Shift the current view by factor × window size (-0.25 = 25% left, +0.25 = right).
function _shiftDomain(factor) {
  const range = _getWindowRange();
  const nowMs = Date.now();
  let startMs, endMs;
  if (range) {
    const spanMs = range.end - range.start;
    startMs = range.start.getTime() + factor * spanMs;
    endMs   = range.end.getTime()   + factor * spanMs;
  } else {
    // Fall back to default 1h window centred on now.
    const windowSecs = parseInt(document.getElementById('timeline-window').value, 10) || 3600;
    const spanMs = windowSecs * 1000;
    startMs = nowMs - spanMs + factor * spanMs;
    endMs   = nowMs           + factor * spanMs;
  }
  _customDomain = { start: new Date(startMs), end: new Date(endMs) };
  _drawTimeline();
}

// Move keyboard focus up/down across lanes.
function _moveFocus(delta) {
  const n = _tlData.agentNames.length;
  if (n === 0) return;
  _focusedLaneIdx = Math.max(0, Math.min(n - 1, _focusedLaneIdx + delta));
  _focusedRunIdx  = -1;   // clear run selection when changing lanes
  _drawTimeline();
}

// Returns runs for the focused lane visible in the current window, sorted by time.
function _laneRuns() {
  if (_focusedLaneIdx < 0) return [];
  const name   = _tlData.agentNames[_focusedLaneIdx];
  const range  = _getWindowRange();
  const nowSec = Math.floor(Date.now() / 1000);
  const xMin   = range ? range.start.getTime() : -Infinity;
  const xMax   = range ? range.end.getTime()   : Infinity;
  return _tlData.history
    .filter(r => {
      if (r.agent_name !== name) return false;
      const startMs = r.time.start_ts * 1000;
      const endMs   = startMs + (r.time.duration_ms || 0);
      return endMs >= xMin && startMs <= xMax;
    })
    .sort((a, b) => a.time.start_ts - b.time.start_ts);
}

// Move focus to prev/next run in the focused lane.
function _moveTick(delta) {
  if (_focusedLaneIdx < 0) return;
  const runs = _laneRuns();
  if (runs.length === 0) return;
  const next = _focusedRunIdx < 0
    ? (delta > 0 ? 0 : runs.length - 1)
    : Math.max(0, Math.min(runs.length - 1, _focusedRunIdx + delta));
  _focusedRunIdx = next;
  _drawTimeline();
}

// ── Draw ──────────────────────────────────────────────────────────────────────

function _drawTimeline() {
  const wrap = document.getElementById('timeline-svg-wrap');
  const svg  = d3.select('#timeline-svg');
  svg.selectAll('*').remove();

  const { history, jobs, agentNames } = _tlData;
  if (!history || history.length === 0) {
    svg.attr('height', 60)
       .append('text')
       .attr('x', 20).attr('y', 36)
       .attr('fill', '#7d8590').attr('font-size', 13)
       .text('No run history yet. Runs will appear here after agents execute.');
    return;
  }

  // ── Time domain ─────────────────────────────────────────────────
  const range      = _getWindowRange();
  const nowSec     = Math.floor(Date.now() / 1000);
  const dataMinMs  = d3.min(history, r => r.time.start_ts) * 1000;
  const xMin       = range ? range.start : new Date(dataMinMs);
  const xMax       = range ? range.end   : new Date(nowSec * 1000);

  const filtered = history.filter(r => {
    const startMs = r.time.start_ts * 1000;
    const endMs   = startMs + (r.time.duration_ms || 0);
    return endMs >= xMin.getTime() && startMs <= xMax.getTime();
  });

  if (filtered.length === 0) {
    svg.attr('height', 60)
       .append('text').attr('x', 20).attr('y', 36)
       .attr('fill', '#7d8590').attr('font-size', 13)
       .text('No runs in the selected time window. Use \u2190 to pan back, or change the window.');
    return;
  }

  // ── Dimensions ─────────────────────────────────────────────────
  const totalW = wrap.clientWidth - 24 || 900;
  const margin = { top: 20, right: 20, bottom: 36, left: 150 };
  const rowH   = 36, rowPad = 8;
  const chartW = totalW - margin.left - margin.right;
  _chartW = chartW;
  const chartH = agentNames.length * (rowH + rowPad);
  const totalH = chartH + margin.top + margin.bottom;

  svg.attr('width', totalW).attr('height', totalH);

  const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`);

  // ── Scales ─────────────────────────────────────────────────────
  const xScale = d3.scaleTime().domain([xMin, xMax]).range([0, chartW]);
  const yScale = d3.scaleBand()
    .domain(agentNames)
    .range([0, chartH])
    .padding(0.2);

  // ── Lane focus highlight ────────────────────────────────────────
  if (_focusedLaneIdx >= 0 && _focusedLaneIdx < agentNames.length) {
    const focusedName = agentNames[_focusedLaneIdx];
    const fy = yScale(focusedName);
    if (fy != null) {
      g.append('rect')
        .attr('x', 0).attr('y', fy - rowPad / 2)
        .attr('width', chartW)
        .attr('height', yScale.bandwidth() + rowPad)
        .attr('fill', 'rgba(88,166,255,.07)')
        .attr('rx', 3);
    }
  }

  // ── Axes ───────────────────────────────────────────────────────
  const xAxis = d3.axisBottom(xScale)
    .ticks(Math.min(8, Math.floor(chartW / 90)))
    .tickSize(-chartH)
    .tickFormat(d3.timeFormat('%H:%M'));

  g.append('g')
    .attr('class', 'x-axis')
    .attr('transform', `translate(0,${chartH})`)
    .call(xAxis)
    .call(ax => {
      ax.select('.domain').remove();
      ax.selectAll('.tick line').attr('stroke', '#30363d').attr('stroke-dasharray', '3,3');
      ax.selectAll('.tick text').attr('fill', '#7d8590').attr('font-size', 11);
    });

  const yAxis = d3.axisLeft(yScale).tickSize(0);
  g.append('g')
    .attr('class', 'y-axis')
    .call(yAxis)
    .call(ax => {
      ax.select('.domain').remove();
      ax.selectAll('.tick text')
        .attr('fill', d => {
          const idx = agentNames.indexOf(d);
          return idx === _focusedLaneIdx ? '#e6edf3' : '#7d8590';
        })
        .attr('font-size', 12)
        .attr('font-family', 'SFMono-Regular, Consolas, monospace')
        .attr('x', -8);
    });

  // ── Upcoming run markers ────────────────────────────────────────
  jobs.forEach(job => {
    if (!job.next_run) return;
    const nx = new Date(job.next_run * 1000);
    if (nx < xMin || nx > xMax) return;
    const px = xScale(nx);
    const py = yScale(job.agent_name);
    if (py == null) return;
    g.append('rect')
      .attr('x', px - 1).attr('y', py)
      .attr('width', 3).attr('height', yScale.bandwidth())
      .attr('fill', 'none')
      .attr('stroke', ltAgentColor(job.agent_name, agentNames))
      .attr('stroke-width', 1.5)
      .attr('stroke-dasharray', '3,2')
      .attr('opacity', 0.5);
  });

  // ── Run rectangles + invisible wide hit rects ──────────────────
  const minVisW = 3;   // minimum visible bar width
  const minHitW = 12;  // minimum hit target width (tooltip)
  const focusedRunData = (_focusedLaneIdx >= 0 && _focusedRunIdx >= 0)
    ? _laneRuns()[_focusedRunIdx]
    : null;

  filtered.forEach(r => {
    const x0 = xScale(new Date(r.time.start_ts * 1000));
    const x1 = xScale(new Date((r.time.start_ts + (r.time.duration_ms || 0) / 1000) * 1000));
    const visW = Math.max(minVisW, x1 - x0);
    const hitW = Math.max(minHitW, x1 - x0);
    const hitX = x0 - (hitW - visW) / 2;
    const py   = yScale(r.agent_name);
    if (py == null) return;
    const fill = r.status === 'error' ? '#f85149' : ltAgentColor(r.agent_name, agentNames);

    // Visible bar
    const isFocused = focusedRunData && r.time.start_ts === focusedRunData.time.start_ts && r.agent_name === focusedRunData.agent_name;
    g.append('rect')
      .attr('x', x0).attr('y', py)
      .attr('width', visW).attr('height', yScale.bandwidth())
      .attr('fill', fill).attr('opacity', isFocused ? 1 : 0.85).attr('rx', 3)
      .style('pointer-events', 'none');

    // Focus ring on keyboard-selected run
    if (isFocused) {
      g.append('rect')
        .attr('x', x0 - 2).attr('y', py - 2)
        .attr('width', visW + 4).attr('height', yScale.bandwidth() + 4)
        .attr('fill', 'none')
        .attr('stroke', '#e6edf3').attr('stroke-width', 1.5).attr('rx', 4)
        .style('pointer-events', 'none');
    }

    // Invisible wider hit rect
    g.append('rect')
      .attr('x', hitX).attr('y', py)
      .attr('width', hitW).attr('height', yScale.bandwidth())
      .attr('fill', 'transparent')
      .style('cursor', 'crosshair')
      .on('mouseenter', () => _setInfo(r))
      .on('mouseleave', () => _clearInfo());
  });

  // ── Info strip for keyboard-focused run ─────────────────────────
  if (focusedRunData) { _setInfo(focusedRunData); }
}

// ── Keyboard navigation ───────────────────────────────────────────────────────

(function _initKeyNav() {
  const el = document.getElementById('timeline-svg-wrap');
  if (!el) return;

  // Clicking anywhere inside the SVG wrap (including SVG children) must focus it.
  el.addEventListener('mousedown', () => el.focus());

  el.addEventListener('keydown', e => {
    switch (e.key) {
      case 'ArrowLeft':  _moveTick(-1);  e.preventDefault(); break;
      case 'ArrowRight': _moveTick( 1);  e.preventDefault(); break;
      case 'ArrowUp':    _moveFocus(-1); e.preventDefault(); break;
      case 'ArrowDown':  _moveFocus( 1); e.preventDefault(); break;
      case 'Escape':
        _focusedLaneIdx = -1;
        _focusedRunIdx  = -1;
        if (_infoEl) { _infoEl.textContent = ''; _infoEl.style.color = ''; }
        _drawTimeline();
        e.preventDefault();
        break;
    }
  });
})();

// ── Window resize ─────────────────────────────────────────────────────────────

window.addEventListener('resize', () => {
  if (document.getElementById('tab-timeline')?.classList.contains('active')) {
    _drawTimeline();
  }
});

// ── Update from data ──────────────────────────────────────────────────────────

document.addEventListener('ltdataupdate', e => {
  const data = e.detail;
  _tlData = {
    history:    data.history || [],
    jobs:       data.jobs    || [],
    agentNames: Object.keys((data.metrics && data.metrics.agents) || {}),
  };
  if (_focusedLaneIdx >= _tlData.agentNames.length) _focusedLaneIdx = -1;

  if (document.getElementById('tab-timeline')?.classList.contains('active')) {
    _drawTimeline();
  }
});

// ── Tab change (redraw on switch, auto-focus for keyboard nav) ────────────────

document.addEventListener('lttabchange', e => {
  if (e.detail.tab === 'timeline') {
    _drawTimeline();
    // Delay focus slightly so the tab panel is visible before focus is set.
    setTimeout(() => document.getElementById('timeline-svg-wrap')?.focus(), 60);
  }
});
// ── Click-drag pan ───────────────────────────────────────────────────────────────
// Mousedown + drag shifts _customDomain by a pixel-proportional ms offset.

(function _initDragPan() {
  const svgWrap = document.getElementById('timeline-svg-wrap');
  if (!svgWrap) return;

  svgWrap.addEventListener('mousedown', e => {
    if (e.button !== 0) return;
    let range = _getWindowRange();
    if (!range) {
      // "All" mode: synthesize domain from data extent so drag still works.
      if (!_tlData.history.length) return;
      const minMs = d3.min(_tlData.history, r => r.time.start_ts) * 1000;
      range = { start: new Date(minMs), end: new Date(Date.now()) };
    }
    if (!_chartW) return;
    _dragStart = {
      x:        e.clientX,
      domainMs: { start: range.start.getTime(), end: range.end.getTime() },
      committed: false,
    };
    e.preventDefault();
  });

  document.addEventListener('mousemove', e => {
    if (!_dragStart || !_chartW) return;
    const deltaX = e.clientX - _dragStart.x;
    if (!_dragStart.committed) {
      if (Math.abs(deltaX) < 4) return; // ignore tiny jitter / plain clicks
      _dragStart.committed = true;
      svgWrap.style.cursor = 'grabbing';
    }
    const spanMs  = _dragStart.domainMs.end - _dragStart.domainMs.start;
    const deltaMs = -(deltaX / _chartW) * spanMs;
    _customDomain = {
      start: new Date(_dragStart.domainMs.start + deltaMs),
      end:   new Date(_dragStart.domainMs.end   + deltaMs),
    };
    _drawTimeline();
  });

  document.addEventListener('mouseup', () => {
    if (_dragStart) {
      _dragStart = null;
      svgWrap.style.cursor = '';
    }
  });
})();
// ── Window selector ───────────────────────────────────────────────────────────

document.getElementById('timeline-window')?.addEventListener('change', () => {
  _customDomain   = null;   // reset any pan state
  _focusedLaneIdx = -1;
  _drawTimeline();
});
