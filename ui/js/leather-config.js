/**
 * leather-config.js — Config tab: formatted key/value display of /config response.
 * Depends on: leather-common.js
 */

'use strict';

// ── Section groupings ─────────────────────────────────────────────────────────

const CONFIG_SECTIONS = [
  {
    title: 'Scheduling',
    keys:  ['scheduler_tick', 'max_concurrent_jobs'],
  },
  {
    title: 'Token Budget',
    keys:  ['max_tokens', 'completion_reserve', 'summarize_threshold'],
  },
  {
    title: 'LLM',
    keys:  ['llm_endpoint', 'llm_timeout', 'model', 'temperature'],
  },
  {
    title: 'Logging',
    keys:  ['log_level', 'log_format'],
  },
  {
    title: 'API',
    keys:  ['api_addr'],
  },
  {
    title: 'Paths',
    keys:  ['agent_dir'],
  },
];

// ── Renderer ──────────────────────────────────────────────────────────────────

function _renderConfig(cfg) {
  const el = document.getElementById('config-display');
  if (!el) return;
  if (!cfg || Object.keys(cfg).length === 0) {
    el.innerHTML = '<p class="empty">No config data available.</p>';
    return;
  }

  // Track which keys have been rendered so we can show ungrouped remainder.
  const rendered = new Set();
  let html = '';

  CONFIG_SECTIONS.forEach(section => {
    const rows = section.keys
      .filter(k => cfg[k] !== undefined)
      .map(k => {
        rendered.add(k);
        return _row(k, cfg[k]);
      }).join('');
    if (!rows) return;
    html += `<div class="config-section">
      <div class="config-section-title">${ltEscapeHtml(section.title)}</div>
      ${rows}
    </div>`;
  });

  // Catch-all: any keys not in a section
  const remainder = Object.keys(cfg).filter(k => !rendered.has(k));
  if (remainder.length > 0) {
    html += `<div class="config-section">
      <div class="config-section-title">Other</div>
      ${remainder.map(k => _row(k, cfg[k])).join('')}
    </div>`;
  }

  el.innerHTML = html;
}

function _row(key, value) {
  const displayVal = value === null || value === undefined
    ? '<span style="color:var(--muted)">null</span>'
    : ltEscapeHtml(String(value));
  return `<div class="config-row">
    <div class="config-key">${ltEscapeHtml(key)}</div>
    <div class="config-value">${displayVal}</div>
  </div>`;
}

// ── Data update handler ───────────────────────────────────────────────────────

document.addEventListener('ltdataupdate', e => {
  const panel = document.getElementById('tab-config');
  if (panel && panel.classList.contains('active')) {
    _renderConfig(e.detail.config);
  } else {
    // Cache latest config so it renders correctly when the tab is opened.
    _pendingConfig = e.detail.config;
  }
});

// Render when the Config tab becomes active (tab-btn click sets active class
// before dispatching resize; we hook the tab buttons directly).
let _pendingConfig = null;
document.querySelectorAll('.tab-btn').forEach(btn => {
  if (btn.dataset.tab === 'config') {
    btn.addEventListener('click', () => {
      if (_pendingConfig) { _renderConfig(_pendingConfig); _pendingConfig = null; }
    });
  }
});
