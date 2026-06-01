/**
 * leather-api.js — endpoint fetch functions, auto-refresh poller, data cache.
 * Depends on: leather-common.js
 */

'use strict';

// ── API fetch functions ───────────────────────────────────────────────────────

const LtApi = {
  /** Read the base URL from the #server-url input. */
  base() {
    const el = document.getElementById('server-url');
    return el ? el.value.replace(/\/$/, '') : 'http://127.0.0.1:7749';
  },

  fetchStatus()   { return ltFetch('/status',   this.base()); },
  fetchConfig()   { return ltFetch('/config',   this.base()); },
  fetchJobs()     { return ltFetch('/jobs',     this.base()); },
  fetchMetrics()  { return ltFetch('/metrics',  this.base()); },
  fetchHistory()  { return ltFetch('/history',  this.base()); },
  fetchSnapshot() { return ltFetch('/snapshot', this.base()); },

  /** Send a replay-live control command (pause / resume / speed). */
  replayControl(action, speed) {
    const base = this.base();
    let url = `${base}/replay/control?action=${encodeURIComponent(action)}`;
    if (speed != null) url += `&speed=${encodeURIComponent(speed)}`;
    return fetch(url, { cache: 'no-store' }).then(r => r.json());
  },

  /** Fetch all endpoints in parallel and return a combined result object. */
  async fetchAll() {
    const [status, config, jobs, metrics, history] = await Promise.all([
      this.fetchStatus(),
      this.fetchConfig(),
      this.fetchJobs(),
      this.fetchMetrics(),
      this.fetchHistory(),
    ]);
    return { status, config, jobs, metrics, history };
  },
};

// ── Poller ────────────────────────────────────────────────────────────────────

const LtPoller = {
  _timer:       null,
  _lastFetch:   0,
  _tickTimer:   null,
  _urlInput:    null,
  _selectEl:    null,
  _nowBtn:      null,
  _lastUpdEl:   null,
  _connDot:     null,

  /**
   * Initialise the poller.  Call once after DOM is ready.
   * @param {HTMLInputElement}  urlInput  - #server-url
   * @param {HTMLSelectElement} selectEl  - #refresh-select
   * @param {HTMLButtonElement} nowBtn    - #refresh-now
   * @param {HTMLElement}       lastUpdEl - #last-updated
   * @param {HTMLElement}       connDot   - #conn-dot
   */
  init(urlInput, selectEl, nowBtn, lastUpdEl, connDot) {
    this._urlInput  = urlInput;
    this._selectEl  = selectEl;
    this._nowBtn    = nowBtn;
    this._lastUpdEl = lastUpdEl;
    this._connDot   = connDot;

    selectEl.addEventListener('change', () => this._restart());
    nowBtn.addEventListener('click',    () => this._fetch());
    urlInput.addEventListener('change', () => this._fetch());

    // Update "N sec ago" badge every second.
    this._tickTimer = setInterval(() => this._tick(), 1000);

    this._restart();
  },

  _restart() {
    if (this._timer) { clearInterval(this._timer); this._timer = null; }
    const ms = parseInt(this._selectEl.value, 10);
    if (ms > 0) {
      this._timer = setInterval(() => this._fetch(), ms);
    }
    this._fetch();
  },

  async _fetch() {
    try {
      const data = await LtApi.fetchAll();
      this._lastFetch = Date.now();
      this._connDot.className = 'ok';
      document.dispatchEvent(new CustomEvent('ltdataupdate', { detail: data }));
    } catch (err) {
      this._connDot.className = 'err';
      console.warn('leather API error:', err.message);
    }
  },

  _tick() {
    if (!this._lastFetch) { this._lastUpdEl.textContent = '–'; return; }
    const secs = Math.floor((Date.now() - this._lastFetch) / 1000);
    this._lastUpdEl.textContent = secs < 2 ? 'just now' : `${secs}s ago`;
  },
};

// ── Client-side data cache + snapshot builder ─────────────────────────────────

/**
 * LtDataCache — listens for ltdataupdate events and builds a snapshot JSON
 * from whatever data the client currently has in memory.  Works in both live
 * (API-polling) and local-file modes, so snapshot download never requires a
 * reachable server.
 */
const LtDataCache = (() => {
  let _last = null;

  document.addEventListener('ltdataupdate', e => { _last = e.detail || null; });

  /**
   * Build a snapshotResponse-shaped object from the last cached data.
   * Returns null if no data has been received yet.
   *
   * Shape matches the server's snapshotResponse struct:
   *   { version, commit, captured_at, config, jobs, metrics, history }
   * where metrics is map<agentName, agentMetricSummary>.
   */
  function buildSnapshot() {
    if (!_last) return null;
    const { status, config, jobs, metrics, history } = _last;
    return {
      version:     (status && status.version) || 'local',
      commit:      (status && status.commit)  || '',
      captured_at: Math.floor(Date.now() / 1000),
      config:      config   || {},
      jobs:        jobs     || [],
      // metrics.agents is the per-agent map the server sends as Metrics
      metrics:     (metrics && metrics.agents) || {},
      history:     history  || [],
    };
  }

  return { buildSnapshot };
})();
