"use strict";

const DtInspector = (() => {
  function render(tabsEl, bodyEl, state) {
    const sel = state.selection;
    const selected = selectedEvent(state);
    const inspect = state.inspect;
    const traceNodes = (state.trace && state.trace.nodes) ? state.trace.nodes : [];

    tabsEl.className = "dt-inspector-tab-strip";
    tabsEl.innerHTML = ""
      + tabBtn("details", state.inspectorTab)
      + tabBtn("conversation", state.inspectorTab)
      + tabBtn("raw", state.inspectorTab);

    tabsEl.querySelectorAll("button").forEach(btn => {
      btn.onclick = () => DtStore.set({ inspectorTab: btn.getAttribute("data-tab") });
    });

    if (!sel) {
      bodyEl.textContent = "Select an event to inspect lineage, details, and raw payload.";
      return;
    }

    // Entity selection with no events captured yet — render a useful drill-down
    // summary instead of an empty header.
    if (sel.type === "entity" && !selected) {
      bodyEl.innerHTML = entityDrillDown(state, sel);
      bodyEl.querySelectorAll(".dt-related-row[data-seq]").forEach(el => {
        el.onclick = () => {
          const seq = Number(el.getAttribute("data-seq"));
          if (seq > 0) { DtStore.selectEvent(seq); DtRouter.toEvent(seq); }
        };
      });
      const cfgBtn = bodyEl.querySelector("#dt-view-config");
      if (cfgBtn) cfgBtn.onclick = async () => {
        const btnKind = cfgBtn.getAttribute("data-kind");
        const btnId = cfgBtn.getAttribute("data-id");
        cfgBtn.disabled = true;
        cfgBtn.textContent = "Loading\u2026";
        try {
          const result = await DtApi.getEntityConfig(btnKind, btnId);
          bodyEl.innerHTML += "<div class=\"dt-config-viewer\">"
            + "<div class=\"dt-text-label\">" + esc(btnKind) + " / " + esc(btnId) + " \u2014 " + esc(result.source_file || "") + "</div>"
            + "<pre class=\"dt-pre\">" + esc(result.content || JSON.stringify(result, null, 2)) + "</pre>"
            + "</div>";
        } catch (err) {
          cfgBtn.disabled = false;
          cfgBtn.textContent = "View Config";
          bodyEl.innerHTML += "<div class=\"dt-inspector-empty\">Config not available: " + esc(String(err && err.message ? err.message : err)) + "</div>";
        }
      };
      return;
    }

    const header = headerBlock(sel, selected, inspect);

    if (state.inspectorTab === "raw") {
      bodyEl.innerHTML = header + "<pre class=\"dt-pre\">" + esc(JSON.stringify(inspect || selected || {}, null, 2)) + "</pre>";
      return;
    }

    if (state.inspectorTab === "conversation") {
      bodyEl.innerHTML = header + conversationView(state, selected);
      bodyEl.querySelectorAll(".dt-conv-row[data-seq]").forEach(el => {
        el.onclick = () => {
          const seq = Number(el.getAttribute("data-seq"));
          if (seq > 0) {
            DtStore.selectEvent(seq);
            DtRouter.toEvent(seq);
          }
        };
      });
      return;
    }

    const subject = selected || (inspect && inspect.payload) || {};
    bodyEl.innerHTML = header
      + detailsView(subject, sel);
  }

  function detailsView(subject, sel) {
    const kind = (subject.kind || (sel && sel.kind) || "").toLowerCase();
    const p = subject.payload || {};

    if (kind === "agent.run" || kind === "agent.response" || kind === "agent.operation.start" || kind === "agent.operation.end") return agentRunDetails(subject, p);
    if (kind === "tool.call") return toolCallDetails(subject, p);
    if (kind === "tool.result") return toolResultDetails(subject, p);
    if (kind === "queue.enqueue" || kind === "queue.dequeue") return queueDetails(subject, p);
    if (kind === "webhook.received") return webhookDetails(subject, p);
    if (kind === "artifact.written") return artifactDetails(subject, p);
    if (kind === "hide.cut" || kind === "cut.selected") return hideCutDetails(subject, p);
    if (kind === "agent.turn.event") return agentTurnDetails(subject, p);
    if (kind === "cache.hit" || kind === "cache.miss" || kind === "cache.store") return cacheDetails(subject, p);
    return genericDetails(subject, sel, p);
  }

  // genericPayload shows all non-empty payload fields not in skipKeys.
  // Use this at the end of every specific renderer to surface any fields
  // the curated view didn't explicitly cover.
  function genericPayload(p, skipKeys) {
    const skip = new Set(skipKeys || []);
    const textKeys = new Set(["prompt", "response", "result_preview", "content", "args",
      "summary", "error", "body_preview", "content_preview", "user_prompt", "system_prompt"]);
    const rows = [];
    Object.keys(p).sort().forEach(k => {
      if (skip.has(k) || textKeys.has(k)) return;
      const v = p[k];
      if (v == null || v === "" || v === 0 || v === false) return;
      if (Array.isArray(v) && v.length === 0) return;
      rows.push(kv(k, typeof v === "object" ? JSON.stringify(v) : String(v)));
    });
    return rows.length ? "<div class=\"dt-kv-list dt-kv-payload\">" + rows.join("") + "</div>" : "";
  }

  function agentRunDetails(subject, p) {
    const rows = [];
    if (p.agent || subject.entity_id) rows.push(kv("Agent", p.agent || subject.entity_id));
    if (p.model) rows.push(kv("Model", p.model));
    if (p.operation) rows.push(kv("Operation", p.operation));
    if (p.prompt_tokens || p.completion_tokens || p.total_tokens) {
      rows.push(kv("Prompt tokens", String(p.prompt_tokens || 0)));
      rows.push(kv("Completion tokens", String(p.completion_tokens || 0)));
      rows.push(kv("Total tokens", String(p.total_tokens || 0)));
    }
    if (p.tool_rounds != null) rows.push(kv("Tool rounds", String(p.tool_rounds)));
    if (p.duration_ms != null) rows.push(kv("Duration", (p.duration_ms / 1000).toFixed(2) + "s"));
    if (p.cache_hit) rows.push(kv("Cache hit", "yes"));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + (p.user_prompt ? textBlock("User prompt", p.user_prompt) : "")
      + (p.response ? textBlock("Response", p.response) : "")
      + genericPayload(p, ["agent", "model", "operation", "prompt_tokens", "completion_tokens",
          "total_tokens", "tool_rounds", "duration_ms", "cache_hit", "user_prompt", "response",
          "progress_kind", "round", "merged_seqs", "system_prompt"]);
  }

  function toolCallDetails(subject, p) {
    const rows = [];
    if (p.tool) rows.push(kv("Tool", p.tool));
    if (p.agent || subject.entity_id) rows.push(kv("Agent", p.agent || subject.entity_id));
    if (p.server || p.mcp_server) rows.push(kv("MCP server", p.server || p.mcp_server));
    if (p.path) rows.push(kv("Path", p.path));
    if (p.round != null) rows.push(kv("Round", String(p.round)));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + (p.args ? textBlock("Arguments", p.args) : "")
      + genericPayload(p, ["tool", "agent", "server", "mcp_server", "path", "round", "args", "tool_type", "progress_kind"]);
  }

  function toolResultDetails(subject, p) {
    const ok = !p.error;
    const rows = [kv("Status", ok ? "\u2713 ok" : "\u2717 error")];
    if (p.tool) rows.push(kv("Tool", p.tool));
    if (p.lines != null) rows.push(kv("Lines", String(p.lines)));
    if (p.result_bytes != null) rows.push(kv("Bytes", String(p.result_bytes)));
    return "<div class=\"dt-kv-list\">" + rows.join("") + "</div>"
      + (p.error ? textBlock("Error", p.error) : "")
      + (p.result_preview ? textBlock("Preview", p.result_preview) : "")
      + genericPayload(p, ["tool", "lines", "result_bytes", "error", "result_preview", "progress_kind", "round", "tool_type"]);
  }

  function queueDetails(subject, p) {
    const rows = [];
    // For single-use fan-out enqueue events, dest_queue holds the dynamic queue name
    // and p.queue (source) may be empty. Show them separately so users see both sides.
    const singleUse = p.dest_queue && p.hide_id && p.dest_queue.includes(p.hide_id);
    if (p.dest_queue) {
      const label = singleUse ? "Dest queue <span class=\"dt-badge-inline dt-badge-ephemeral\">single-use</span>" : "Dest queue";
      rows.push(kvRaw(label, p.dest_queue));
    }
    if (p.queue) rows.push(kv("Source queue", p.queue));
    if (!p.dest_queue && !p.queue) rows.push(kv("Queue", "-"));
    if (p.priority != null) rows.push(kv("Priority", String(p.priority)));
    if (p.worker) rows.push(kv("Worker", p.worker));
    if (p.curing) rows.push(kv("Curing", p.curing));
    if (p.hide_id) rows.push(kv("Correlation ID", p.hide_id));
    if (p.hide_kind) rows.push(kv("Hide kind", p.hide_kind));
    if (p.item_id) rows.push(kv("Item ID", p.item_id));
    if (p.attempt) rows.push(kv("Attempt", String(p.attempt)));
    if (p.error) rows.push(kv("Error", p.error));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + genericPayload(p, ["queue", "dest_queue", "priority", "worker", "curing",
          "hide_id", "hide_kind", "item_id", "attempt", "error", "source", "webhook_name"]);
  }

  function webhookDetails(subject, p) {
    const rows = [];
    if (p.webhook_name || subject.entity_id) rows.push(kv("Webhook", p.webhook_name || subject.entity_id));
    if (p.action) rows.push(kv("Action", p.action));
    if (p.delivery_id) rows.push(kv("Delivery ID", p.delivery_id));
    if (p.path) rows.push(kv("Path", p.path));
    if (p.queue) rows.push(kv("Queue", p.queue));
    if (p.curing) rows.push(kv("Curing", p.curing));
    if (p.hide_id) rows.push(kv("Hide ID", p.hide_id));
    if (p.hide_kind) rows.push(kv("Hide kind", p.hide_kind));
    if (p.route_count != null) rows.push(kv("Routes matched", String(p.route_count)));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + (p.body_preview ? textBlock("Body preview", p.body_preview) : "")
      + genericPayload(p, ["webhook_name", "action", "delivery_id", "path", "queue",
          "curing", "hide_id", "hide_kind", "route_count", "source"]);
  }

  function artifactDetails(subject, p) {
    const rows = [];
    if (p.artifact || subject.entity_id) rows.push(kv("Artifact ID", p.artifact || subject.entity_id));
    if (p.path) rows.push(kv("Path", p.path));
    if (p.curing) rows.push(kv("Curing", p.curing));
    if (p.agent) rows.push(kv("Agent", p.agent));
    if (p.hide_id) rows.push(kv("Hide ID", p.hide_id));
    if (p.size_bytes != null) rows.push(kv("Bytes", String(p.size_bytes)));
    if (p.page != null) rows.push(kv("Page", String(p.page) + " / " + (p.total_pages || "?")));
    if (p.tokens != null) rows.push(kv("Tokens", String(p.tokens)));
    if (p.operation) rows.push(kv("Operation", p.operation));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + (p.content_preview ? textBlock("Content preview", p.content_preview) : "")
      + genericPayload(p, ["artifact", "path", "curing", "agent", "hide_id", "size_bytes",
          "page", "total_pages", "tokens", "operation"]);
  }

  function hideCutDetails(subject, p) {
    const rows = [];
    if (p.hide_id || subject.entity_id) rows.push(kv("Hide ID", p.hide_id || subject.entity_id));
    if (p.page != null) rows.push(kv("Page", String(p.page) + " / " + (p.total_pages || "?")));
    if (p.tokens != null) rows.push(kv("Tokens", String(p.tokens)));
    if (p.bytes != null) rows.push(kv("Bytes", String(p.bytes)));
    if (p.chars != null) rows.push(kv("Chars", String(p.chars)));
    return (rows.length ? "<div class=\"dt-kv-list\">" + rows.join("") + "</div>" : "")
      + genericPayload(p, ["hide_id", "page", "total_pages", "tokens", "bytes", "chars", "progress_kind"]);
  }

  function agentTurnDetails(subject, p) {
    const pk = p.progress_kind || "-";
    const rows = [kv("Role", pk)];
    if (subject.entity_id) rows.push(kv("Agent", subject.entity_id));
    if (p.round != null) rows.push(kv("Round", String(p.round)));
    if (p.skill) rows.push(kv("Skill", p.skill));
    if (p.var_key) rows.push(kv("Variable", p.var_key + " = " + (p.var_val || "")));
    if (p.hide_id) rows.push(kv("Hide ID", p.hide_id));
    if (p.total_pages != null) rows.push(kv("Hide pages", String(p.total_pages)));
    return "<div class=\"dt-kv-list\">" + rows.join("") + "</div>"
      + (p.prompt ? textBlock(pk === "system" ? "System prompt" : "Prompt", p.prompt) : "")
      + (p.response ? textBlock("Response", p.response) : "")
      + genericPayload(p, ["progress_kind", "round", "skill", "var_key", "var_val",
          "hide_id", "total_pages", "prompt", "response", "agent", "tool", "tool_type"]);
  }

  function cacheDetails(subject, p) {
    const rows = [kv("Event", subject.kind || "-")];
    if (p.key) rows.push(kv("Key", p.key));
    if (p.size_bytes != null) rows.push(kv("Size (bytes)", String(p.size_bytes)));
    return "<div class=\"dt-kv-list\">" + rows.join("") + "</div>"
      + genericPayload(p, ["key", "size_bytes"]);
  }

  function genericDetails(subject, sel, p) {
    const payload = p || {};
    const meta = ""
      + "<div class=\"dt-kv-list\">"
      + kv("seq", subject.seq != null ? String(subject.seq) : "-")
      + kv("kind", esc(subject.kind || (sel && sel.kind) || "-"))
      + kv("source", esc(subject.source || "-"))
      + kv("at", subject.at ? ltFormatTs(subject.at) : "-")
      + kv("entity", esc((subject.entity_kind || (sel && sel.kind) || "-") + " / " + (subject.entity_id || (sel && sel.id) || "-")))
      + (subject.err ? kv("error", esc(subject.err)) : "")
      + "</div>";
    const textKeys = ["prompt", "response", "result_preview", "content", "args", "summary", "error"];
    const textBlocks = [];
    textKeys.forEach(k => {
      const v = payload[k];
      if (v && String(v).trim() !== "") textBlocks.push(textBlock(k, v));
    });
    const skip = new Set(textKeys);
    const rows = [];
    Object.keys(payload).sort().forEach(k => {
      if (skip.has(k)) return;
      const v = payload[k];
      if (v == null || v === "" || v === 0 || v === false) return;
      if (Array.isArray(v) && v.length === 0) return;
      rows.push(kv(k, typeof v === "object" ? JSON.stringify(v) : esc(String(v))));
    });
    const payloadList = rows.length ? "<div class=\"dt-kv-list dt-kv-payload\">" + rows.join("") + "</div>" : "";
    return meta + textBlocks.join("") + payloadList;
  }

  function textBlock(label, value) {
    return "<div class=\"dt-text-block\">"
      + "<div class=\"dt-text-label\">" + esc(label) + "</div>"
      + "<div class=\"dt-text-body\">" + esc(String(value)) + "</div>"
      + "</div>";
  }

  function conversationView(state, selected) {
    // Scope to the agent of the selected event.
    let agentName = "";
    if (selected) {
      const p = selected.payload || {};
      agentName = p.agent || (selected.entity_kind === "agent" ? selected.entity_id : "");
    }
    const all = state.events || [];
    const selSeq = selected ? (selected.seq || 0) : 0;

    // All conversation events for this agent.
    const agentConv = all.filter(ev => {
      if (ev.source !== "runner") return false;
      const evAgent = (ev.payload || {}).agent || (ev.entity_kind === "agent" ? ev.entity_id : "");
      if (agentName && evAgent !== agentName) return false;
      const k = ev.kind || "";
      return k === "agent.turn.event" || k === "agent.response" || k === "agent.run"
        || k === "tool.call" || k === "tool.result";
    });

    if (agentConv.length === 0) {
      return "<div class=\"dt-inspector-empty\">No runner events captured yet for this agent.</div>";
    }

    // Scope to the "current round": events between the last agent.response
    // strictly before selSeq and the first agent.response at-or-after selSeq.
    // System-prompt turns (progress_kind=system) are always included.
    let roundStartSeq = 0; // exclusive lower bound (events after this are in scope)
    let roundEndSeq = Infinity;

    if (selected) {
      for (const ev of agentConv) {
        if ((ev.kind === "agent.response" || ev.kind === "agent.run") && ev.seq < selSeq) {
          roundStartSeq = ev.seq; // will include events AFTER this
        }
      }
      for (const ev of agentConv) {
        if ((ev.kind === "agent.response" || ev.kind === "agent.run") && ev.seq >= selSeq) {
          roundEndSeq = ev.seq; // inclusive upper bound
          break;
        }
      }
    }

    const conv = agentConv.filter(ev => {
      // Always include system-prompt turns (appear once, not per-round).
      if (ev.kind === "agent.turn.event" && (ev.payload || {}).progress_kind === "system") return true;
      return ev.seq > roundStartSeq && ev.seq <= roundEndSeq;
    });

    const activeSeq = selected ? selected.seq : -1;
    const rows = conv.map(ev => convRow(ev, ev.seq === activeSeq)).join("");
    const roundLabel = selected ? " \u00b7 round events" : "";
    const title = agentName
      ? "<div class=\"dt-conv-title\">Conversation \u00b7 " + esc(agentName) + roundLabel + " \u00b7 " + conv.length + "</div>"
      : "<div class=\"dt-conv-title\">Conversation \u00b7 " + conv.length + " events</div>";
    return title + "<div class=\"dt-conv\">" + rows + "</div>";
  }

  function convRow(ev, active) {
    const p = ev.payload || {};
    const ts = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "--:--:--";
    let role = "event";
    let body = "";
    let meta = "";
    let cls = "dt-conv-other";
    if (ev.kind === "agent.run") {
      role = "agent";
      cls = "dt-conv-agent";
      body = String(p.response || "");
      const pt = p.prompt_tokens || 0;
      const ct = p.completion_tokens || 0;
      const tt = p.total_tokens || 0;
      meta = "tokens: prompt=" + pt + "  completion=" + ct + "  total=" + tt
        + (p.user_prompt ? "\nuser: " + String(p.user_prompt).slice(0, 200) : "");
    } else if (ev.kind === "agent.response") {
      role = "agent";
      cls = "dt-conv-agent";
      body = String(p.response || "");
      const pt = p.prompt_tokens || 0;
      const ct = p.completion_tokens || 0;
      const tt = p.total_tokens || 0;
      meta = "tokens: prompt=" + pt + "  completion=" + ct + "  total=" + tt;
    } else if (ev.kind === "agent.turn.event") {
      const pk = (p.progress_kind || "").toLowerCase();
      if (pk === "system") { role = "system"; cls = "dt-conv-system"; body = String(p.prompt || ""); }
      else if (pk === "user") { role = "user"; cls = "dt-conv-user"; body = String(p.prompt || ""); }
      else if (pk === "extract") { role = "extract"; cls = "dt-conv-other"; body = (p.var_key || "") + " = " + (p.var_val || ""); }
      else if (pk === "skill_start") { role = "skill"; cls = "dt-conv-other"; body = String(p.skill || ""); }
      else if (pk === "hide") { role = "hide"; cls = "dt-conv-other"; body = (p.hide_id || "") + "  pages: " + (p.total_pages || "-"); }
      else { role = pk || "event"; body = String(p.prompt || p.response || ""); }
    } else if (ev.kind === "tool.call") {
      role = "tool";
      cls = "dt-conv-tool";
      body = String(p.tool || "-") + "(" + (p.args || "").slice(0, 200) + ")";
    } else if (ev.kind === "tool.result") {
      role = "result";
      cls = "dt-conv-tool";
      body = p.error ? ("\u2717 " + p.error) : ("\u2713 " + (p.result_preview || ("bytes=" + (p.result_bytes || 0))));
    }
    const activeCls = active ? " dt-conv-active" : "";
    return "<div class=\"dt-conv-row " + cls + activeCls + "\" data-seq=\"" + (ev.seq || 0) + "\">"
      + "<div class=\"dt-conv-ts\">" + esc(ts) + "</div>"
      + "<div class=\"dt-conv-role\">" + esc(role) + "</div>"
      + "<div class=\"dt-conv-body\">" + esc(body) + (meta ? "<div class=\"dt-conv-meta\">" + esc(meta) + "</div>" : "") + "</div>"
      + "</div>";
  }

  function selectedEvent(state) {
    const sel = state.selection;
    if (!sel) return null;
    if (sel.type === "event") {
      return state.events.find(ev => ev.seq === sel.seq) || null;
    }
    const matches = state.events.filter(ev => {
      const p = ev.payload || {};
      if (sel.kind === "tool") {
        return (ev.kind || "").startsWith("tool.") && String(p.tool || "") === sel.id;
      }
      if (!ev.entity_id || !sel.id) return false;
      if (ev.entity_id !== sel.id) return false;
      return ev.entity_kind === sel.kind;
    });
    return matches.length ? matches[matches.length - 1] : null;
  }

  function headerBlock(sel, selected, inspect) {
    const title = sel.type === "event"
      ? ((selected && selected.kind) || "event")
      : (sel.kind + " / " + sel.id);
    const seq = selected && selected.seq ? selected.seq : "-";
    const at = selected && selected.at ? ltFormatTs(selected.at).split(" ")[1] : "-";
    const rel = inspect && inspect.payload ? inspect.payload : selected;
    const chips = [];
    if (rel && rel.source) chips.push("source: " + rel.source);
    if (rel && rel.entity_kind) chips.push("kind: " + rel.entity_kind);
    if (rel && rel.entity_id) chips.push("id: " + rel.entity_id);
    return "<div class=\"dt-inspector-head\">"
      + "<div><div class=\"dt-inspector-kicker " + DtIcons.kindClass((selected && selected.kind) || "") + "\">" + esc((selected && selected.kind) ? selected.kind.toUpperCase().replace(/\./g, "_") : "ENTITY") + "</div><div class=\"dt-inspector-seq\">" + at + "</div></div>"
      + "<div style=\"display:flex;align-items:center;gap:8px\">"
      + (rel && rel.payload && rel.payload.curing ? "<span class=\"dt-curing-badge\">" + esc(rel.payload.curing) + "</span>" : (rel && rel.curing ? "<span class=\"dt-curing-badge\">" + esc(rel.curing) + "</span>" : ""))
      + "<div class=\"dt-inspector-seq\">seq: " + seq + "</div>"
      + "</div>"
      + "</div><div class=\"dt-inspector-title\">" + esc(entityName(selected, rel, title)) + "</div><div class=\"dt-inspector-chiprow\">" + chips.map(c => "<span class=\"dt-badge\">" + esc(c) + "</span>").join("") + "</div>";
  }

  function quickActions(flags) {
    const canHide = !!(flags && flags.canHide);
    const canCut = !!(flags && flags.canCut);
    const canArtifact = !!(flags && flags.canArtifact);
    return "<div class=\"dt-quick-actions\">"
      + "<button type=\"button\" data-action=\"find-origin\">Replay this operation</button>"
      + "<button type=\"button\" data-action=\"related\">Open related</button>"
      + "<button type=\"button\" data-action=\"copy-lineage\">Copy lineage</button>"
      + "<button type=\"button\" data-action=\"open-hide\" " + (canHide ? "" : "disabled") + ">Open hide</button>"
      + "<button type=\"button\" data-action=\"open-cut\" " + (canCut ? "" : "disabled") + ">Open cut</button>"
      + "<button type=\"button\" data-action=\"open-artifact\" " + (canArtifact ? "" : "disabled") + ">Open artifact</button>"
      + "</div>";
  }

  function tabBtn(name, active) {
    const cls = active === name ? "active" : "";
    const labels = { details: "Details", conversation: "Conversation", raw: "Raw JSON" };
    const label = labels[name] || name;
    return "<button class=\"" + cls + "\" data-tab=\"" + name + "\">" + label + "</button>";
  }

  function entityName(selected, rel, fallback) {
    const payload = (selected && selected.payload) || {};
    return payload.artifact || payload.path || payload.hide_id || (rel && rel.entity_id) || fallback;
  }

  function kv(k, v) {
    return "<div class=\"dt-kv\"><span>" + esc(k) + "</span><span>" + esc(v) + "</span></div>";
  }

  // kvRaw: like kv but the key may contain trusted HTML (e.g. inline badges).
  function kvRaw(k, v) {
    return "<div class=\"dt-kv\"><span>" + k + "</span><span>" + esc(v) + "</span></div>";
  }

  function kvRow(label, value, action, enabled) {
    const on = enabled !== false;
    return "<div class=\"dt-row-action\"><div><div class=\"dt-row-k\">" + esc(label) + "</div><div class=\"dt-row-v\">" + esc(value) + "</div></div><button type=\"button\" data-action=\"" + esc(action) + "\" " + (on ? "" : "disabled") + ">" + esc(actionLabel(action)) + "</button></div>";
  }

  function relatedEvents(state, selected) {
    const base = selected || (state.inspect && state.inspect.payload) || {};
    return state.events
      .filter(ev => ev.entity_id && base.entity_id && ev.entity_id === base.entity_id)
      .slice(-12)
      .reverse();
  }

  function syntheticLineage(state, selected) {
    if (!selected || !selected.seq) return [];
    const nearby = state.events.filter(ev => Math.abs(ev.seq - selected.seq) <= 6);
    return nearby.slice(-8);
  }

  // entityDrillDown renders a useful summary for an entity (agent, queue, tool, ...)
  // when nothing about it has been observed in the event ring yet, or when the user
  // simply clicked it in the tree.
  function entityDrillDown(state, sel) {
    const kind = sel.kind;
    const id = sel.id;
    const events = state.events.filter(ev => {
      const p = ev.payload || {};
      if (kind === "tool") return (ev.kind || "").startsWith("tool.") && String(p.tool || "") === id;
      if (kind === "queue") return String(p.queue || "") === id;
      if (kind === "worker") return String(p.worker || "") === id;
      if (kind === "webhook") return String(p.webhook_name || ev.entity_id || "") === id;
      if (kind === "curing") return String(p.curing || "") === id;
      // Hide: match any event carrying this hide_id in the payload, plus legacy entity_kind="hide" events.
      if (kind === "hide") {
        if (String(p.hide_id || "") === id && id !== "") return true;
        return ev.entity_kind === "hide" && ev.entity_id === id;
      }
      return ev.entity_kind === kind && ev.entity_id === id;
    });
    const head = "<div class=\"dt-inspector-head\">"
      + "<div><div class=\"dt-inspector-kicker dt-kind-" + esc(kind) + "\">" + esc(kind.toUpperCase()) + "</div></div>"
      + "</div>"
      + "<div class=\"dt-inspector-title\">" + esc(id) + "</div>";
    const configBtn = (kind === "agent" || kind === "curing" || kind === "server")
      ? "<button class=\"dt-toolbar-btn\" id=\"dt-view-config\" data-kind=\"" + esc(kind) + "\" data-id=\"" + esc(id) + "\">View Config</button>"
      : "";
    if (!events.length) {
      return head + "<div class=\"dt-inspector-empty\">No events captured for this " + esc(kind) + " yet. It will populate as soon as it runs.</div>";
    }
    // Per-kind summary
    let stats = "";
    if (kind === "hide") {
      // Extract metadata from any event carrying this hide_id.
      const rep = events[0] || {};
      const rp = rep.payload || {};
      // Prefer a hide.cut event for page/token info if available.
      const cutEv = events.find(ev => ev.kind === "hide.cut" || ev.kind === "cut.selected") || rep;
      const cp = cutEv.payload || {};
      // Gather distinct curings + queues + agents that touched this hide.
      const curings = [...new Set(events.map(ev => (ev.payload || {}).curing).filter(Boolean))];
      const queues  = [...new Set(events.map(ev => (ev.payload || {}).queue).filter(Boolean))];
      const enqueues  = events.filter(ev => ev.kind === "queue.enqueue").length;
      const dequeues  = events.filter(ev => ev.kind === "queue.dequeue").length;
      stats = "<div class=\"dt-kv-list dt-kv-payload\">"
        + kv("hide id", id)
        + (rp.hide_kind ? kv("kind", rp.hide_kind) : "")
        + (curings.length ? kv("curing", curings.join(", ")) : "")
        + (queues.length  ? kv("queue",  queues.join(", "))  : "")
        + (cp.total_pages ? kv("pages", String(cp.total_pages)) : "")
        + (cp.tokens ? kv("tokens", String(cp.tokens)) : "")
        + (cp.bytes  ? kv("bytes",  String(cp.bytes))  : "")
        + kv("enqueue", String(enqueues))
        + kv("dequeue", String(dequeues))
        + "</div>";
    } else if (kind === "agent") {
      const runs = events.filter(ev => ev.kind === "agent.response" || ev.kind === "agent.run");
      const totals = runs.reduce((acc, ev) => {
        const p = ev.payload || {};
        acc.prompt += p.prompt_tokens || 0;
        acc.completion += p.completion_tokens || 0;
        acc.total += p.total_tokens || 0;
        return acc;
      }, { prompt: 0, completion: 0, total: 0 });
      stats = "<div class=\"dt-kv-list dt-kv-payload\">"
        + kv("runs", String(runs.length))
        + kv("prompt tokens", String(totals.prompt))
        + kv("completion tokens", String(totals.completion))
        + kv("total tokens", String(totals.total))
        + (runs.length ? kv("avg total/run", String(Math.round(totals.total / runs.length))) : "")
        + "</div>";
    } else {
      stats = "<div class=\"dt-kv-list dt-kv-payload\">" + kv("events", String(events.length)) + "</div>";
    }
    const recent = events.slice(-12).reverse().map(ev => {
      const ts = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "--:--:--";
      return "<div class=\"dt-related-row\" data-seq=\"" + ev.seq + "\">"
        + "<span class=\"dt-conv-ts\">" + esc(ts) + "</span>"
        + "<span class=\"dt-kind " + DtIcons.kindClass(ev.kind) + "\">" + esc((ev.kind || "").toUpperCase().replace(/\./g, "_")) + "</span>"
        + "<span class=\"dt-near-seq\">seq " + ev.seq + "</span>"
        + "</div>";
    }).join("");
    const recentBlock = "<div class=\"dt-conv-title\">Recent events</div><div class=\"dt-related\">" + recent + "</div>";
    return head + configBtn + stats + recentBlock;
  }

  function bindActionButtons(container, state, selected) {
    const payload = (selected && selected.payload) || {};
    container.querySelectorAll("button[data-action]").forEach(btn => {
      btn.onclick = async () => {
        if (btn.disabled) return;
        const action = btn.getAttribute("data-action");
        if (action === "related") {
          DtStore.set({ treeFilter: null, treeHighlight: null });
          return;
        }
        if (action === "find-origin") {
          const origin = state.events.find(ev => ev.kind && (ev.kind.startsWith("http.") || ev.kind.startsWith("webhook.")));
          if (origin) {
            DtStore.selectEvent(origin.seq);
            DtRouter.toEvent(origin.seq);
          }
          return;
        }
        if (action === "view-agent") {
          const agent = payload.agent || (selected && selected.entity_kind === "agent" ? selected.entity_id : "");
          if (agent) {
            DtStore.selectEntity("agent", agent);
            DtRouter.toEntity("agent", agent);
          }
          return;
        }
        if (action === "open-hide") {
          jumpByQuery(payload.hide_id || "", "hide");
          return;
        }
        if (action === "open-cut") {
          const q = payload.hide_id ? (payload.hide_id + " " + (payload.page || "")) : String(payload.page || "");
          jumpByQuery(q, "hide");
          return;
        }
        if (action === "open-artifact") {
          jumpByQuery(payload.path || payload.artifact || (selected && selected.entity_kind === "artifact" ? selected.entity_id : ""), "artifact");
          return;
        }
        if (action === "copy-lineage") {
          const text = buildLineageText(state, selected);
          try {
            await navigator.clipboard.writeText(text);
          } catch (_) {
            // Clipboard might be unavailable on some file:// contexts.
          }
        }
      };
    });
  }

  function jumpByQuery(query, filterKind) {
    if (!query) return;
    // Use treeFilter to filter the trace to the matching entity
    const kindMap = {
      hide: "hide",
      artifact: "artifact",
      signal: "agent",
    };
    const kind = kindMap[filterKind] || filterKind || "hide";
    DtStore.set({
      treeFilter: { kind: kind, id: String(query) },
      treeHighlight: null,
      inspectorTab: "details",
    });
  }

  function buildLineageText(state, selected) {
    const traceNodes = (state.trace && state.trace.nodes) ? state.trace.nodes : [];
    const nodes = traceNodes.length
      ? traceNodes.map(n => n.event || {})
      : syntheticLineage(state, selected);
    if (!nodes.length) return "No lineage";
    return nodes.map(ev => "#" + (ev.seq || "-") + " " + (ev.kind || "-") + " " + (ev.entity_id || "-")).join("\n");
  }

  function actionLabel(action) {
    switch (action) {
      case "find-origin": return "Find origin";
      case "open-hide": return "Open hide";
      case "open-cut": return "Open cut";
      case "view-agent": return "View agent";
      case "open-artifact": return "Open artifact";
      default: return action;
    }
  }

  function shortKind(kind) {
    const k = (kind || "").split(".");
    return k[k.length - 1] || "event";
  }

  // 9-node fixed pictographic chain matching design spec
  function lineageGraph(sequence, selected) {
    const nodes = [
      { id: "http", label: "HTTP", cls: "dt-kind-http", icon: "\u25CE", match: ev => (ev.kind || "").startsWith("http.") },
      { id: "webhook", label: "Webhook", cls: "dt-kind-webhook", icon: "\u2301", match: ev => (ev.kind || "").startsWith("webhook.") },
      { id: "enqueue", label: "Enqueue", cls: "dt-kind-queue", icon: "\u21A5", match: ev => ev.kind === "queue.enqueue" },
      { id: "dequeue", label: "Dequeue", cls: "dt-kind-queue", icon: "\u21A7", match: ev => ev.kind === "queue.dequeue" },
      { id: "agent", label: "Agent", cls: "dt-kind-agent", icon: "\u2618", match: ev => ev.kind === "agent.operation.start" },
      { id: "cut", label: "Cut", cls: "dt-kind-hide", icon: "\u25A3", match: ev => ev.kind === "cut.selected" || ev.kind === "hide.cut" },
      { id: "toolcall", label: "Tool Call", cls: "dt-kind-tool", icon: "\u25B6", match: ev => ev.kind === "tool.call" },
      { id: "toolresult", label: "Tool Result", cls: "dt-kind-tool", icon: "\u2713", match: ev => ev.kind === "tool.result" },
      { id: "artifact", label: "Artifact", cls: "dt-kind-artifact", icon: "\u25C7", match: ev => ev.kind === "artifact.written" },
    ];
    const cells = nodes.map((n, idx) => {
      const ev = (sequence || []).find(e => n.match(e));
      const active = selected && ev && ev.seq === selected.seq;
      const seqAttr = ev ? ev.seq : 0;
      const stateCls = ev ? " present" : " missing";
      const activeCls = active ? " active" : "";
      const arrow = idx < nodes.length - 1 && idx !== 4 ? "<span class=\"dt-line-arrow\">\u2192</span>" : "";
      return "<div class=\"dt-line-cell\">"
        + "<button type=\"button\" class=\"dt-line-node " + n.cls + stateCls + activeCls + "\" data-seq=\"" + seqAttr + "\" " + (ev ? "" : "disabled") + ">"
        + "<span class=\"dt-line-node-icon\">" + n.icon + "</span></button>"
        + "<span class=\"dt-line-node-label\">" + esc(n.label) + "</span>"
        + arrow
        + "</div>";
    }).join("");
    return "<div class=\"dt-lineage-graph\">" + cells + "</div>";
  }

  function originLabel(sequence, subject) {
    const origin = (sequence || []).find(ev => (ev.kind || "").startsWith("http.") || (ev.kind || "").startsWith("webhook."));
    if (!origin) return (subject.kind || "-");
    const p = origin.payload || {};
    if (origin.kind === "http.request") return "HTTP " + (p.method || "GET") + " " + (p.path || "-");
    return (origin.kind || "-") + " " + (p.action || p.webhook_name || "");
  }

  function esc(s) {
    return ltEscapeHtml(s == null ? "" : String(s));
  }

  return { render };
})();
