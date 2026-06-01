"use strict";

const DtStore = (() => {
  const maxEvents = 4096;
  const state = {
    snapshot: null,
    events: [],
    selection: null, // {type:'event',seq} | {type:'entity',kind,id}
    paused: false,
    tail: true,
    filterKind: "",
    timelineQuery: "",
    collapseNoise: true,
    autoFocus: true,
    focusMode: false,
    trace: null,
    inspect: null,
    inspectError: "",
    connectionState: "idle",
    lastSeq: 0,
    dropped: 0,
    stats: null,
    inspectorTab: "details",
    traceView: "trace",
    showEmptySections: false,
    treeHighlight: null,
    treeFilter: null,
    demoMode: false,
    demoReason: "",
    flowMode: false,
    autoHighlightRelated: true,
    // T7.1: welcome card state. null = hidden. Otherwise an object:
    // { reason: "no-token"|"unauthorized"|"degraded"|"network"|"loading"|"error", message?: string }
    // Set only on the first-connect probe (or when no token is present at
    // boot). Never set again on intermittent failures during normal use.
    welcome: null,
  };
  const listeners = [];

  function emit() {
    listeners.forEach(fn => fn(state));
  }

  function set(patch) {
    Object.assign(state, patch);
    emit();
  }

  const CONV_KINDS = new Set([
    "agent.turn.event", "agent.response", "agent.run", "tool.call", "tool.result"
  ]);

  function selectEvent(seq) {
    state.selection = { type: "event", seq: seq };
    // Auto-switch to "details" for non-conversation events so the user always
    // lands on meaningful content rather than an empty conversation tab.
    const ev = state.events.find(e => e.seq === seq);
    if (ev && !CONV_KINDS.has(ev.kind || "")) {
      state.inspectorTab = "details";
    }
    emit();
  }

  function selectEntity(kind, id) {
    state.selection = { type: "entity", kind: kind, id: id };
    // Preserve the user's current inspector tab across selection changes.
    emit();
  }

  function clearFocus() {
    state.focusMode = false;
    state.trace = null;
    emit();
  }

  function addEvent(ev) {
    if (!ev || !ev.seq) return;
    if (state.events.some(existing => existing.seq === ev.seq)) {
      return;
    }
    state.events.push(ev);
    if (state.events.length > maxEvents) {
      state.events.splice(0, state.events.length - maxEvents);
      state.dropped += 1;
    }
    state.lastSeq = ev.seq;
    emit();
  }

  function subscribe(fn) {
    listeners.push(fn);
    fn(state);
    return () => {
      const idx = listeners.indexOf(fn);
      if (idx >= 0) listeners.splice(idx, 1);
    };
  }

  return { state, set, addEvent, subscribe, selectEvent, selectEntity, clearFocus, clearTreeFilter() { DtStore.set({ treeFilter: null, treeHighlight: null }); } };
})();
