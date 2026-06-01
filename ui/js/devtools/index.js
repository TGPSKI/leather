"use strict";

const LeatherDevtools = (() => {
  let mounted = false;
  let refs = null;
  let lastTraceSeq = 0;
  let lastInspectKey = "";
  let unsub = null;

  function init() {
    // Standalone devtools.html: mount immediately (no tab router present)
    const isStandalone = document.getElementById("devtools-root") &&
      !document.querySelector('.tab-btn[data-tab="devtools"]');
    if (isStandalone) {
      mount();
      return;
    }
    // Legacy tab-embedded path (kept for backwards compat during transition)
    document.addEventListener("lttabchange", e => {
      if (e && e.detail && e.detail.tab === "devtools") {
        mount();
      } else {
        unmount();
      }
    });

    if (window.location.hash.indexOf("#/devtools") === 0) {
      const btn = document.querySelector('.tab-btn[data-tab="devtools"]');
      if (btn) btn.click();
      mount();
    }
  }

  function mount() {
    if (mounted) return;
    const root = document.getElementById("devtools-root");
    if (!root) return;

    root.innerHTML = ""
      + "<header class=\"dt-topbar\" id=\"dt-topbar\">"
      + "<div class=\"dt-top-left\"><span class=\"dt-top-brand\">DevTools</span><span class=\"dt-top-beta\">BETA</span><span id=\"dt-top-conn\" class=\"dt-top-conn\">Connecting</span></div>"
      + "<div class=\"dt-top-right\">"
      + "<span id=\"dt-api-addr-wrap\" class=\"dt-api-addr-wrap dt-api-addr-hidden\">"
      + "<input id=\"dt-api-addr\" class=\"dt-api-addr\" type=\"url\" placeholder=\"http://127.0.0.1:7749\" title=\"API base URL\">"
      + "</span>"
      + "<button id=\"dt-top-settings\" class=\"dt-top-toggle\" type=\"button\" title=\"Configure API address\">⚙</button>"
      + "<button id=\"dt-top-demo\" class=\"dt-top-toggle dt-top-toggle-muted\" type=\"button\" title=\"Load demo data\">Demo</button>"
      + "<button id=\"dt-top-reload\" class=\"dt-top-toggle\" type=\"button\" title=\"Reload snapshot\">Reload</button>"
      + "<span id=\"dt-top-live\" class=\"dt-top-live\">Live</span><span id=\"dt-top-seq\">Seq: 0</span></div>"
      + "</header>"
      + "<div class=\"dt-content\" id=\"dt-content\">"
      + "<nav class=\"dt-subtab-strip\">"
      + "<button class=\"dt-subtab active\" id=\"dt-subtab-events\">Events</button>"
      + "<button class=\"dt-subtab\" id=\"dt-subtab-flow\">Flow</button>"
      + "</nav>"
      + "<div class=\"dt-panels\">"
      + "<div class=\"dt-panel\" id=\"dt-panel-events\">"
      + "<div class=\"dt-body\" id=\"dt-body\">"
      + "<aside class=\"dt-pane dt-pane-tree\" id=\"dt-pane-tree\"><h3>System Tree</h3><div class=\"dt-scroll\" id=\"dt-tree\"></div></aside>"
      + "<div class=\"dt-resize-handle\" id=\"dt-resize-left\" aria-label=\"Resize tree\" tabindex=\"0\"></div>"
      + "<main class=\"dt-pane dt-pane-trace\" id=\"dt-pane-trace\"><div class=\"dt-toolbar\"><button id=\"dt-pause\" class=\"dt-toolbar-btn\" aria-pressed=\"false\">⏸ Pause</button><button id=\"dt-tail\" class=\"dt-toolbar-btn\" aria-pressed=\"true\">⤓ Tail</button><button id=\"dt-clear\" class=\"dt-toolbar-btn\">✕ Clear</button></div><div class=\"dt-scroll dt-trace-main\" id=\"dt-timeline\"></div></main>"
      + "<div class=\"dt-resize-handle\" id=\"dt-resize-mid\" aria-label=\"Resize inspector\" tabindex=\"0\"></div>"
      + "<section class=\"dt-pane dt-pane-inspector\" id=\"dt-pane-inspector\"><h3>Inspector</h3><div class=\"dt-inspector-tab-strip\" id=\"dt-inspector-tabs\"></div><div class=\"dt-inspector\" id=\"dt-inspector\"></div></section>"
      + "</div>"
      + "</div>"
      + "<div class=\"dt-panel dt-panel-hidden\" id=\"dt-panel-flow\">"
      + "<div class=\"dt-flow-outer\" id=\"dt-flow-container\"></div>"
      + "</div>"
      + "</div>"
      + "</div>"
      + "<footer class=\"dt-footer\" id=\"dt-footer\"></footer>";

    refs = {
      tree: document.getElementById("dt-tree"),
      timeline: document.getElementById("dt-timeline"),
      tabs: document.getElementById("dt-inspector-tabs"),
      inspector: document.getElementById("dt-inspector"),
      footer: document.getElementById("dt-footer"),
      topConn: document.getElementById("dt-top-conn"),
      topLive: document.getElementById("dt-top-live"),
      topSeq: document.getElementById("dt-top-seq"),
      pause: document.getElementById("dt-pause"),
      tail: document.getElementById("dt-tail"),
      clear: document.getElementById("dt-clear"),
    };

    refs.pause.onclick = () => {
      const next = !DtStore.state.paused;
      DtStore.set({ paused: next });
      refs.pause.setAttribute("aria-pressed", String(next));
    };
    refs.tail.onclick = () => {
      const next = !DtStore.state.tail;
      DtStore.set({ tail: next });
      refs.tail.setAttribute("aria-pressed", String(next));
    };
    refs.clear.onclick = () => DtStore.set({ events: [], selection: null, trace: null, inspect: null });

    const demoBtn = document.getElementById("dt-top-demo");
    if (demoBtn) {
      // T1.11: only show the Demo button when the demo bundle was loaded
      // (i.e. user opted in with ?demo=1 or #demo=1). Without DtDemo present
      // the button cannot do anything useful.
      if (typeof DtDemo === "undefined") {
        demoBtn.style.display = "none";
      } else {
        demoBtn.onclick = () => {
          const next = !DtStore.state.demoMode;
          DtStore.set({ demoMode: next });
          bootstrapData();
        };
      }
    }
    const reloadBtn = document.getElementById("dt-top-reload");
    if (reloadBtn) reloadBtn.onclick = () => bootstrapData();

    const evTabBtn = document.getElementById("dt-subtab-events");
    const flTabBtn = document.getElementById("dt-subtab-flow");
    if (evTabBtn) evTabBtn.onclick = () => DtStore.set({ flowMode: false });
    if (flTabBtn) flTabBtn.onclick = () => DtStore.set({ flowMode: true });

    // Gear icon: toggle API address input visibility
    const settingsBtn = document.getElementById("dt-top-settings");
    const addrWrap = document.getElementById("dt-api-addr-wrap");
    const addrInput = document.getElementById("dt-api-addr");
    if (settingsBtn && addrWrap && addrInput) {
      // Pre-fill with current base
      const currentBase = localStorage.getItem("leather.devtools.apiAddr") || "http://127.0.0.1:7749";
      addrInput.value = currentBase;
      settingsBtn.onclick = () => {
        const hidden = addrWrap.classList.toggle("dt-api-addr-hidden");
        if (!hidden) addrInput.focus();
      };
      const applyAddr = () => {
        const val = addrInput.value.trim();
        if (val) {
          localStorage.setItem("leather.devtools.apiAddr", val);
          const url = new URL(window.location.href);
          url.searchParams.set("api", val);
          window.location.href = url.toString();
        }
      };
      addrInput.onchange = applyAddr;
      addrInput.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); applyAddr(); } };
    }

    document.addEventListener("keydown", onKeydown);

    unsub = DtStore.subscribe(scheduleRender);
    bootstrapData();
    restoreColumnWidths();
    initResizeHandles();

    mounted = true;
  }

  // Coalesce rapid SSE-driven re-renders into one frame to avoid focus theft,
  // scroll resets, and double-click bugs when events arrive while interacting.
  let renderScheduled = false;
  let pendingState = null;
  function scheduleRender(state) {
    pendingState = state;
    if (renderScheduled) return;
    renderScheduled = true;
    (window.requestAnimationFrame || (cb => setTimeout(cb, 16)))(() => {
      renderScheduled = false;
      const s = pendingState;
      pendingState = null;
      if (s) render(s);
    });
  }

  function unmount() {
    if (!mounted) return;
    DtStream.disconnect();
    if (unsub) {
      unsub();
      unsub = null;
    }
    document.removeEventListener("keydown", onKeydown);
    mounted = false;
  }

  function isDemoRequested() {
    if (typeof DtDemo === "undefined") return false;
    if (DtStore.state.demoMode) return true;
    const h = window.location.hash || "";
    if (h.indexOf("demo=1") >= 0) return true;
    const q = window.location.search || "";
    return q.indexOf("demo=1") >= 0;
  }

  function loadDemo(reason) {
    DtStream.disconnect();
    const snap = DtDemo.snapshot();
    DtStore.set({
      demoMode: true,
      snapshot: snap,
      stats: snap.bus_stats || null,
      events: (snap.recent_events || []).slice(),
      lastSeq: snap.recent_events && snap.recent_events.length
        ? snap.recent_events[snap.recent_events.length - 1].seq
        : 0,
      connectionState: "demo",
      demoReason: reason || "",
      trace: null,
      inspect: null,
    });
    const events = snap.recent_events || [];
    if (events.length) {
      const artifact = events.slice().reverse().find(ev => ev.kind === "artifact.written") || events[events.length - 1];
      DtStore.selectEvent(artifact.seq);
      DtRouter.toEvent(artifact.seq);
    }
  }

  async function bootstrapData() {
    if (isDemoRequested()) {
      loadDemo("user toggle");
      return;
    }
    // T7.1: if this is the very first connect attempt and no token is in
    // storage, show the welcome card immediately rather than letting the
    // probe fail with an opaque 401. We still attempt the probe in the
    // background in case the server is running with auth disabled, but the
    // welcome card stays visible until the user supplies a token and clicks
    // Save & connect (or Retry).
    if (!firstProbeDone && !DtApi.token()) {
      DtStore.set({ welcome: { reason: "no-token" }, connectionState: "idle" });
    } else if (!firstProbeDone) {
      // Show a transient loading welcome if the probe takes too long.
      armLoadingTimer();
    }
    try {
      DtStore.set({ connectionState: "loading", demoMode: false, demoReason: "" });
      const snap = await DtApi.snapshot();
      clearLoadingTimer();
      firstProbeDone = true;
      DtStore.set({
        snapshot: snap,
        stats: snap.bus_stats || null,
        events: snap.recent_events || [],
        lastSeq: (snap.recent_events && snap.recent_events.length) ? snap.recent_events[snap.recent_events.length - 1].seq : 0,
        connectionState: "snapshot",
        welcome: null,
      });
      if (snap.recent_events && snap.recent_events.length && !DtStore.state.selection) {
        const latest = snap.recent_events[snap.recent_events.length - 1];
        DtStore.selectEvent(latest.seq);
        DtRouter.toEvent(latest.seq);
      }
      DtStream.connect(DtApi.eventsURL(), {
        onEvent(ev) {
          if (DtStore.state.paused) return;
          DtStore.addEvent(ev);
        },
        onState(state) {
          DtStore.set({ connectionState: state });
        },
        onGap(info) {
          DtStore.set({ connectionState: "gap" });
          console.warn("devtools gap", info);
        },
      });
    } catch (err) {
      clearLoadingTimer();
      console.warn("devtools bootstrap failed", err);
      const patch = {
        connectionState: "error",
        demoMode: false,
        demoReason: "",
        events: [],
        snapshot: null,
      };
      // T7.1: surface the welcome card only on the first probe so transient
      // failures during normal use don't reset the UI.
      if (!firstProbeDone) {
        let reason = "error";
        if (err && err.networkError) reason = "network";
        else if (err && (err.status === 401 || err.status === 403)) reason = "unauthorized";
        else if (err && err.status === 503) reason = "degraded";
        patch.welcome = { reason: reason, message: err && err.message ? err.message : String(err) };
      }
      firstProbeDone = true;
      DtStore.set(patch);
    }
  }

  // T7.1: helpers and module-scoped flags for the welcome card.
  let firstProbeDone = false;
  let loadingTimer = null;
  function armLoadingTimer() {
    clearLoadingTimer();
    loadingTimer = setTimeout(() => {
      if (!firstProbeDone && !DtStore.state.welcome) {
        DtStore.set({ welcome: { reason: "loading" } });
      }
    }, 5000);
  }
  function clearLoadingTimer() {
    if (loadingTimer) { clearTimeout(loadingTimer); loadingTimer = null; }
  }

  // Exposed for the welcome card buttons (timeline.js renders them).
  function saveTokenAndRetry(t) {
    DtApi.setToken((t || "").trim());
    firstProbeDone = false;
    DtStore.set({ welcome: null });
    bootstrapData();
  }
  function retryProbe() {
    firstProbeDone = false;
    DtStore.set({ welcome: null });
    bootstrapData();
  }
  function openApiAddressEditor() {
    const settingsBtn = document.getElementById("dt-top-settings");
    if (settingsBtn) settingsBtn.click();
    const input = document.getElementById("dt-api-addr");
    if (input) input.focus();
  }
  function activateDemo() {
    DtStore.set({ demoMode: true, welcome: null });
    bootstrapData();
  }

  function render(state) {
    if (!refs) return;
    DtTree.render(refs.tree, state);
    DtTimeline.render(refs.timeline, state);
    DtInspector.render(refs.tabs, refs.inspector, state);
    DtFooter.render(refs.footer, state);

    // Sync subtab panel visibility with flowMode state.
    const evPanel = document.getElementById("dt-panel-events");
    const flPanel = document.getElementById("dt-panel-flow");
    const evTabEl = document.getElementById("dt-subtab-events");
    const flTabEl = document.getElementById("dt-subtab-flow");
    if (state.flowMode) {
      if (evPanel) evPanel.classList.add("dt-panel-hidden");
      if (flPanel) flPanel.classList.remove("dt-panel-hidden");
      if (evTabEl) evTabEl.classList.remove("active");
      if (flTabEl) flTabEl.classList.add("active");
      const fc = document.getElementById("dt-flow-container");
      if (fc && typeof DtFlow !== "undefined") DtFlow.render(fc, state);
    } else {
      if (evPanel) evPanel.classList.remove("dt-panel-hidden");
      if (flPanel) flPanel.classList.add("dt-panel-hidden");
      if (evTabEl) evTabEl.classList.add("active");
      if (flTabEl) flTabEl.classList.remove("active");
    }
    if (refs.topConn) {
      let txt = "Connecting";
      let cls = "dt-state-warn";
      if (state.demoMode) { txt = "DEMO DATA " + (state.demoReason ? "(" + state.demoReason + ")" : ""); cls = "dt-state-warn"; }
      else if (state.connectionState === "live") { txt = "Connected to " + DtApi.base(); cls = "dt-state-live"; }
      else if (state.connectionState === "snapshot") { txt = "Snapshot loaded \u2014 awaiting stream"; cls = "dt-state-warn"; }
      else if (state.connectionState === "error") { txt = "Connection error"; cls = "dt-state-err"; }
      refs.topConn.textContent = txt;
      refs.topConn.className = "dt-top-conn " + cls;
    }
    if (refs.topLive) {
      const live = !state.demoMode && state.connectionState === "live";
      refs.topLive.textContent = state.demoMode ? "Demo" : "Live";
      refs.topLive.className = "dt-top-live " + (live ? "dt-state-live" : "dt-state-warn");
    }
    const dBtn = document.getElementById("dt-top-demo");
    if (dBtn) dBtn.setAttribute("aria-pressed", String(!!state.demoMode));
    if (refs.topSeq) {
      refs.topSeq.textContent = "Seq: " + (state.lastSeq || 0);
    }
    refs.pause.setAttribute("aria-pressed", String(!!state.paused));
    refs.tail.setAttribute("aria-pressed", String(!!state.tail));

    if (state.tail) {
      const main = refs.timeline.querySelector(".dt-trace-main");
      if (main) {
        main.scrollTop = main.scrollHeight;
      }
    }

    syncSelectionFromRoute(state);
    syncSelectionData(state);
  }

  function syncSelectionFromRoute(state) {
    const route = DtRouter.parseHash();
    if (route.type === "event") {
      const seq = Number(route.id);
      if (!Number.isNaN(seq) && (!state.selection || state.selection.type !== "event" || state.selection.seq !== seq)) {
        DtStore.selectEvent(seq);
      }
      return;
    }
    if (route.type === "entity") {
      const kind = route.kind;
      const id = route.id;
      if (!state.selection || state.selection.type !== "entity" || state.selection.kind !== kind || state.selection.id !== id) {
        DtStore.selectEntity(kind, id);
      }
    }
  }

  async function syncSelectionData(state) {
    const sel = state.selection;
    if (!sel) return;

    if (sel.type === "event") {
      if (sel.seq !== lastTraceSeq) {
        lastTraceSeq = sel.seq;
        try {
          const trace = await DtApi.trace(sel.seq);
          const nodeCount = trace && trace.nodes ? trace.nodes.length : 0;
          if (DtStore.state.selection && DtStore.state.selection.type === "event" && DtStore.state.selection.seq === sel.seq) {
            DtStore.set({
              trace: trace,
              focusMode: (DtStore.state.autoFocus && nodeCount > 1) || DtStore.state.focusMode,
              inspect: null,
              inspectError: "",
            });
          }
        } catch (err) {
          DtStore.set({ trace: null, inspectError: String(err && err.message ? err.message : err) });
        }
      }
      return;
    }

    const key = sel.kind + ":" + sel.id;
    if (key === lastInspectKey) return;
    lastInspectKey = key;
    try {
      const inspect = await DtApi.inspect(sel.kind, sel.id);
      if (DtStore.state.selection && DtStore.state.selection.type === "entity"
        && DtStore.state.selection.kind === sel.kind && DtStore.state.selection.id === sel.id) {
        DtStore.set({ inspect: inspect, inspectError: "" });
      }
    } catch (err) {
      DtStore.set({ inspect: null, inspectError: String(err && err.message ? err.message : err) });
    }
  }

  function onKeydown(ev) {
    if (!mounted) return;

    if (ev.key === "/" && !inputFocused()) {
      const input = document.getElementById("dt-tree-search");
      if (input) { ev.preventDefault(); input.focus(); }
      return;
    }

    if (ev.key === "Escape") {
      DtStore.clearFocus();
      if (typeof DtStore.clearTreeFilter === "function") DtStore.clearTreeFilter();
      return;
    }

    if (inputFocused()) return;

    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      ev.preventDefault();
      navigateTrace(ev.key === "ArrowDown" ? 1 : -1);
      return;
    }

    if (ev.key === "Enter") {
      const active = document.querySelector(".dt-event-row.active");
      if (active) active.scrollIntoView({ block: "nearest" });
      return;
    }
  }

  function inputFocused() {
    const el = document.activeElement;
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
  }

  function navigateTrace(delta) {
    const state = DtStore.state;
    const events = state.events || [];
    if (!events.length) return;
    const currentSeq = state.selection && state.selection.type === "event"
      ? state.selection.seq : 0;
    const idx = events.findIndex(ev => ev.seq === currentSeq);
    const next = idx < 0
      ? (delta > 0 ? 0 : events.length - 1)
      : Math.max(0, Math.min(events.length - 1, idx + delta));
    const target = events[next];
    if (target) {
      DtStore.selectEvent(target.seq);
      DtRouter.toEvent(target.seq);
      requestAnimationFrame(() => {
        const row = document.querySelector(".dt-event-row[data-seq=\"" + target.seq + "\"]");
        if (row) row.scrollIntoView({ block: "nearest", behavior: "smooth" });
      });
    }
  }

  function restoreColumnWidths() {
    const tree = localStorage.getItem("leather.devtools.dt-tree-w");
    const insp = localStorage.getItem("leather.devtools.dt-inspector-w");
    if (tree) document.documentElement.style.setProperty("--dt-tree-w", tree);
    if (insp) document.documentElement.style.setProperty("--dt-inspector-w", insp);
  }

  function initResizeHandles() {
    function makeResizer(handleId, cssVar, side) {
      const handle = document.getElementById(handleId);
      if (!handle) return;
      let startX, startVal;
      handle.addEventListener("mousedown", e => {
        startX = e.clientX;
        const current = parseInt(
          getComputedStyle(document.documentElement).getPropertyValue(cssVar) || "220", 10
        );
        startVal = current;
        handle.classList.add("dragging");
        const move = ev => {
          const delta = ev.clientX - startX;
          const next = Math.max(120, Math.min(1200, startVal + (side === "right" ? -delta : delta)));
          document.documentElement.style.setProperty(cssVar, next + "px");
          localStorage.setItem("leather.devtools." + cssVar.replace(/^--/, ""), next + "px");
        };
        const up = () => {
          handle.classList.remove("dragging");
          document.removeEventListener("mousemove", move);
          document.removeEventListener("mouseup", up);
        };
        document.addEventListener("mousemove", move);
        document.addEventListener("mouseup", up);
        e.preventDefault();
      });
    }
    makeResizer("dt-resize-left", "--dt-tree-w", "left");
    makeResizer("dt-resize-mid", "--dt-inspector-w", "right");
  }

  return { init, mount, unmount, saveTokenAndRetry, retryProbe, openApiAddressEditor, activateDemo };
})();

LeatherDevtools.init();
