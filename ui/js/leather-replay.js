/**
 * leather-replay.js — replay banner, download-snapshot button, and
 * live-replay playback controls.
 *
 * Depends on: leather-api.js, leather-common.js
 *
 * DOM surface
 * -----------
 *  #replay-banner        — yellow bar rendered between topbar and content
 *  #replay-banner-msg    — text label inside the banner
 *  #replay-live-controls — container for live-replay buttons (hidden in
 *                          static-replay and normal modes)
 *  #replay-btn-pause     — pause / resume toggle
 *  #replay-btn-slower    — halve current speed
 *  #replay-btn-faster    — double current speed
 *  #replay-btn-1x        — reset speed to 1×
 *  #replay-speed-display — "1.0×" label
 *  #btn-download-snapshot — topbar button; always rendered, always active
 */

'use strict';

const LtReplay = (() => {
  // ── internal state ────────────────────────────────────────────────────────
  let _replayMode     = false;   // static snapshot replay
  let _replayLiveMode = false;   // live JSONL replay
  let _paused         = false;
  let _speed          = 1.0;

  // ── helpers ───────────────────────────────────────────────────────────────
  function el(id)  { return document.getElementById(id); }
  function fmt(ts) {
    if (!ts) return '';
    return new Date(ts * 1000).toLocaleString(undefined, {
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
  }
  function fmtSpeed(s) {
    return (s === Math.floor(s)) ? s + '×' : s.toFixed(1) + '×';
  }

  // ── public: update from polled data ──────────────────────────────────────
  /**
   * Called on every poll cycle with the current status payload.
   * @param {object} status – parsed /status JSON
   */
  function onStatus(status) {
    if (!status) return;

    _replayMode     = !!status.replay_mode;
    _replayLiveMode = !!status.replay_live_mode;

    const banner      = el('replay-banner');
    const bannerMsg   = el('replay-banner-msg');
    const liveCtrl    = el('replay-live-controls');
    const speedDisp   = el('replay-speed-display');
    const pauseBtn    = el('replay-btn-pause');

    if (!banner) return;

    if (_replayMode) {
      banner.style.display = 'flex';
      if (liveCtrl) liveCtrl.style.display = 'none';
      if (bannerMsg) {
        const ts = status.replay_captured_at ? fmt(status.replay_captured_at) : '—';
        bannerMsg.textContent = `Snapshot captured ${ts} · read-only`;
      }
      return;
    }

    // Local-file mode is managed by LtLocal — don't touch the banner.
    if (status.local_mode) return;

    if (_replayLiveMode) {
      _paused = !!status.replay_paused;
      _speed  = status.replay_speed || 1.0;

      banner.style.display = 'flex';
      if (liveCtrl) liveCtrl.style.display = 'flex';
      if (speedDisp) speedDisp.textContent = fmtSpeed(_speed);
      if (pauseBtn) pauseBtn.textContent = _paused ? '▶ Resume' : '⏸ Pause';

      if (bannerMsg) {
        const clockStr = status.replay_clock_at ? fmt(status.replay_clock_at) : '—';
        const state    = _paused ? 'paused' : `${fmtSpeed(_speed)} speed`;
        bannerMsg.textContent = `Live Replay · clock ${clockStr} · ${state}`;
      }
      return;
    }

    // Normal mode — hide banner.
    banner.style.display = 'none';
  }

  // ── public: init (call after DOM ready) ──────────────────────────────────
  function init() {
    _initDownloadBtn();
    _initLiveControls();
  }

  // ── download snapshot ─────────────────────────────────────────────────────
  // Snapshot is built client-side from the last cached ltdataupdate payload.
  // This works in both live (API-polling) and local-file modes and does not
  // require the server to be reachable at download time.
  function _initDownloadBtn() {
    const btn = el('btn-download-snapshot');
    if (!btn) return;
    btn.addEventListener('click', () => {
      try {
        const snap = LtDataCache.buildSnapshot();
        if (!snap) {
          alert('No data loaded yet — wait for the first poll cycle or load local files.');
          return;
        }
        const blob = new Blob([JSON.stringify(snap, null, 2)], { type: 'application/json' });
        const url  = URL.createObjectURL(blob);
        const a    = document.createElement('a');
        a.href     = url;
        const ts   = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        a.download = `leather-snapshot-${ts}.json`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
      } catch (err) {
        console.error('leather-replay: snapshot build failed', err);
        alert('Snapshot failed: ' + (err && err.message ? err.message : err));
      }
    });
  }

  // ── live-replay controls ──────────────────────────────────────────────────
  function _initLiveControls() {
    const pauseBtn  = el('replay-btn-pause');
    const slowerBtn = el('replay-btn-slower');
    const fasterBtn = el('replay-btn-faster');
    const resetBtn  = el('replay-btn-1x');

    if (pauseBtn) {
      pauseBtn.addEventListener('click', () => {
        const action = _paused ? 'resume' : 'pause';
        LtApi.replayControl(action).catch(e => console.error('replay control', e));
      });
    }
    if (slowerBtn) {
      slowerBtn.addEventListener('click', () => {
        const next = Math.max(0.25, _speed / 2);
        LtApi.replayControl('speed', next).catch(e => console.error('replay control', e));
      });
    }
    if (fasterBtn) {
      fasterBtn.addEventListener('click', () => {
        const next = Math.min(64, _speed * 2);
        LtApi.replayControl('speed', next).catch(e => console.error('replay control', e));
      });
    }
    if (resetBtn) {
      resetBtn.addEventListener('click', () => {
        LtApi.replayControl('speed', 1.0).catch(e => console.error('replay control', e));
      });
    }
  }

  return { init, onStatus };
})();
