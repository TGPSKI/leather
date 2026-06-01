"use strict";

const DtTree = (() => {
  let query = "";

  function render(container, state) {
    const snap = state.snapshot || {};
    const stats = snap.bus_stats || {};
    const events = state.events;

    const model = mergeDirectory(buildModel(events), snap.directory);
    const selected = state.selection && state.selection.type === "entity"
      ? state.selection.kind + ":" + state.selection.id
      : "";

    // Build candidate sections; only render those with items (or always for Agents+Cache),
    // unless user toggled showEmptySections.
    const showAll = !!state.showEmptySections;
    const tannerySub = model.queues.length + model.workers.length + model.webhooks.length;
    const hasTannery = events.some(ev => ev.kind && (ev.kind.startsWith("queue.") || ev.kind.startsWith("webhook.") || ev.kind.startsWith("curing.") || ev.kind === "artifact.written"));
    const tanneryStatus = (hasTannery && tannerySub > 0) ? "running" : "idle";
    const sections = [];
    if (showAll || hasTannery) {
      const ephemeralQueueSet = model.ephemeralQueues || new Set();
      const staticQs = model.queues.filter(r => !ephemeralQueueSet.has(r.id));
      const ephemeralQs = model.queues.filter(r => ephemeralQueueSet.has(r.id));
      const queueBranches = [
        branch("Static", "queue", staticQs, selected),
        branchEphemeral("Single-use", ephemeralQs, selected),
        branch("Workers", "worker", model.workers, selected),
        branch("Webhooks", "webhook", model.webhooks, selected),
      ];
      sections.push(rootSection("Tannery", tanneryStatus, "tannery", tannerySub, queueBranches));
    }
    if (showAll || model.curings.length) {
      sections.push(rootSection("Curings", "active", "curing", model.curings.length, [leaves("curing", model.curings, selected, state)]));
    }
    sections.push(rootSection("Agents", "live", "agent", model.agents.length, [leaves("agent", model.agents, selected, state)]));
    if (showAll || model.tools.length) {
      sections.push(rootSection("MCP Servers", "live", "server", model.tools.length, [leaves("tool", model.tools, selected, state)]));
    }
    if (model.toolsets && model.toolsets.length) {
      sections.push(rootSection("Toolsets", "live", "server", model.toolsets.length, [leaves("toolset", model.toolsets, selected, state)]));
    }
    if (showAll || model.hides.length) {
      sections.push(rootSection("Hides", "buffered", "hide", model.hides.length, [leaves("hide", model.hides, selected, state)]));
    }
    if (showAll || model.artifacts.length) {
      sections.push(rootSection("Artifacts", "stored", "artifact", model.artifacts.length, [leaves("artifact", model.artifacts, selected, state)]));
    }
    if (showAll || model.cacheActive) {
      const cacheStatus = model.cacheHitRate != null ? Math.round(model.cacheHitRate * 100) + "% hit" : "active";
      sections.push(rootSection("Cache", cacheStatus, "cache", model.cacheTotal || 0, []));
    }
    const hiddenCount = (showAll ? 0
      : (hasTannery ? 0 : 1)
      + (model.curings.length ? 0 : 1)
      + (model.tools.length ? 0 : 1)
      + (model.hides.length ? 0 : 1)
      + (model.artifacts.length ? 0 : 1)
      + (model.cacheActive ? 0 : 1));
    const toggle = "<div class=\"dt-tree-toggle\"><label><input id=\"dt-tree-showall\" type=\"checkbox\" "
      + (showAll ? "checked" : "")
      + "> Show empty (" + hiddenCount + " hidden)</label></div>";
    const html = ""
      + "<div class=\"dt-tree-search\"><input id=\"dt-tree-search\" type=\"text\" placeholder=\"Search entities...\" value=\"" + esc(query) + "\"></div>"
      + toggle
      + sections.join("");

    container.innerHTML = html;

    const input = container.querySelector("#dt-tree-search");
    if (input) {
      input.oninput = () => {
        query = input.value || "";
        render(container, state);
      };
      input.onkeydown = ev => {
        ev.stopPropagation();
      };
    }
    const showAllBox = container.querySelector("#dt-tree-showall");
    if (showAllBox) {
      showAllBox.onchange = () => DtStore.set({ showEmptySections: !!showAllBox.checked });
    }

    container.querySelectorAll(".dt-tree-leaf[data-kind][data-id]").forEach(el => {
      let lastClick = 0;
      el.onclick = () => {
        const kind = el.getAttribute("data-kind");
        const id = el.getAttribute("data-id");
        const now = Date.now();
        const isDouble = (now - lastClick) < 300;
        lastClick = now;

        if (isDouble) {
          // Double click: toggle trace filter
          const cur = DtStore.state.treeFilter;
          const same = cur && cur.kind === kind && cur.id === id;
          DtStore.set({ treeFilter: same ? null : { kind, id }, treeHighlight: null });
          return;
        }

        // Single click: toggle highlight; for hide entities also navigate to
        // show the hide inspector summary (hides have no standalone event kind).
        const cur = DtStore.state.treeHighlight;
        const same = cur && cur.kind === kind && cur.id === id;
        DtStore.set({ treeHighlight: same ? null : { kind, id }, treeFilter: null });
        if (kind === "hide" && !same) {
          DtStore.selectEntity(kind, id);
          DtRouter.toEntity(kind, id);
        }
      };
    });

  }

  function buildModel(events) {
    const queues = {};
    const workers = {};
    const webhooks = {};
    const curings = {};
    const agents = {};
    const artifacts = {};
    const hides = {};
    const tools = {};
    let cacheHits = 0, cacheMisses = 0, cacheStores = 0;

    events.forEach(ev => {
      const p = ev.payload || {};
      if (p.queue) queues[String(p.queue)] = (queues[String(p.queue)] || 0) + 1;
      if (p.dest_queue) queues[String(p.dest_queue)] = (queues[String(p.dest_queue)] || 0) + 1;
      if (p.worker) workers[String(p.worker)] = (workers[String(p.worker)] || 0) + 1;
      if (p.webhook_name) webhooks[String(p.webhook_name)] = (webhooks[String(p.webhook_name)] || 0) + 1;
      if (p.curing) curings[String(p.curing)] = (curings[String(p.curing)] || 0) + 1;
      if (ev.entity_kind === "agent" && ev.entity_id) agents[String(ev.entity_id)] = (agents[String(ev.entity_id)] || 0) + 1;
      if (ev.entity_kind === "artifact" && ev.entity_id) artifacts[String(ev.entity_id)] = (artifacts[String(ev.entity_id)] || 0) + 1;
      if (ev.kind && ev.kind.startsWith("tool.")) {
        const name = String(p.tool || "tool");
        tools[name] = (tools[name] || 0) + 1;
      }
      // Register hides from any event that carries a hide_id payload field,
      // including queue.enqueue/dequeue where the curing input hide is identified.
      if (p.hide_id) {
        const id = String(p.hide_id);
        hides[id] = (hides[id] || 0) + 1;
      } else if ((ev.entity_kind === "hide" || ev.kind === "cut.selected") && ev.entity_id) {
        hides[String(ev.entity_id)] = (hides[String(ev.entity_id)] || 0) + 1;
      }
      if (ev.kind === "cache.hit") cacheHits++;
      if (ev.kind === "cache.miss") cacheMisses++;
      if (ev.kind === "cache.store") cacheStores++;
    });

    // Detect single-use (ephemeral) queues: any queue whose name contains a known
    // hide_id is a dynamically created single-use queue, not a static named queue.
    const knownHideIds = Object.keys(hides);
    const ephemeralQueues = new Set();
    Object.keys(queues).forEach(qName => {
      for (const hid of knownHideIds) {
        if (hid && qName.includes(hid)) {
          ephemeralQueues.add(qName);
          break;
        }
      }
    });

    return {
      queues: pairs(queues),
      workers: pairs(workers),
      webhooks: pairs(webhooks),
      curings: pairs(curings),
      agents: pairs(agents),
      tools: pairs(tools),
      artifacts: pairs(artifacts),
      hides: pairs(hides),
      ephemeralQueues,
      cacheActive: (cacheHits + cacheMisses + cacheStores) > 0,
      cacheHitRate: cacheHits + cacheMisses > 0 ? cacheHits / (cacheHits + cacheMisses) : null,
      cacheTotal: cacheHits + cacheMisses + cacheStores,
    };
  }

  function mergeDirectory(model, directory) {
    if (!directory) return model;
    const sections = ["queues", "workers", "webhooks", "curings", "agents", "hides", "artifacts"];
    sections.forEach(section => {
      const dir = directory[section];
      if (!Array.isArray(dir)) return;
      const byId = {};
      (model[section] || []).forEach(r => { byId[r.id] = r; });
      dir.forEach(entry => {
        const existing = byId[entry.id];
        if (existing) {
          existing.status = entry.status || existing.status;
          existing.count = (existing.count || 0) + (entry.count || 0);
        } else {
          model[section] = (model[section] || []).concat([{ id: entry.id, count: entry.count || 0, status: entry.status }]);
          byId[entry.id] = model[section][model[section].length - 1];
        }
      });
    });
    if (directory.toolsets && Array.isArray(directory.toolsets)) {
      model.toolsets = directory.toolsets.map(t => ({ id: t.id, count: t.count || 0, status: t.status }));
    }
    if (directory.cache) {
      model.cacheHitRate = directory.cache.hitRate;
    }
    return model;
  }

  function pairs(map) {
    return Object.keys(map)
      .map(k => ({ id: k, count: map[k] }))
      .sort((a, b) => b.count - a.count);
  }

  function rootSection(title, status, kind, count, blocks) {
    const stateCls = statusClass(status);
    return "<div class=\"dt-tree-section\">"
      + "<div class=\"dt-tree-item dt-tree-root\"><div class=\"dt-tree-row\"><span class=\"dt-tree-label\"><span class=\"dt-tree-icon\">" + DtIcons.iconForEntity(kind) + "</span><span>" + esc(title) + "</span></span><span class=\"dt-tree-right\"><span class=\"dt-tree-status-dot " + stateCls + "\"></span><span class=\"dt-tree-status " + stateCls + "\">" + esc(status) + "</span><span class=\"dt-badge\">" + count + "</span></span></div></div>"
      + blocks.join("")
      + "</div>";
  }

  function statusClass(status) {
    const s = (status || "").toLowerCase();
    if (s.indexOf("hit") >= 0) return "dt-state-live";
    switch (s) {
      case "running":
      case "active":
      case "live":
      case "stored":
      case "buffered":
      case "warm":
      case "busy":
        return "dt-state-live";
      case "scheduled":
        return "dt-state-warn";
      case "idle":
        return "dt-state-muted";
      case "error":
      case "failed":
        return "dt-state-err";
      default:
        return "dt-state-warn";
    }
  }

  function branch(title, kind, rows, selectedKey) {
    const shown = rows.filter(r => !query || r.id.toLowerCase().indexOf(query.toLowerCase()) >= 0).slice(0, 7);
    const leaves = shown.map(row => {
      const key = kind + ":" + row.id;
      const active = key === selectedKey ? " active" : "";
      return "<div class=\"dt-tree-item dt-tree-leaf" + active + "\" data-kind=\"" + esc(kind) + "\" data-id=\"" + esc(row.id) + "\">"
        + "<div class=\"dt-tree-row\"><span class=\"dt-tree-label\"><span class=\"dt-tree-icon\">" + DtIcons.iconForEntity(kind) + "</span><span>" + esc(row.id) + "</span></span><span class=\"dt-badge\">" + row.count + "</span></div>"
        + "</div>";
    }).join("");
    return "<div class=\"dt-tree-sub\">"
      + "<div class=\"dt-tree-item dt-tree-branch\"><div class=\"dt-tree-row\"><span class=\"dt-tree-label\"><span class=\"dt-tree-icon\">" + DtIcons.iconForEntity(kind) + "</span><span>" + esc(title) + "</span></span><span class=\"dt-badge\">" + rows.length + "</span></div></div>"
      + leaves
      + "</div>";
  }

  // branchEphemeral renders single-use (ephemeral) queues grouped by their
  // queue-name prefix (the part before the embedded hide_id). This groups all
  // instances of the same logical queue type together, e.g.:
  //   pr-meta (2)
  //     └── …a3f2   (event 1)
  //     └── …b8d9   (event 2)
  //   pr-diff (2)
  //     └── …
  function branchEphemeral(title, rows, selectedKey) {
    if (!rows.length) return "";

    // Split each queue name into (prefix, hideId) on the first "hide_" marker.
    const groups = {}; // prefix → [{ id, count, hideId }]
    const ungrouped = [];
    rows.forEach(row => {
      const hideStart = row.id.indexOf("hide_");
      if (hideStart >= 0) {
        const hideId = row.id.slice(hideStart);
        const prefix = row.id.slice(0, hideStart).replace(/-$/, "") || "(unnamed)";
        if (!groups[prefix]) groups[prefix] = [];
        groups[prefix].push({ id: row.id, count: row.count, hideId });
      } else {
        ungrouped.push(row);
      }
    });

    const qFilter = query ? query.toLowerCase() : "";

    let html = "<div class=\"dt-tree-sub\">"
      + "<div class=\"dt-tree-item dt-tree-branch\"><div class=\"dt-tree-row\">"
      + "<span class=\"dt-tree-label\"><span class=\"dt-tree-icon\">\u2261</span><span>" + esc(title) + "</span></span>"
      + "<span class=\"dt-badge\">" + rows.length + "</span>"
      + "</div></div>";

    // One sub-branch per prefix, sorted alphabetically.
    Object.keys(groups).sort().forEach(prefix => {
      const members = groups[prefix];
      const shown = members.filter(r => !qFilter
        || r.id.toLowerCase().indexOf(qFilter) >= 0
        || prefix.toLowerCase().indexOf(qFilter) >= 0);
      if (!shown.length) return;

      const leavesHtml = shown.map(row => {
        const key = "queue:" + row.id;
        const active = key === selectedKey ? " active" : "";
        // Show the trailing uniquifier of the hide_id (last 12 chars) as the leaf label.
        const leafLabel = row.hideId.length > 16 ? "\u2026" + row.hideId.slice(-12) : row.hideId;
        return "<div class=\"dt-tree-item dt-tree-leaf dt-tree-ephemeral" + active
          + "\" data-kind=\"queue\" data-id=\"" + esc(row.id) + "\" title=\"" + esc(row.id) + "\">"
          + "<div class=\"dt-tree-row\"><span class=\"dt-tree-label\">"
          + "<span class=\"dt-tree-icon\">\u2261</span>"
          + "<span class=\"dt-tree-ephemeral-event-id\">" + esc(leafLabel) + "</span>"
          + "</span><span class=\"dt-badge\">" + row.count + "</span></div>"
          + "</div>";
      }).join("");

      html += "<div class=\"dt-tree-sub dt-tree-ephemeral-group\">"
        + "<div class=\"dt-tree-item dt-tree-branch dt-tree-ephemeral-header\">"
        + "<div class=\"dt-tree-row\"><span class=\"dt-tree-label\">"
        + "<span class=\"dt-tree-icon\">\u22d8</span>"
        + "<span>" + esc(prefix) + "</span>"
        + "</span><span class=\"dt-badge\">" + shown.length + "</span></div></div>"
        + leavesHtml
        + "</div>";
    });

    // Queues that don't embed a hide_id fall through as plain leaves.
    ungrouped
      .filter(r => !qFilter || r.id.toLowerCase().indexOf(qFilter) >= 0)
      .slice(0, 7)
      .forEach(row => {
        const key = "queue:" + row.id;
        const active = key === selectedKey ? " active" : "";
        const label = row.id.length > 32 ? row.id.slice(0, 16) + "\u2026" + row.id.slice(-10) : row.id;
        html += "<div class=\"dt-tree-item dt-tree-leaf dt-tree-ephemeral" + active
          + "\" data-kind=\"queue\" data-id=\"" + esc(row.id) + "\" title=\"" + esc(row.id) + "\">"
          + "<div class=\"dt-tree-row\"><span class=\"dt-tree-label\">"
          + "<span class=\"dt-tree-icon\">\u2261</span><span>" + esc(label) + "</span>"
          + "</span><span class=\"dt-badge\">" + row.count + "</span></div>"
          + "</div>";
      });

    html += "</div>";
    return html;
  }

  function leaves(kind, rows, selectedKey, state) {
    if (!rows.length) {
      return "<div class=\"dt-tree-sub\"><div class=\"dt-tree-item dt-tree-empty\">No items</div></div>";
    }
    // Build set of member IDs for the highlighted curing, so agents/queues/tools in that curing get a membership indicator
    const curingHl = state && state.treeHighlight && state.treeHighlight.kind === "curing" ? state.treeHighlight.id : null;
    const curingMembers = curingHl ? buildCuringMembers(state.events || [], kind, curingHl) : null;
    return "<div class=\"dt-tree-sub\">" + rows.slice(0, 12).map(row => {
      const key = kind + ":" + row.id;
      const active = key === selectedKey ? " active" : "";
      const hl = state && state.treeHighlight && state.treeHighlight.kind === kind && state.treeHighlight.id === row.id;
      const filt = state && state.treeFilter && state.treeFilter.kind === kind && state.treeFilter.id === row.id;
      const isMember = curingMembers && curingMembers.has(row.id);
      const extraCls = hl ? " hl" : (filt ? " filtered" : (isMember ? " dt-curing-member" : ""));
      const status = row.status ? "<span class=\"dt-tree-status-dot " + statusClass(row.status) + "\"></span><span class=\"dt-tree-status " + statusClass(row.status) + "\">" + esc(row.status) + "</span>" : "";
      const memberDot = isMember ? "<span class=\"dt-curing-member-dot\" title=\"part of curing: " + esc(curingHl) + "\"></span>" : "";
      return "<div class=\"dt-tree-item dt-tree-leaf" + active + extraCls + "\" data-kind=\"" + esc(kind) + "\" data-id=\"" + esc(row.id) + "\">"
        + "<div class=\"dt-tree-row\"><span class=\"dt-tree-label\"><span class=\"dt-tree-icon\">" + DtIcons.iconForEntity(kind) + "</span><span>" + esc(row.id) + "</span></span><span class=\"dt-tree-right\">" + memberDot + status + "<span class=\"dt-badge\">" + (row.count || 0) + "</span></span></div>"
        + "</div>";
    }).join("") + "</div>";
  }

  function buildCuringMembers(events, kind, curingId) {
    const members = new Set();
    events.forEach(ev => {
      const p = ev.payload || {};
      if (String(p.curing || "") !== curingId) return;
      if (kind === "agent" && (ev.entity_kind === "agent" || p.agent)) members.add(ev.entity_id || p.agent || "");
      if (kind === "queue" && (p.queue || p.dest_queue)) {
        if (p.queue) members.add(p.queue);
        if (p.dest_queue) members.add(p.dest_queue);
      }
      if (kind === "hide" && p.hide_id) members.add(p.hide_id);
    });
    members.delete("");
    return members;
  }

  function esc(s) {
    return ltEscapeHtml(s == null ? "" : String(s));
  }

  function latestEventForEntity(events, kind, id) {
    if (!events || !events.length) return null;
    const filtered = events.filter(ev => {
      const p = ev.payload || {};
      if (kind === "tool") {
        return (ev.kind || "").startsWith("tool.") && String(p.tool || "") === id;
      }
      if (ev.entity_id !== id) return false;
      if (!kind) return true;
      return ev.entity_kind === kind;
    });
    if (!filtered.length) return null;
    return filtered[filtered.length - 1];
  }

  return { render };
})();
