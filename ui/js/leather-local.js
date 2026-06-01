/**
 * leather-local.js — offline / local-file mode.
 *
 * Lets the user drag-and-drop or browse for:
 *   jobs.json          → model.Job[]
 *   *.jsonl            → model.RunRecord[] (one per line)
 *   snapshot.json      → full snapshotResponse (all data in one file)
 *
 * Parses the files client-side, computes metrics, and dispatches
 * `ltdataupdate` so all other tabs update without a running server.
 * The poller is paused while local data is active.
 *
 * Depends on: leather-api.js, leather-common.js
 */

'use strict';

const LtLocal = (() => {
  // ── state ─────────────────────────────────────────────────────────────────
  /** @type {Array<{name: string, kind: string, data: any}>} */
  let _files = [];

  // ── DOM refs (set in init) ─────────────────────────────────────────────────
  let _dropZone, _fileInput, _folderInput, _fileList, _loadedCount, _hint;

  // ── public API ─────────────────────────────────────────────────────────────
  function init() {
    _dropZone    = document.getElementById('lt-drop-zone');
    _fileInput   = document.getElementById('lt-file-input');
    _folderInput = document.getElementById('lt-folder-input');
    _fileList    = document.getElementById('lt-file-list');
    _loadedCount = document.getElementById('lt-loaded-count');
    _hint        = document.getElementById('lt-hint');

    if (!_dropZone || !_fileInput) return;

    // Browse buttons
    document.getElementById('lt-btn-files').addEventListener('click', () => _fileInput.click());
    document.getElementById('lt-btn-folder').addEventListener('click', () => {
      if (_folderInput) _folderInput.click();
    });

    // File inputs
    _fileInput.addEventListener('change', () => {
      if (_fileInput.files && _fileInput.files.length) {
        _ingest(_fileInput.files);
        _fileInput.value = '';
      }
    });
    if (_folderInput) {
      _folderInput.addEventListener('change', () => {
        if (_folderInput.files && _folderInput.files.length) {
          _ingest(_folderInput.files);
          _folderInput.value = '';
        }
      });
    }

    // Drop zone
    _dropZone.addEventListener('click', () => _fileInput.click());
    _dropZone.addEventListener('dragover', e => {
      e.preventDefault();
      _dropZone.classList.add('drag-over');
    });
    _dropZone.addEventListener('dragleave', () => _dropZone.classList.remove('drag-over'));
    _dropZone.addEventListener('drop', e => {
      e.preventDefault();
      _dropZone.classList.remove('drag-over');
      if (e.dataTransfer && e.dataTransfer.files.length) _ingest(e.dataTransfer.files);
    });

    // Unload (toolbar button and banner clear button)
    document.getElementById('lt-btn-unload').addEventListener('click', _unloadAll);
    const bannerClear = document.getElementById('lt-banner-clear');
    if (bannerClear) bannerClear.addEventListener('click', _unloadAll);

    // File chip click → remove individual file
    _fileList.addEventListener('click', e => {
      const chip = e.target.closest('.file-chip');
      if (!chip) return;
      const idx = parseInt(chip.dataset.idx || '-1', 10);
      if (idx >= 0 && idx < _files.length) {
        _files.splice(idx, 1);
        _commit();
      }
    });

    _renderFileList();
    _updateCount();
  }

  // ── file ingestion ─────────────────────────────────────────────────────────
  async function _ingest(fileList) {
    const incoming = Array.from(fileList).filter(f => /\.(json|jsonl)$/i.test(f.name));
    if (!incoming.length) return;

    const parsed = await Promise.all(incoming.map(_parseFile));
    const valid = parsed.filter(Boolean);
    if (!valid.length) return;

    // Deduplicate by name: replace existing entry with same filename
    for (const entry of valid) {
      const i = _files.findIndex(f => f.name === entry.name);
      if (i >= 0) _files[i] = entry;
      else _files.push(entry);
    }

    _commit();
  }

  async function _parseFile(file) {
    const text = await _readText(file);
    const name = file.name;

    if (/\.jsonl$/i.test(name)) {
      const recs = _parseJSONL(text);
      return { name, kind: 'jsonl', data: recs };
    }

    if (/\.json$/i.test(name)) {
      let parsed;
      try { parsed = JSON.parse(text); } catch { return null; }

      // Snapshot: root object with history + version fields
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)
          && Array.isArray(parsed.history) && parsed.version !== undefined) {
        return { name, kind: 'snapshot', data: parsed };
      }

      // Jobs list: array with agent_name + last_run
      if (Array.isArray(parsed) && parsed.length
          && parsed[0] && parsed[0].agent_name !== undefined
          && parsed[0].last_run !== undefined) {
        return { name, kind: 'jobs', data: parsed };
      }

      return null;
    }

    return null;
  }

  function _parseJSONL(text) {
    return text.split('\n')
      .map(l => l.trim())
      .filter(l => l.length > 0)
      .map(l => { try { return JSON.parse(l); } catch { return null; } })
      .filter(Boolean);
  }

  function _readText(file) {
    return new Promise((resolve, reject) => {
      const r = new FileReader();
      r.onload = () => resolve(String(r.result || ''));
      r.onerror = reject;
      r.readAsText(file);
    });
  }

  // ── commit: rebuild UI + dispatch ──────────────────────────────────────────
  function _commit() {
    _renderFileList();
    _updateCount();

    if (_files.length === 0) {
      _unloadAll();
      return;
    }

    _pausePoller();
    _showBanner();

    const data = _buildData();
    document.dispatchEvent(new CustomEvent('ltdataupdate', { detail: data }));

    // Update hint text
    if (_hint) _hint.style.display = 'none';
  }

  // ── data assembly ──────────────────────────────────────────────────────────
  function _buildData() {
    let jobs = [];
    let history = [];
    let config = {};
    let snapshotAgents = {}; // agent config fields from snapshot metrics

    for (const f of _files) {
      if (f.kind === 'jobs') {
        jobs = jobs.concat(f.data);
      } else if (f.kind === 'jsonl') {
        history = history.concat(f.data);
      } else if (f.kind === 'snapshot') {
        if (Array.isArray(f.data.jobs)) jobs = jobs.concat(f.data.jobs);
        if (Array.isArray(f.data.history)) history = history.concat(f.data.history);
        if (f.data.config) config = f.data.config;
        // Capture agent config metadata (lifecycle_file, schedule, model, tags, etc.)
        // so the Agents tab matches live mode. Run stats are recomputed from history.
        if (f.data.metrics && typeof f.data.metrics === 'object') {
          Object.assign(snapshotAgents, f.data.metrics);
        }
      }
    }

    // Sort history descending
    history.sort((a, b) => (b.time && b.time.start_ts || 0) - (a.time && a.time.start_ts || 0));

    // Compute run stats from history, then overlay agent config from snapshot metrics.
    // Config fields (lifecycle_file, schedule, model, etc.) are not in run logs —
    // they must come from the snapshot's metrics map.
    const agentMap = _computeMetrics(history);
    for (const [name, meta] of Object.entries(snapshotAgents)) {
      if (!agentMap[name]) {
        // Agent is registered but has no history in the loaded files.
        agentMap[name] = {
          run_count: 0, error_count: 0,
          total_prompt_tokens: 0, total_completion_tokens: 0,
          avg_duration_ms: 0, recent_runs: [],
          lifecycle_file: '', schedule: '', model: '',
          system_prompt: '', user_prompt: '', tags: [],
        };
      }
      const a = agentMap[name];
      a.lifecycle_file = meta.lifecycle_file || '';
      a.schedule       = meta.schedule       || '';
      a.model          = meta.model          || '';
      a.system_prompt  = meta.system_prompt  || a.system_prompt;
      a.user_prompt    = meta.user_prompt    || a.user_prompt;
      a.tags           = meta.tags           || a.tags;
      if (meta.max_tokens)  a.max_tokens  = meta.max_tokens;
      if (meta.temperature) a.temperature = meta.temperature;
      if (meta.timeout_ms)  a.timeout_ms  = meta.timeout_ms;
    }

    const metrics = { agents: agentMap };

    const agentCount = jobs.length || Object.keys(metrics.agents).length;
    const firstTs = history.length ? (history[history.length - 1].time && history[history.length - 1].time.start_ts || 0) : 0;
    const lastTs  = history.length ? (history[0].time && history[0].time.start_ts || 0) : 0;

    const status = {
      version:             'local',
      commit:              '',
      started_at:          firstTs,  // kept for status compat
      uptime_seconds:      lastTs - firstTs,
      llm_endpoint:        config.llm_endpoint || '(local files)',
      agent_count:         agentCount,
      scheduler_tick:      config.scheduler_tick || '',
      max_concurrent_jobs: config.max_concurrent_jobs || 0,
      local_mode:          true,
      local_record_count:  history.length,
    };

    return { status, config, jobs, metrics, history };
  }

  function _computeMetrics(history) {
    const agents = {};
    for (const rec of history) {
      const name = rec.agent_name;
      if (!name) continue;
      if (!agents[name]) {
        agents[name] = {
          run_count: 0,
          error_count: 0,
          total_prompt_tokens: 0,
          total_completion_tokens: 0,
          _total_duration_ms: 0,
          avg_duration_ms: 0,
          recent_runs: [],
          // config fields unknown from run logs alone
          lifecycle_file: '', schedule: '', model: '',
          system_prompt: '', user_prompt: '', tags: [],
        };
      }
      const a = agents[name];
      a.run_count++;
      if (rec.status === 'error') a.error_count++;
      a.total_prompt_tokens     += (rec.tokens && rec.tokens.prompt)   || 0;
      a.total_completion_tokens += (rec.tokens && rec.tokens.response) || 0;
      a._total_duration_ms      += (rec.time   && rec.time.duration_ms) || 0;
      a.recent_runs.push(rec);
    }

    for (const a of Object.values(agents)) {
      if (a.run_count > 0) a.avg_duration_ms = a._total_duration_ms / a.run_count;
      // recent_runs is already sorted descending (history was sorted desc above)
      a.recent_runs = a.recent_runs.slice(0, 50);
      delete a._total_duration_ms;
    }
    return agents;
  }

  // ── UI helpers ─────────────────────────────────────────────────────────────
  function _renderFileList() {
    if (!_fileList) return;
    if (_files.length === 0) { _fileList.innerHTML = ''; return; }

    const kindClass = { jobs: 'fc-json', jsonl: 'fc-jsonl', snapshot: 'fc-snapshot' };
    const kindLabel = { jobs: 'jobs', jsonl: 'runs', snapshot: 'snapshot' };

    _fileList.innerHTML = _files.map((f, i) => {
      const cls  = kindClass[f.kind] || '';
      const lbl  = kindLabel[f.kind] || f.kind;
      const count = f.kind === 'jsonl'     ? ` (${f.data.length})`
                  : f.kind === 'jobs'      ? ` (${f.data.length})`
                  : f.kind === 'snapshot'  ? ` (${(f.data.history || []).length} recs)`
                  : '';
      return `<span class="file-chip ${cls}" data-idx="${i}" title="Click to remove">`
           + `${_esc(f.name)}${count}`
           + `<span class="fc-x">\u00d7</span></span>`;
    }).join('');
  }

  function _updateCount() {
    if (!_loadedCount) return;
    const n = _files.length;
    _loadedCount.textContent = n ? `${n} file${n !== 1 ? 's' : ''}` : '0 files';
  }

  function _showBanner() {
    const banner  = document.getElementById('replay-banner');
    const icon    = document.getElementById('replay-banner-icon');
    const msg     = document.getElementById('replay-banner-msg');
    const liveCtrl = document.getElementById('replay-live-controls');
    const clearBtn = document.getElementById('lt-banner-clear');

    if (!banner) return;

    let recordCount = 0;
    for (const f of _files) {
      if (f.kind === 'jsonl') recordCount += f.data.length;
      else if (f.kind === 'snapshot') recordCount += (f.data.history || []).length;
    }

    banner.style.display     = 'flex';
    banner.style.background  = '#0d2219';
    banner.style.borderBottomColor = '#2ea043';

    if (icon) { icon.textContent = '\ud83d\udcc2 Local'; icon.style.color = '#3fb950'; }
    if (msg) {
      const agentNames = [...new Set(_files.flatMap(f =>
        f.kind === 'jsonl' ? f.data.map(r => r.agent_name).filter(Boolean) : []
      ))];
      const agentStr = agentNames.length
        ? `${agentNames.length} agent${agentNames.length !== 1 ? 's' : ''}`
        : '';
      msg.textContent = [
        agentStr,
        `${recordCount} records`,
        'live refresh paused',
      ].filter(Boolean).join(' · ');
    }
    if (liveCtrl) liveCtrl.style.display = 'none';
    if (clearBtn) clearBtn.style.display = 'inline-block';
  }

  function _hideBanner() {
    const banner  = document.getElementById('replay-banner');
    const clearBtn = document.getElementById('lt-banner-clear');
    if (banner) banner.style.display = 'none';
    if (clearBtn) clearBtn.style.display = 'none';
  }

  // ── unload ─────────────────────────────────────────────────────────────────
  function _unloadAll() {
    _files = [];
    _renderFileList();
    _updateCount();
    _hideBanner();
    if (_hint) _hint.style.display = '';
    _resumePoller();
  }

  // ── poller pause / resume ──────────────────────────────────────────────────
  function _pausePoller() {
    const sel = document.getElementById('refresh-select');
    if (sel && sel.value !== '0') {
      sel.value = '0';
      sel.dispatchEvent(new Event('change'));
    }
  }

  function _resumePoller() {
    // Restore default interval and trigger an immediate refresh
    const sel = document.getElementById('refresh-select');
    if (sel) {
      sel.value = '10000';
      sel.dispatchEvent(new Event('change'));
    }
    const btn = document.getElementById('refresh-now');
    if (btn) btn.click();
  }

  // ── util ───────────────────────────────────────────────────────────────────
  function _esc(s) {
    return String(s || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  return { init, clear: _unloadAll };
})();
