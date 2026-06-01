/**
 * leather-common.js — shared utilities, formatters, and theme constants.
 * Loaded first; all other leather-*.js files depend on this.
 */

'use strict';

// ── Formatters ────────────────────────────────────────────────────────────────

/** Format a Unix timestamp (seconds) as "YYYY-MM-DD HH:MM:SS". */
function ltFormatTs(unixSecs) {
  if (!unixSecs) return '–';
  const d = new Date(unixSecs * 1000);
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ` +
         `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** Format a Unix timestamp as a short relative string: "5s ago", "2m ago", "3h ago". */
function ltTimeAgo(unixSecs) {
  if (!unixSecs) return '–';
  const diff = Math.floor(Date.now() / 1000) - unixSecs;
  if (diff < 2)   return 'just now';
  if (diff < 60)  return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return `${Math.floor(diff / 3600)}h ago`;
}

/** Format a duration in ms as "1.2s", "845ms", "0ms". */
function ltFormatDuration(ms) {
  if (ms == null) return '–';
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

/** Format a token count as "1.2k" or "110". */
function ltFormatTokens(n) {
  if (n == null || n === 0) return '0';
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

/** Format an uptime in seconds as "2h 15m 03s". */
function ltFormatUptime(secs) {
  if (secs == null) return '–';
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  const pad = n => String(n).padStart(2, '0');
  if (h > 0) return `${h}h ${pad(m)}m ${pad(s)}s`;
  if (m > 0) return `${m}m ${pad(s)}s`;
  return `${s}s`;
}

/** Escape HTML special characters to prevent XSS. */
function ltEscapeHtml(s) {
  if (s == null) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/**
 * Return an HTML badge for a job status string.
 * @param {string} status - "success" | "error" | "running" | "pending" | "skipped"
 */
function ltStatusBadge(status) {
  const cls = {
    success: 'badge-success',
    error:   'badge-error',
    running: 'badge-running',
    pending: 'badge-pending',
    skipped: 'badge-skipped',
  }[status] || 'badge-pending';
  return `<span class="badge ${cls}">${ltEscapeHtml(status)}</span>`;
}

// ── Colors ────────────────────────────────────────────────────────────────────

/** Agent colour palette — cycles through if more agents than colours. */
const LT_AGENT_COLORS = [
  '#58a6ff', '#3fb950', '#ffa657', '#bc8cff',
  '#d29922', '#39d353', '#f85149', '#79c0ff',
];

/** Return a stable colour for an agent name. */
function ltAgentColor(name, allNames) {
  const idx = allNames.indexOf(name);
  return LT_AGENT_COLORS[(idx < 0 ? 0 : idx) % LT_AGENT_COLORS.length];
}

// ── Fetch helper ──────────────────────────────────────────────────────────────

/**
 * Fetch JSON from a leather API endpoint.
 * @param {string} path - e.g. "/status"
 * @param {string} base - base URL, e.g. "http://127.0.0.1:8080"
 * @returns {Promise<any>}
 */
function ltFetch(path, base) {
  return fetch(base + path, { cache: 'no-store' }).then(r => {
    if (!r.ok) throw new Error(`HTTP ${r.status} from ${path}`);
    return r.json();
  });
}
