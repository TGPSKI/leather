/**
 * leather-agents.js — Agents tab: per-agent selector chips (grouped by lifecycle),
 * run history table with expandable detail cards, info cards.
 * Depends on: leather-common.js, leather-api.js
 */

'use strict';

// ── State ─────────────────────────────────────────────────────────────────────

let _agentsData      = {};    // metrics.agents map  (keyed by agent name)
let _jobsData        = [];    // /status jobs array
let _selectedAgent   = null;
let _agentNames      = [];
let _configExpanded  = false; // whether the config panel is expanded

// ── Chip rendering (grouped by lifecycle file) ────────────────────────────────

function _renderChips() {
  const container = document.getElementById('agent-chips');
  if (!container) return;
  if (_agentNames.length === 0) {
    container.innerHTML = '<span style="color:var(--muted);font-size:12px">No agents found.</span>';
    return;
  }

  // Group agents by lifecycle basename (stripped of .lifecycle.yaml extension).
  const groups = {};   // lifecycle label → [agent names]
  for (const name of _agentNames) {
    const lf    = (_agentsData[name] && _agentsData[name].lifecycle_file) || '';
    const label = lf.replace(/\.lifecycle\.yaml$/, '') || '\u2014';
    if (!groups[label]) groups[label] = [];
    groups[label].push(name);
  }

  const html = Object.keys(groups).sort().map(label => {
    const chips = groups[label].map(name =>
      `<button class="agent-chip${name === _selectedAgent ? ' active' : ''}"
               data-agent="${ltEscapeHtml(name)}">${ltEscapeHtml(name)}</button>`
    ).join('');
    return `<div class="chip-group">
      <span class="chip-group-label">${ltEscapeHtml(label)}</span>${chips}
    </div>`;
  }).join('');

  container.innerHTML = html;
  container.querySelectorAll('.agent-chip').forEach(chip => {
    chip.addEventListener('click', () => {
      _selectedAgent = chip.dataset.agent;
      _renderChips();
      _renderDetail();
    });
  });
}

// ── Detail panel ──────────────────────────────────────────────────────────────

// Handles expandable run-row clicks and config-panel toggle via event delegation on #agent-detail.
function _initExpandHandler() {
  const panel = document.getElementById('agent-detail');
  if (!panel || panel.dataset.expandInit) return;
  panel.dataset.expandInit = '1';
  panel.addEventListener('click', e => {
    // Config panel toggle.
    if (e.target.closest('#config-toggle-btn')) {
      _configExpanded = !_configExpanded;
      _renderDetail();
      return;
    }
    // Run history row expand.
    const row = e.target.closest('.run-row');
    if (!row) return;
    const detailRow = row.nextElementSibling;
    if (!detailRow || !detailRow.classList.contains('run-detail-row')) return;
    const visible = detailRow.style.display !== 'none';
    detailRow.style.display = visible ? 'none' : '';
    row.classList.toggle('expanded', !visible);
  });
}

