"use strict";

const DtTimeline = (() => {
  function render(container, state) {
    // T7.1: welcome card on first-connect failure (or when no token is
    // present at boot). Rendered in the trace pane in place of the empty
    // event list. Only set during the very first probe — see index.js.
    if (state.welcome) {
      renderWelcome(container, state.welcome);
      return;
    }
    // Show empty/disconnected state when no events and connection errored
    if (!state.events.length && state.connectionState === "error") {
      container.innerHTML = "<div class=\"dt-empty-state\">"
        + "<div class=\"dt-empty-title\">Not connected</div>"
        + "<div class=\"dt-empty-body\">Start <code>leather serve --api</code> and click Reload, or try the demo.</div>"
        + "<div class=\"dt-empty-actions\">"
        + "<button class=\"dt-toolbar-btn\" id=\"dt-empty-demo\">Demo Data</button>"
        + "<button class=\"dt-toolbar-btn\" id=\"dt-empty-reload\">↺ Reload</button>"
        + "</div></div>";
      const demoBtn = container.querySelector("#dt-empty-demo");
      if (demoBtn) demoBtn.onclick = () => { DtStore.set({ demoMode: true }); };
      const reloadBtn = container.querySelector("#dt-empty-reload");
      if (reloadBtn) reloadBtn.onclick = () => {
        // Trigger reload via the top bar reload button if possible
        const btn = document.getElementById("dt-top-reload");
        if (btn) btn.click();
      };
      return;
    }
    if (state.traceView === "conversation") {
      renderConversation(container, state);
      return;
    }
    const events = filtered(state);
    const grouped = state.collapseNoise ? groupByRun(events) : events;
    const compacted = state.collapseNoise ? compact(grouped) : grouped.map(ev => ({ event: ev, count: 1 }));
    const selectedSeq = state.selection && state.selection.type === "event" ? state.selection.seq : 0;
    const relatedSeqs = getRelatedSeqs(state);
    const traceEvents = (state.trace && state.trace.nodes ? state.trace.nodes.map(n => n.event) : []);
    const primaryEvents = state.focusMode && traceEvents.length > 1 ? traceEvents : compacted.map(it => it.event);
    const showNearby = !!state.focusMode;
    const nearby = showNearby ? compacted.filter(it => !primaryEvents.some(pe => pe.seq === it.event.seq)).slice(-16).reverse().map(it => it.event) : [];

    container.innerHTML = ""
      + "<div class=\"dt-trace-header\">"
      + "<div class=\"dt-trace-view-toggle\">"
      + "<button class=\"dt-toolbar-btn" + (state.traceView !== "conversation" ? " active" : "") + "\" id=\"dt-view-trace\">Trace</button>"
      + "<button class=\"dt-toolbar-btn" + (state.traceView === "conversation" ? " active" : "") + "\" id=\"dt-view-conv\">Conversation</button>"
      + "</div>"
      + "<div class=\"dt-trace-actions\">"
      + "<button id=\"dt-filter-signals\" class=\"dt-toolbar-btn" + (state.filterKind === "signal" ? " active" : "") + "\" type=\"button\">Signals</button>"
      + "<button id=\"dt-filter-all\" class=\"dt-toolbar-btn" + (state.filterKind === "" ? " active" : "") + "\" type=\"button\">All</button>"
      + "<button id=\"dt-view-full\" class=\"dt-toolbar-btn\">↩ Full</button>"
      + "<label><input id=\"dt-auto-focus\" type=\"checkbox\" " + (state.autoFocus ? "checked" : "") + "> Auto-focus</label>"
      + "<label><input id=\"dt-auto-hl\" type=\"checkbox\" " + (state.autoHighlightRelated ? "checked" : "") + "> Auto-highlight</label>"
      + "</div></div>"
      + legend()
      + "<div class=\"dt-trace-grid" + (showNearby ? "" : " dt-trace-grid-solo") + "\">"
      + "<div class=\"dt-trace-main\" tabindex=\"0\">" + chainHeader(primaryEvents, state) + renderRows(primaryEvents, selectedSeq, compacted, state, relatedSeqs) + "</div>"
      + (showNearby ? ("<div class=\"dt-trace-nearby\">" + renderNearby(nearby) + "</div>") : "")
      + "</div>";

    const btnFull = container.querySelector("#dt-view-full");
    if (btnFull) {
      btnFull.onclick = () => DtStore.clearFocus();
    }
    const auto = container.querySelector("#dt-auto-focus");
    if (auto) {
      auto.onchange = () => DtStore.set({ autoFocus: !!auto.checked });
    }
    const autoHl = container.querySelector("#dt-auto-hl");
    if (autoHl) autoHl.onchange = () => DtStore.set({ autoHighlightRelated: !!autoHl.checked });
    const btnSignals = container.querySelector("#dt-filter-signals");
    const btnAll = container.querySelector("#dt-filter-all");
    if (btnSignals) btnSignals.onclick = () => DtStore.set({ filterKind: "signal" });
    if (btnAll) btnAll.onclick = () => DtStore.set({ filterKind: "" });
    const traceBtn = container.querySelector("#dt-view-trace");
    const convBtn = container.querySelector("#dt-view-conv");
    if (traceBtn) traceBtn.onclick = () => DtStore.set({ traceView: "trace" });
    if (convBtn) convBtn.onclick = () => DtStore.set({ traceView: "conversation" });

    container.querySelectorAll(".dt-event-row[data-seq]").forEach(el => {
      el.onclick = () => {
        const seq = Number(el.getAttribute("data-seq"));
        DtStore.selectEvent(seq);
        DtRouter.toEvent(seq);
      };
    });
    container.querySelectorAll(".dt-near-row[data-seq]").forEach(el => {
      el.onclick = () => {
        const seq = Number(el.getAttribute("data-seq"));
        DtStore.selectEvent(seq);
        DtRouter.toEvent(seq);
      };
    });
  }

  function filtered(state) {
    const prefixMap = {
      tool: "tool.",
      agent: "agent.",
      queue: "queue.",
      webhook: "webhook.",
      hide: "hide.",
      signal: "signal",
      events: "",
    };
    const prefix = prefixMap[state.filterKind] || "";
    return state.events.filter(ev => {
      if (prefix === "signal") {
        const kind = ev.kind || "";
        const isSignal = kind.startsWith("http.") || kind.startsWith("webhook.") || kind.startsWith("queue.")
          || kind.startsWith("tool.") || kind.startsWith("hide.") || kind.startsWith("artifact.")
          || kind === "agent.response" || kind === "agent.operation.start" || kind === "agent.operation.end"
          || kind.startsWith("curing.") || kind.startsWith("cut.");
        if (!isSignal) return false;
      } else if (prefix && (ev.kind || "").indexOf(prefix) !== 0) {
        return false;
      }
      // Also apply treeFilter from DtStore if present
      if (state.treeFilter) {
        const { kind, id } = state.treeFilter;
        if (typeof DtCommon !== "undefined" && !DtCommon.matchesEntity(ev, kind, id)) return false;
      }
      return true;
    });
  }

  function labelFor(ev) {
    const p = ev.payload || {};
    const kind = ev.kind || "";
    const agent = p.agent || ev.entity_id || "";
    const tool = p.tool || ev.entity_id || "";
    const q = p.queue || p.dest_queue || ev.entity_id || "";
    const hideId = p.hide_id || ev.entity_id || "";
    const hideShort = hideId.length > 12 ? hideId.slice(0, 11) + "\u2026" : hideId;
    const curing = p.curing || ev.entity_id || "";
    const art = p.artifact || ev.entity_id || "";
    switch (kind) {
      case "agent.run":             return "Agent " + agent + ": Run";
      case "agent.response":        return "Agent " + agent + ": Response";
      case "agent.operation.start": return "Agent " + agent + ": Start";
      case "agent.operation.end":   return "Agent " + agent + ": End";
      case "agent.turn.event":      return "Agent " + agent + ": " + (p.progress_kind || "Turn");
      case "tool.call":             return "Tool " + tool + ": Call";
      case "tool.result":           return "Tool " + tool + ": Result";
      case "queue.enqueue":         return "Queue " + q + ": Enqueue";
      case "queue.dequeue":         return "Queue " + q + ": Dequeue";
      case "queue.retry":           return "Queue " + q + ": Retry";
      case "queue.dlq":             return "Queue " + q + ": DLQ";
      case "hide.cut":              return "Hide " + hideShort + ": Cut";
      case "cut.selected":          return "Hide " + hideShort + ": Selected";
      case "artifact.written":      return "Artifact " + art + ": Written";
      case "curing.start":          return "Curing " + curing + ": Start";
      case "curing.end":
      case "curing.complete":       return "Curing " + curing + ": Complete";
      case "curing.step":
      case "curing.step.complete":  return "Curing " + curing + ": Step";
      case "webhook.received":      return "Webhook " + (p.webhook_name || ev.entity_id || "") + ": Received";
      case "http.request":          return "HTTP: " + (p.method || "GET") + " " + (p.path || "-");
      case "cache.hit":             return "Cache: Hit";
      case "cache.miss":            return "Cache: Miss";
      case "cache.store":           return "Cache: Store";
      case "system.metric":         return "System: Metric";
      default:                      return kind.replace(/\./g, ": ").replace(/_/g, " ");
    }
  }

  function renderRows(events, selectedSeq, compacted, state, relatedSeqs) {
    if (!events.length) return "<div class=\"empty\" style=\"padding:24px\">No events yet.</div>";
    const hl = state && state.treeHighlight;
    return events.map((ev, idx) => {
      const active = selectedSeq === ev.seq ? " active" : "";
      const highlighted = (hl && typeof DtCommon !== "undefined" && DtCommon.matchesEntity(ev, hl.kind, hl.id)) ? " highlighted" : "";
      const related = (relatedSeqs && relatedSeqs.has(ev.seq)) ? " related" : "";
      const icon = DtIcons.iconForKind(ev.kind);
      const at = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "-";
      const kls = DtIcons.kindClass(ev.kind);
      const title = labelFor(ev);
      const detail = detailFor(ev);
      const tertiary = tertiaryFor(ev);
      const count = findCount(compacted, ev.seq);
      const curingBadge = (ev.payload && ev.payload.curing)
        ? "<span class=\"dt-curing-badge\">" + esc(ev.payload.curing) + "</span>"
        : "";
      return "<div class=\"dt-event-row" + active + highlighted + related + "\" data-seq=\"" + ev.seq + "\">"
        + "<div class=\"dt-step\"><span class=\"dt-ord " + kls + "\">" + (idx + 1) + "</span><span class=\"dt-step-line\"></span></div>"
        + "<div class=\"dt-event-main\">"
        + "<div class=\"dt-event-top\"><span class=\"dt-icon " + kls + "\">" + icon + "</span><span class=\"dt-kind " + kls + "\">" + esc(title) + "</span>" + (count > 1 ? "<span class=\"dt-count\">x" + count + "</span>" : "") + "<span class=\"dt-time\">" + at + "</span><span class=\"dt-seq\">seq: " + ev.seq + "</span>" + curingBadge + "</div>"
        + "<div class=\"dt-event-sub\">" + esc(detail) + "</div>"
        + (tertiary ? "<div class=\"dt-event-sub dt-event-sub2\">" + esc(tertiary) + "</div>" : "")
        + "</div>"
        + "</div>";
    }).join("");
  }

  function renderNearby(events) {
    if (!events.length) {
      return "<div class=\"dt-near-title\">Nearby events</div><div class=\"dt-near-empty\">No nearby events</div>";
    }
    const rows = events.map(ev => {
      const at = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "-";
      return "<div class=\"dt-near-row\" data-seq=\"" + ev.seq + "\">"
        + "<div class=\"dt-near-top\"><div class=\"dt-near-time\">" + at + "</div><div class=\"dt-near-seq\">seq: " + ev.seq + "</div></div>"
        + "<div class=\"dt-near-kind " + DtIcons.kindClass(ev.kind) + "\">" + esc(labelFor(ev)) + "</div>"
        + "<div class=\"dt-near-detail\">" + esc(detailFor(ev)) + "</div>"
        + "</div>";
    }).join("");
    return "<div class=\"dt-near-title\">Nearby events (live)</div>" + rows;
  }

  function chainHeader(primary, state) {
    const selected = state.selection && state.selection.type === "event"
      ? state.events.find(ev => ev.seq === state.selection.seq)
      : null;
    const marker = selected ? ((selected.entity_id || selected.kind || "").slice(0, 18)) : "live";
    return "<div class=\"dt-chain-head\">"
      + "<div class=\"dt-chain-title\">Causal chain <span>(" + primary.length + " steps)</span></div>"
      + "<div class=\"dt-chain-meta\">Root <span>→</span> This " + esc(marker) + "</div>"
      + "</div>";
  }

  function compact(events) {
    if (!events.length) return [];
    const out = [];
    events.forEach(ev => {
      const prev = out.length ? out[out.length - 1] : null;
      if (!prev) {
        out.push({ event: ev, count: 1 });
        return;
      }
      const sameKind = (prev.event.kind || "") === (ev.kind || "");
      const sameEntity = (prev.event.entity_id || "") === (ev.entity_id || "");
      const noisy = (ev.kind || "").startsWith("agent.turn.event") || (ev.kind || "").startsWith("agent.turn.context");
      if (sameKind && sameEntity && noisy) {
        prev.event = ev;
        prev.count += 1;
        return;
      }
      out.push({ event: ev, count: 1 });
    });
    return out;
  }

  // groupByRun collapses (agent.turn.event system/user) + (agent.response) triplets
  // into a single synthetic "agent.run" row. The user prompt and system prompt are
  // attached to the response row so one row per invocation tells the whole story.
  function groupByRun(events) {
    const out = [];
    let pending = null; // accumulator for a run keyed by entity_id
    const flush = () => {
      if (pending) {
        out.push(...pending.pre);
        pending = null;
      }
    };
    events.forEach(ev => {
      const kind = ev.kind || "";
      const entity = ev.entity_id || "";
      if (kind === "agent.turn.event") {
        const pk = (ev.payload && ev.payload.progress_kind) || "";
        if (pk === "system" || pk === "user") {
          if (!pending || pending.entity !== entity) {
            flush();
            pending = { entity: entity, pre: [], system: "", user: "" };
          }
          if (pk === "system" && !pending.system) pending.system = (ev.payload && ev.payload.prompt) || "";
          if (pk === "user" && !pending.user) pending.user = (ev.payload && ev.payload.prompt) || "";
          pending.pre.push(ev);
          return;
        }
        flush();
        out.push(ev);
        return;
      }
      if (kind === "agent.response") {
        if (pending && pending.entity === entity) {
          const merged = Object.assign({}, ev, {
            kind: "agent.run",
            payload: Object.assign({}, ev.payload || {}, {
              system_prompt: pending.system,
              user_prompt: pending.user,
              merged_seqs: pending.pre.map(p => p.seq),
            }),
          });
          out.push(merged);
          pending = null;
          return;
        }
        flush();
        out.push(ev);
        return;
      }
      flush();
      out.push(ev);
    });
    flush();
    return out;
  }

  function findCount(compacted, seq) {
    const item = compacted.find(it => it.event.seq === seq);
    return item ? item.count : 1;
  }

  function detailFor(ev) {
    const p = ev.payload || {};
    if (ev.kind === "http.request") return (p.method || "GET") + " " + (p.path || "-") + (p.action ? "  \u2022  " + p.action + " \u2022 id: " + (p.delivery_id || "-") : "");
    if (ev.kind === "webhook.received") return (p.webhook_name || ev.entity_id || "-") + "  \u2022  action: " + (p.action || "-");
    if (ev.kind === "queue.enqueue") return "queue: " + (p.dest_queue || p.queue || "-") + "  \u2022  curing: " + (p.curing || "-");
    if (ev.kind === "queue.dequeue") return "queue: " + (p.queue || p.dest_queue || "-") + "  \u2022  curing: " + (p.curing || p.worker || "-");
    if (ev.kind === "queue.retry") return "queue: " + (p.queue || "-") + "  \u2022  attempt: " + (p.attempt || "-") + (p.error ? "  \u2022  " + p.error.slice(0, 60) : "");
    if (ev.kind === "queue.dlq") return "dlq: " + (p.queue || "-") + "-dlq  \u2022  attempt: " + (p.attempt || "-") + (p.error ? "  \u2022  " + p.error.slice(0, 60) : "");
    if (ev.kind === "agent.run") {
      const ct = (p.completion_tokens != null ? p.completion_tokens : 0);
      const pt = (p.prompt_tokens != null ? p.prompt_tokens : 0);
      const tt = (p.total_tokens != null ? p.total_tokens : 0);
      const user = String(p.user_prompt || "").replace(/\s+/g, " ").slice(0, 80);
      return (p.agent || ev.entity_id || "-")
        + "  •  tokens: " + pt + "/" + ct + " (" + tt + ")"
        + (user ? "  •  user: " + user : "");
    }
    if (ev.kind === "agent.response") {
      const ct = (p.completion_tokens != null ? p.completion_tokens : (p.tokens_completion || 0));
      const pt = (p.prompt_tokens != null ? p.prompt_tokens : (p.tokens_prompt || 0));
      const tt = (p.total_tokens != null ? p.total_tokens : (p.tokens || 0));
      return "agent: " + (p.agent || ev.entity_id || "-")
        + "  \u2022  tokens: " + pt + "/" + ct + " (" + tt + ")";
    }
    if (ev.kind === "agent.turn.event") {
      const pk = p.progress_kind || "-";
      const text = p.prompt || p.response || "";
      return pk + (text ? "  \u2022  " + String(text).slice(0, 80).replace(/\s+/g, " ") : "");
    }
    if (ev.kind === "agent.operation.start") return "agent: " + (p.agent || "-") + "  \u2022  model: " + (p.model || "-");
    if (ev.kind === "agent.operation.end") return "agent: " + (p.agent || "-") + "  \u2022  duration: " + ((p.duration_ms || 0) / 1000).toFixed(1) + "s";
    if (ev.kind === "cut.selected") return "hide: " + (p.hide_id || ev.entity_id || "-") + "  \u2022  page: " + (p.page || "-") + "/" + (p.total_pages || "-") + "  \u2022  tokens: " + (p.tokens || "-");
    if (ev.kind === "tool.call") return "tool: " + (p.tool || "-") + (p.path ? "  \u2022  path: " + p.path : "");
    if (ev.kind === "tool.result") return "status: " + (p.error ? "error" : (p.status || "ok")) + (p.lines ? "  \u2022  " + p.lines + " lines" : (p.result_bytes ? "  \u2022  bytes: " + p.result_bytes : ""));
    if (ev.kind === "curing.start") return "curing: " + (p.curing || ev.entity_id || "-") + (p.agent ? "  \u2022  agent: " + p.agent : "") + (p.queue ? "  \u2022  queue: " + p.queue : "");
    if (ev.kind === "curing.end" || ev.kind === "curing.complete") return "curing: " + (p.curing || ev.entity_id || "-") + (p.duration_ms != null ? "  \u2022  duration: " + (p.duration_ms / 1000).toFixed(1) + "s" : "") + (p.status ? "  \u2022  status: " + p.status : "");
    if (ev.kind === "curing.step.complete") return (p.curing || "-") + " \u2022 " + (p.agent || "-") + " \u2022 completed cuts: " + (p.completed_cuts || 0) + "/" + (p.total_cuts || 0);
    if (ev.kind === "curing.step") return "curing: " + (p.curing || ev.entity_id || "-") + (p.step != null ? "  \u2022  step: " + p.step : "") + (p.agent ? "  \u2022  agent: " + p.agent : "");
    if (ev.kind === "artifact.written") return "artifact: " + (p.artifact || ev.entity_id || "-") + (p.path ? "  \u2022  path: " + p.path : "");
    if (ev.kind === "hide.cut") {
      const hideId = p.hide_id || ev.entity_id || "-";
      return "hide: " + hideId
        + (p.page != null ? "  \u2022  page: " + p.page + "/" + (p.total_pages || "?") : "")
        + (p.tokens != null ? "  \u2022  tokens: " + p.tokens : "")
        + (p.bytes != null ? "  \u2022  bytes: " + p.bytes : "")
        + (p.chars != null ? "  \u2022  chars: " + p.chars : "");
    }
    if (ev.kind === "cache.store" || ev.kind === "cache.hit" || ev.kind === "cache.miss") return "key: " + (p.key || "-") + (p.size_bytes ? "  \u2022  size: " + Math.round(p.size_bytes / 1024) + "KB" : "");
    if (ev.kind === "system.metric") return "memory: " + (p.memory_mb || 0) + "MB  \u2022  goroutines: " + (p.goroutines || 0);
    return "source: " + (ev.source || "-") + "  \u2022  entity: " + (ev.entity_id || "-");
  }

  function tertiaryFor(ev) {
    const p = ev.payload || {};
    if ((ev.kind === "agent.run" || ev.kind === "agent.response") && p.response) {
      return String(p.response).replace(/\s+/g, " ").slice(0, 160);
    }
    if (ev.kind === "agent.response" && p.response) {
      return String(p.response).replace(/\s+/g, " ").slice(0, 120);
    }
    if (ev.kind === "agent.turn.event" && p.prompt) {
      return String(p.prompt).replace(/\s+/g, " ").slice(0, 120);
    }
    // For queue events, show the hide_id as contextual tertiary detail.
    if ((ev.kind === "queue.enqueue" || ev.kind === "queue.dequeue"
         || ev.kind === "queue.retry" || ev.kind === "queue.dlq") && p.hide_id) {
      return "hide: " + p.hide_id;
    }
    if (p.path) return "path: " + p.path;
    if (p.model) return "model: " + p.model;
    if (p.worker) return "worker: " + p.worker;
    if (p.operation) return "operation: " + p.operation;
    if (p.result_preview) return String(p.result_preview).slice(0, 80);
    return "";
  }

  function legend() {
    return "<div class=\"dt-legend\">"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-http\"></i>Ingest</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-queue\"></i>Queue</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-agent\"></i>Agent</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-hide\"></i>Context</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-tool\"></i>Tool</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-curing\"></i>Curing</span>"
      + "<span class=\"dt-legend-item\"><i class=\"dt-dot dt-kind-artifact\"></i>Artifact</span>"
      + "</div>";
  }

  function esc(s) {
    return ltEscapeHtml(s == null ? "" : String(s));
  }

  function renderConversation(container, state) {
    // Show ALL conversation-type events — not scoped to a selection.
    // Selection only highlights the matching row.
    const selectedSeq = state.selection && state.selection.type === "event" ? state.selection.seq : 0;
    const events = state.events.filter(ev => {
      const k = ev.kind || "";
      return k === "agent.turn.event" || k === "agent.response" || k === "agent.run"
        || k === "tool.call" || k === "tool.result"
        || k === "hide.cut" || k === "cut.selected";
    });

    container.innerHTML = ""
      + "<div class=\"dt-conv-header\">"
      + "<div class=\"dt-trace-view-toggle\">"
      + "<button class=\"dt-toolbar-btn\" id=\"dt-view-trace\">&larr; Trace</button>"
      + "<button class=\"dt-toolbar-btn active\" id=\"dt-view-conv\">Conversation</button>"
      + "</div>"
      + "</div>"
      + "<div class=\"dt-conv-body\">"
      + (events.length
          ? "<div class=\"dt-conv-title\">" + events.length + " conversation events</div>"
          : "<div class=\"dt-conv-title\">No conversation events yet</div>")
      + events.map(ev => convRow(ev, ev.seq === selectedSeq)).join("")
      + "</div>";

    const traceBtn = container.querySelector("#dt-view-trace");
    if (traceBtn) traceBtn.onclick = () => DtStore.set({ traceView: "trace" });
    const convBtnBack = container.querySelector("#dt-view-conv");
    if (convBtnBack) convBtnBack.onclick = () => DtStore.set({ traceView: "trace" });
    container.querySelectorAll(".dt-conv-row[data-seq]").forEach(el => {
      el.onclick = () => {
        const seq = Number(el.getAttribute("data-seq"));
        if (seq > 0) { DtStore.selectEvent(seq); DtRouter.toEvent(seq); }
      };
    });
  }

  function scopedRunEvents(events, selectedEvent, agentName) {
    if (!events || !events.length || !selectedEvent) return [];
    const beforeIdx = events.findIndex(ev => ev.seq === selectedEvent.seq);
    if (beforeIdx < 0) return [];

    let startIdx = 0;
    for (let i = beforeIdx; i >= 0; i--) {
      const ev = events[i];
      if (!agentName || ev.entity_id === agentName || (ev.payload && ev.payload.agent === agentName)) {
        if (ev.kind === "agent.operation.start") { startIdx = i; break; }
        const pk = ev.payload && ev.payload.progress_kind;
        if (ev.kind === "agent.turn.event" && pk === "system") { startIdx = i; break; }
      }
    }

    let endIdx = events.length - 1;
    for (let i = beforeIdx; i < events.length; i++) {
      const ev = events[i];
      if (!agentName || ev.entity_id === agentName || (ev.payload && ev.payload.agent === agentName)) {
        if (ev.kind === "agent.operation.end" || ev.kind === "agent.response" || ev.kind === "agent.run") {
          endIdx = i;
          break;
        }
      }
    }

    return events.slice(startIdx, endIdx + 1).filter(ev => {
      if (!agentName) return true;
      const p = ev.payload || {};
      return ev.entity_id === agentName || p.agent === agentName
        || ev.kind === "tool.call" || ev.kind === "tool.result";
    });
  }

  function convRow(ev, active) {
    const p = ev.payload || {};
    const ts = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "--:--:--";
    let role = "event";
    let body = "";
    let cls = "dt-conv-other";
    if (ev.kind === "agent.run") {
      role = "agent"; cls = "dt-conv-agent";
      body = String(p.response || "");
    } else if (ev.kind === "agent.response") {
      role = "agent"; cls = "dt-conv-agent";
      body = String(p.response || "");
    } else if (ev.kind === "agent.turn.event") {
      const pk = (p.progress_kind || "").toLowerCase();
      if (pk === "system") { role = "system"; cls = "dt-conv-system"; body = String(p.prompt || ""); }
      else if (pk === "user") { role = "user"; cls = "dt-conv-user"; body = String(p.prompt || ""); }
      else { role = pk || "event"; body = String(p.prompt || p.response || ""); }
    } else if (ev.kind === "tool.call") {
      role = "tool"; cls = "dt-conv-tool";
      body = String(p.tool || "-") + "(" + (p.args || "").slice(0, 200) + ")";
    } else if (ev.kind === "tool.result") {
      role = "result"; cls = "dt-conv-tool";
      body = p.error ? ("\u2717 " + p.error) : ("\u2713 " + (p.result_preview || ("bytes=" + (p.result_bytes || 0))));
    }
    const activeCls = active ? " dt-conv-active" : "";
    return "<div class=\"dt-conv-row " + cls + activeCls + "\" data-seq=\"" + (ev.seq || 0) + "\">"
      + "<div class=\"dt-conv-ts\">" + esc(ts) + "</div>"
      + "<div class=\"dt-conv-role\">" + esc(role) + "</div>"
      + "<div class=\"dt-conv-body\">" + esc(body) + "</div>"
      + "</div>";
  }

  function getRelatedSeqs(state) {
    if (!state.autoHighlightRelated) return new Set();
    const sel = state.selection;
    if (!sel || sel.type !== "event") return new Set();
    const selEv = state.events.find(ev => ev.seq === sel.seq);
    if (!selEv) return new Set();

    const related = new Set();
    const entityId = selEv.entity_id || "";
    const agentName = (selEv.payload && selEv.payload.agent) || entityId;

    if (entityId) {
      state.events.forEach(ev => {
        if (ev.entity_id === entityId) related.add(ev.seq);
      });
    }
    if (agentName && agentName !== entityId) {
      state.events.forEach(ev => {
        const p = ev.payload || {};
        if (p.agent === agentName || ev.entity_id === agentName) related.add(ev.seq);
      });
    }
    if (state.trace && state.trace.nodes) {
      state.trace.nodes.forEach(n => {
        if (n.event && n.event.seq) related.add(n.event.seq);
      });
    }
    related.delete(sel.seq);
    return related;
  }

  // T7.1: welcome card shown on first-connect failure or when no auth token
  // is present at boot. Renders inline (no new dependencies, no modal). The
  // token input writes to sessionStorage under "leather.devtools.token" via
  // DtApi.setToken — the same key used by the URL-hash capture path in api.js.
  function renderWelcome(container, welcome) {
    const reason = welcome && welcome.reason || "error";
    const apiBase = (typeof DtApi !== "undefined" && DtApi.base) ? DtApi.base() : "http://127.0.0.1:7749";
    const headlines = {
      "no-token": "Authentication token required",
      "unauthorized": "Token rejected by API",
      "degraded": "API is starting up or degraded",
      "network": "Cannot reach the Leather API",
      "loading": "Still waiting for the API…",
      "error": "Could not contact the Leather API",
    };
    const bodies = {
      "no-token": "Paste your DevTools token below, or start the server which will print a launch URL containing one.",
      "unauthorized": "The stored token was not accepted. Paste a fresh one from the token file and try again.",
      "degraded": "The API responded 503. The server may still be initialising — try Retry in a moment.",
      "network": "No response from <code>" + escapeHtml(apiBase) + "</code>. The server may not be running.",
      "loading": "First snapshot is taking longer than expected. You can keep waiting, or paste a token / pick a different API address.",
      "error": "First probe failed. See details below and retry.",
    };
    const msg = welcome && welcome.message ? "<div class=\"dt-welcome-detail\"><code>" + escapeHtml(welcome.message) + "</code></div>" : "";
    container.innerHTML = ""
      + "<div class=\"dt-empty-state dt-welcome-card\" role=\"region\" aria-label=\"DevTools welcome\">"
      + "<div class=\"dt-empty-title\">" + escapeHtml(headlines[reason] || headlines.error) + "</div>"
      + "<div class=\"dt-empty-body\">" + (bodies[reason] || bodies.error) + "</div>"
      + "<ol class=\"dt-welcome-steps\">"
      + "<li>Start the API in a terminal: <code>leather serve --api</code></li>"
      + "<li>Find your token in <code>&lt;state-dir&gt;/devtools.token</code> "
      + "(default state-dir varies by OS — see the project README).</li>"
      + "<li>Paste it below and click <strong>Save &amp; connect</strong>.</li>"
      + "</ol>"
      + "<div class=\"dt-welcome-tokenrow\">"
      + "<label class=\"dt-welcome-label\" for=\"dt-welcome-token\">DevTools token</label>"
      + "<input id=\"dt-welcome-token\" class=\"dt-welcome-input\" type=\"password\" "
      + "autocomplete=\"off\" spellcheck=\"false\" "
      + "placeholder=\"paste contents of devtools.token\" "
      + "aria-label=\"DevTools authentication token\">"
      + "<button class=\"dt-toolbar-btn\" id=\"dt-welcome-save\" type=\"button\">Save &amp; connect</button>"
      + "</div>"
      + "<div class=\"dt-empty-actions\">"
      + "<button class=\"dt-toolbar-btn\" id=\"dt-welcome-retry\" type=\"button\" aria-label=\"Retry connection\">↺ Retry</button>"
      + "<button class=\"dt-toolbar-btn\" id=\"dt-welcome-api\" type=\"button\" aria-label=\"Change API address\">Set API address…</button>"
      + "<button class=\"dt-toolbar-btn\" id=\"dt-welcome-demo\" type=\"button\" aria-label=\"Load demo data\">Try demo</button>"
      + "</div>"
      + msg
      + "</div>";

    const tokenInput = container.querySelector("#dt-welcome-token");
    const saveBtn = container.querySelector("#dt-welcome-save");
    const retryBtn = container.querySelector("#dt-welcome-retry");
    const apiBtn = container.querySelector("#dt-welcome-api");
    const demoBtn = container.querySelector("#dt-welcome-demo");
    const doSave = () => {
      const t = tokenInput && tokenInput.value || "";
      if (typeof LeatherDevtools !== "undefined" && LeatherDevtools.saveTokenAndRetry) {
        LeatherDevtools.saveTokenAndRetry(t);
      }
    };
    if (saveBtn) saveBtn.onclick = doSave;
    if (tokenInput) {
      tokenInput.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); doSave(); } };
      // Focus the token input on first paint for keyboard-only users.
      setTimeout(() => { try { tokenInput.focus(); } catch (_) {} }, 0);
    }
    if (retryBtn) retryBtn.onclick = () => {
      if (typeof LeatherDevtools !== "undefined" && LeatherDevtools.retryProbe) LeatherDevtools.retryProbe();
    };
    if (apiBtn) apiBtn.onclick = () => {
      if (typeof LeatherDevtools !== "undefined" && LeatherDevtools.openApiAddressEditor) LeatherDevtools.openApiAddressEditor();
    };
    if (demoBtn) demoBtn.onclick = () => {
      if (typeof LeatherDevtools !== "undefined" && LeatherDevtools.activateDemo) LeatherDevtools.activateDemo();
    };
  }

  function escapeHtml(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, c => ({
      "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;"
    }[c]));
  }

  return { render };
})();
