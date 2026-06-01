/**
 * leather-overview.js — Overview tab: stat cards, agents table, recent runs table.
 * Depends on: leather-common.js, leather-api.js
 */

'use strict';

// ── Recent runs table ─────────────────────────────────────────────────────────

function _updateRecentRunsTable(data) {
  const wrap = document.getElementById('recent-runs-wrap');
  if (!wrap) return;

  const history   = (data.history || []).slice(0, 20);
  const agentData = (data.metrics && data.metrics.agents) ? data.metrics.agents : {};
  const agentNames = Object.keys(agentData);

  if (history.length === 0) {
    wrap.innerHTML = '<p class="empty">No runs recorded yet. Runs will appear here after agents execute.</p>';
    return;
  }

  const rows = history.map(r => {
    const color     = ltAgentColor(r.agent_name, agentNames);
    const lf        = (agentData[r.agent_name] && agentData[r.agent_name].lifecycle_file) || '';
    const lifecycle = lf.replace(/\.lifecycle\.yaml$/, '');
    const instance  = r.agent_name;
    const nameHtml  = (lifecycle && instance !== lifecycle)
      ? `<span style="color:var(--muted);font-size:10px">${ltEscapeHtml(lifecycle)} /</span> ${ltEscapeHtml(instance)}`
      : ltEscapeHtml(instance);

    return `<tr>
      <td>
        <span class="run-stripe-bar" style="background:${color}"></span>
        <span class="mono" style="font-size:12px">${ltFormatTs(r.time.start_ts).slice(11)}</span>
        <span style="color:var(--muted);font-size:10px;margin-left:6px">${ltTimeAgo(r.time.start_ts)}</span>
      </td>
      <td class="mono" style="color:${color}">${nameHtml}</td>
      <td>${ltFormatDuration(r.time.duration_ms)}</td>
      <td>${ltStatusBadge(r.status)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.prompt)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.response)}</td>
      <td class="mono">${ltFormatTokens(r.tokens.total)}</td>
    </tr>`;
  }).join('');

  wrap.innerHTML = `<table class="lt-table">
    <thead><tr>
      <th>Started At</th><th>Agent</th><th>Duration</th><th>Status</th>
      <th>\u2191 Prompt</th><th>\u2193 Compl</th><th>Total</th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>`;
}

// ── Stat cards ────────────────────────────────────────────────────────────────

function _updateStatCards(data) {
  const { status, metrics } = data;
  const sums = (metrics && metrics.agents) ? metrics.agents : {};

  const upEl = document.getElementById('sc-uptime');
  if (upEl && status) upEl.textContent = ltFormatUptime(status.uptime_seconds);

  const verEl = document.getElementById('sc-version');
  const comEl = document.getElementById('sc-commit');
  if (verEl && status) verEl.textContent = status.version || '\u2013';
  if (comEl && status) comEl.textContent = status.commit ? status.commit.slice(0, 7) : '';

  let totalRuns = 0, totalTokens = 0, totalErrors = 0;
  for (const ag of Object.values(sums)) {
    totalRuns   += ag.run_count   || 0;
    totalTokens += (ag.total_prompt_tokens || 0) + (ag.total_completion_tokens || 0);
    totalErrors += ag.error_count || 0;
  }

  const runsEl = document.getElementById('sc-runs');
  if (runsEl) runsEl.textContent = String(totalRuns);

  const tokEl = document.getElementById('sc-tokens');
  if (tokEl) tokEl.textContent = ltFormatTokens(totalTokens);

  const errEl = document.getElementById('sc-errrate');
  if (errEl) {
    const rate = totalRuns > 0 ? ((totalErrors / totalRuns) * 100).toFixed(1) + '%' : '0%';
    errEl.textContent = rate;
    errEl.style.color = totalErrors > 0 ? 'var(--red)' : 'var(--green)';
  }
}

// ── Agents table ──────────────────────────────────────────────────────────────

function _updateAgentsTable(data) {
  const tbody = document.getElementById('agents-table-body');
  if (!tbody) return;

  const { jobs, metrics } = data;
  const jobList = jobs || [];
  const sums    = (metrics && metrics.agents) ? metrics.agents : {};

  if (jobList.length === 0) {
    tbody.innerHTML = '<tr><td colspan="7" class="empty">No agents registered.</td></tr>';
    return;
  }

  tbody.innerHTML = jobList.map(job => {
    const ag = sums[job.agent_name] || {};
    const avgTokens = ag.run_count
      ? Math.round(((ag.total_prompt_tokens || 0) + (ag.total_completion_tokens || 0)) / ag.run_count)
      : 0;
    const lastRunDisplay = job.last_run ? ltTimeAgo(job.last_run) : '\u2013';
    const nextRunDisplay = job.next_run ? ltFormatTs(job.next_run).slice(11) : '\u2013';
    return `<tr>
      <td class="mono">${ltEscapeHtml(job.agent_name)}</td>
      <td>${ltStatusBadge(job.status)}</td>
      <td class="mono">${lastRunDisplay}</td>
      <td class="mono">${nextRunDisplay}</td>
      <td>${ag.run_count != null ? ag.run_count : (job.run_count || 0)}</td>
      <td style="color:${(ag.error_count || 0) > 0 ? 'var(--red)' : 'var(--muted)'}">${ag.error_count || 0}</td>
      <td>${ltFormatTokens(avgTokens)}</td>
    </tr>`;
  }).join('');
}

// ── Data update handler ───────────────────────────────────────────────────────

document.addEventListener('ltdataupdate', e => {
  const data = e.detail;
  _updateStatCards(data);
  _updateAgentsTable(data);
  _updateRecentRunsTable(data);
});