function _renderDetail() {
  const panel = document.getElementById('agent-detail');
  if (!panel) return;
  if (!_selectedAgent || !_agentsData[_selectedAgent]) {
    panel.innerHTML = '<p class="empty">Select an agent above.</p>';
    return;
  }

  // Snapshot which run rows are currently expanded so we can restore them.
  const expandedTs = new Set();
  panel.querySelectorAll('.run-row.expanded').forEach(row => {
    if (row.dataset.runTs) expandedTs.add(row.dataset.runTs);
  });

  const ag   = _agentsData[_selectedAgent];
  const job  = _jobsData.find(j => j.agent_name === _selectedAgent) || {};
  const runs = ag.recent_runs || [];

  // ── Config card (collapsible) ─────────────────────────────────────
  const nextRun = job.next_run ? ltFormatTs(job.next_run).slice(11) : '\u2013';
  const lastRun = job.last_run ? ltTimeAgo(job.last_run) : '\u2013';
  const lf      = (ag.lifecycle_file || '').replace(/\.lifecycle\.yaml$/, '') || '\u2013';

  // Build expanded config content.
  const tagsHtml = (ag.tags && ag.tags.length)
    ? ag.tags.map(t => `<span style="display:inline-block;background:var(--border);color:var(--fg);border-radius:3px;padding:1px 7px;font-size:11px;margin:2px 4px 2px 0">${ltEscapeHtml(t)}</span>`).join('')
    : '<span style="color:var(--muted)">\u2013</span>';
  const numericRows = [
    ag.max_tokens   ? `<div><span style="color:var(--muted)">max_tokens</span>&nbsp;&nbsp;${ag.max_tokens}</div>` : '',
    ag.temperature  ? `<div><span style="color:var(--muted)">temperature</span>&nbsp;&nbsp;${ag.temperature}</div>` : '',
    ag.timeout_ms   ? `<div><span style="color:var(--muted)">timeout</span>&nbsp;&nbsp;${ag.timeout_ms}ms</div>` : '',
  ].filter(Boolean).join('');
  const expandedHtml = _configExpanded ? `
    <div style="border-top:1px solid var(--border);margin-top:12px;padding-top:12px;display:grid;gap:12px">
      ${numericRows ? `<div style="font-size:12px;font-family:var(--font-mono);color:var(--fg)">${numericRows}</div>` : ''}
      ${(ag.tags && ag.tags.length) ? `<div><div style="color:var(--muted);font-size:11px;text-transform:uppercase;margin-bottom:4px">Tags</div>${tagsHtml}</div>` : ''}
      ${ag.system_prompt ? `<div><div style="color:var(--muted);font-size:11px;text-transform:uppercase;margin-bottom:4px">System Prompt</div>
        <pre style="margin:0;padding:10px;background:var(--bg);border:1px solid var(--border);border-radius:4px;font-size:12px;white-space:pre-wrap;word-break:break-word;max-height:200px;overflow-y:auto">${ltEscapeHtml(ag.system_prompt)}</pre></div>` : ''}
    </div>` : '';
  const toggleArrow = _configExpanded ? '\u25b2' : '\u25bc';
  const infoHtml = `
  <div class="panel" style="margin-bottom:8px;padding:12px 20px">
    <div style="display:flex;align-items:center;justify-content:space-between;gap:12px">
      <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:10px;flex:1">
        <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Schedule</div>
             <div style="font-size:13px;margin-top:4px;font-family:var(--font-mono)">${ltEscapeHtml(ag.schedule || '\u2013')}</div></div>
        <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Model</div>
             <div style="font-size:13px;margin-top:4px;font-family:var(--font-mono);word-break:break-all">${ltEscapeHtml(ag.model || '\u2013')}</div></div>
        <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Lifecycle</div>
             <div style="font-size:13px;margin-top:4px;font-family:var(--font-mono)">${ltEscapeHtml(lf)}</div></div>
      </div>
      <button id="config-toggle-btn" style="background:none;border:1px solid var(--border);color:var(--muted);border-radius:4px;padding:4px 10px;font-size:11px;cursor:pointer;white-space:nowrap;flex-shrink:0">${toggleArrow} ${_configExpanded ? 'hide' : 'details'}</button>
    </div>
    ${expandedHtml}
  </div>
  <div class="panel" style="margin-bottom:16px;padding:16px 20px">
    <div style="display:grid;grid-template-columns:repeat(auto-fill,minmax(140px,1fr));gap:12px">
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Runs</div>
           <div style="font-size:20px;font-weight:700">${ag.run_count || 0}</div></div>
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Errors</div>
           <div style="font-size:20px;font-weight:700;color:${(ag.error_count||0)>0?'var(--red)':'var(--green)'}">${ag.error_count || 0}</div></div>
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Avg Duration</div>
           <div style="font-size:20px;font-weight:700">${ltFormatDuration(Math.round(ag.avg_duration_ms || 0))}</div></div>
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Total Tokens</div>
           <div style="font-size:20px;font-weight:700">${ltFormatTokens((ag.total_prompt_tokens||0)+(ag.total_completion_tokens||0))}</div></div>
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Next Run</div>
           <div style="font-size:13px;margin-top:4px;font-family:var(--font-mono)">${nextRun}</div></div>
      <div><div style="color:var(--muted);font-size:11px;text-transform:uppercase">Last Run</div>
           <div style="font-size:13px;margin-top:4px;font-family:var(--font-mono)">${lastRun}</div></div>
    </div>
  </div>`;

  // ── Run history table with expandable rows ────────────────────────
  const color = ltAgentColor(_selectedAgent, _agentNames);
  const tableRows = runs.slice(0, 50).map(r => {
    const t0 = (r.turns && r.turns[0]) || {};
    const hasDetail = !!(t0.prompt || t0.response);
    const promptEsc  = ltEscapeHtml(t0.prompt   || '');
    const contentEsc = ltEscapeHtml(t0.response || '');
    const clickHint = hasDetail ? ' \u25bc' : '';

    const detailCard = hasDetail ? `
    <tr class="run-detail-row" style="display:none">
      <td colspan="7">
        <div class="run-detail-card">
          ${t0.prompt ? `<div class="run-detail-section">
            <div class="rds-label">User Prompt</div>
            <div class="rds-content">${promptEsc}</div>
          </div>` : ''}
          ${t0.response ? `<div class="run-detail-section">
            <div class="rds-label">Model Response</div>
            <div class="rds-content">${contentEsc}</div>
          </div>` : ''}
        </div>
      </td>
    </tr>` : '';

    return `<tr class="run-row" data-run-ts="${r.time.start_ts}" style="border-left:3px solid ${color}">
      <td class="mono" style="font-size:11px">${ltFormatTs(r.time.start_ts)}</td>
      <td>${ltFormatDuration(r.time.duration_ms)}</td>
      <td>${ltStatusBadge(r.status)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.prompt)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.response)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.total)}</td>
      <td style="color:${r.error?'var(--red)':'var(--muted)'};font-size:11px">${
        r.error ? ltEscapeHtml(r.error) : (hasDetail ? `<span style="opacity:.5">click to expand${clickHint}</span>` : '')
      }</td>
    </tr>${detailCard}`;
  }).join('');

  const tableHtml = `
  <div class="section-title" style="margin-top:20px">Run History (last ${Math.min(runs.length, 50)})</div>
  <div class="panel">
    <table class="lt-table">
      <thead><tr>
        <th>Started At</th><th>Duration</th><th>Status</th>
        <th>\u2191 Prompt</th><th>\u2193 Compl</th><th>Total</th><th>Info</th>
      </tr></thead>
      <tbody>${tableRows || '<tr><td colspan="7" class="empty">No runs yet.</td></tr>'}</tbody>
    </table>
  </div>`;

  panel.innerHTML = infoHtml + tableHtml;
  _initExpandHandler(); // attach delegation (idempotent)

  // Restore previously-expanded rows.
  if (expandedTs.size > 0) {
    panel.querySelectorAll('.run-row').forEach(row => {
      if (!expandedTs.has(row.dataset.runTs)) return;
      const detailRow = row.nextElementSibling;
      if (detailRow && detailRow.classList.contains('run-detail-row')) {
        detailRow.style.display = '';
        row.classList.add('expanded');
      }
    });
  }
}

// ── Data update handler ───────────────────────────────────────────────────────

document.addEventListener('ltdataupdate', e => {
  const data = e.detail;
  _agentsData = (data.metrics && data.metrics.agents) ? data.metrics.agents : {};
  _jobsData   = data.jobs || [];
  _agentNames = Object.keys(_agentsData).sort();

  // Auto-select first agent if none selected or current selection disappeared.
  if (_selectedAgent && !_agentsData[_selectedAgent]) _selectedAgent = null;
  if (!_selectedAgent && _agentNames.length > 0) _selectedAgent = _agentNames[0];

  _renderChips();

  // Only re-render detail when the tab is visible to avoid flicker.
  const panel = document.getElementById('tab-agents');
  if (panel && panel.classList.contains('active')) _renderDetail();
});

// Re-render detail when switching to the agents tab (fixes stale data).
document.addEventListener('lttabchange', e => {
  if (e.detail.tab === 'agents') {
    _renderChips();
    if (_selectedAgent) _renderDetail();
  }
});
