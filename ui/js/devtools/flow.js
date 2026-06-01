"use strict";

const DtFlow = (() => {
  const KIND_ORDER  = ["http", "webhook", "queue", "curing", "hide", "agent", "tool", "artifact"];
  const KIND_LABELS = {
    http: "HTTP", webhook: "Webhook", queue: "Queue", curing: "Curing",
    hide: "Hide",  agent: "Agent",    tool: "Tool",   artifact: "Artifact",
  };
  // Fixed: http and agent were both #57d4ff. http is now slate, agent is cornflower blue.
  const KIND_COLORS = {
    http:     "#94a3b8",
    webhook:  "#ffd26f",
    queue:    "#a78bfa",
    curing:   "#34d399",
    hide:     "#fb923c",
    agent:    "#60a5fa",
    tool:     "#f87171",
    artifact: "#4ade80",
  };
  const NODE_W   = 148;
  const NODE_H   = 48;
  const COL_GAP  = 56;
  const ROW_GAP  = 10;
  const LANE_GAP = 14;
  const LANE_PT  = 22;   // top padding inside lane band (room for label)
  const LANE_PB  = 12;   // bottom padding inside lane band
  const LABEL_W  = 84;   // left margin reserved for lane label text
  const PAD_X    = 14;
  const PAD_Y    = 14;
  const ARROW      = "dt-flarw";
  const AGENT_ROW_H = 18;  // height per agent chip row inside an expanded curing node

  // ── public entry point ─────────────────────────────────────────────────────
  function flowToolbar() {
    return "<div class=\"dt-flow-toolbar\">"
      + "<button class=\"dt-flow-btn dt-flow-btn-zoomin\"  title=\"Zoom in\">+</button>"
      + "<button class=\"dt-flow-btn dt-flow-btn-zoomout\" title=\"Zoom out\">\u2212</button>"
      + "<button class=\"dt-flow-btn dt-flow-btn-fit\"     title=\"Fit diagram to view\">Fit</button>"
      + "<button class=\"dt-flow-btn dt-flow-btn-reset\"   title=\"Reset to 1:1\">1\u00a0:\u00a01</button>"
      + "</div>";
  }

  function render(container, state) {
    const model = buildFlowModel(state.events || []);
    if (!model.nodes.length) {
      container.innerHTML = flowToolbar()
        + "<div class=\"dt-empty-state\">"
        + "<div class=\"dt-empty-title\">No pipeline events yet</div>"
        + "<div class=\"dt-empty-body\">The flow diagram populates as events arrive.</div>"
        + "</div>";
      initViewport(container);
      return;
    }

    // Legend — only kinds that appear, in KIND_ORDER sequence.
    const usedKinds = KIND_ORDER.filter(k => model.nodes.some(n => n.kind === k));
    const legend = "<div class=\"dt-flow-legend\">"
      + usedKinds.map(k =>
          "<span class=\"dt-flow-legend-item\">"
          + "<span class=\"dt-flow-legend-dot\" style=\"background:" + KIND_COLORS[k] + "\"></span>"
          + ltEscapeHtml(KIND_LABELS[k] || k)
          + "</span>").join("")
      + "</div>";

    container.innerHTML = legend + flowToolbar()
      + "<div class=\"dt-flow-viewport\">"
      + "<div class=\"dt-flow-wrap\">" + renderSVG(model, state) + "</div>"
      + "</div>";
    bindNodes(container, state);
    initViewport(container);
  }

  // ── model builder ──────────────────────────────────────────────────────────
  function buildFlowModel(events) {
    const nodes          = [];
    const edges          = [];
    const seen           = {};
    const hideIds        = new Set();
    const rootHideIds    = new Set();
    const ephemeralNodeIds = new Set();
    const lanes          = [];   // ordered [{ id, label }]
    const laneSet        = new Set();

    // ── Pass 0: curing→agent and agent→tool relationships ─────────────────────
    const curingAgentNames = {};  // curingName → Set<agentName>
    const agentToolNames   = {};  // agentName  → Set<toolName>
    events.forEach(ev => {
      const p0 = ev.payload || {};
      // Any event that carries both curing + agent establishes the relationship.
      // Live runner events (agent.response, tool.call, tool.result, etc.) always
      // include both; demo events may carry them on curing.step.complete.
      if (p0.curing && p0.agent) {
        (curingAgentNames[p0.curing] = curingAgentNames[p0.curing] || new Set()).add(p0.agent);
      }
      if (p0.agent && p0.tool) {
        (agentToolNames[p0.agent] = agentToolNames[p0.agent] || new Set()).add(p0.tool);
      }
    });

    // ── Pass 1: root hide IDs and lane order ──────────────────────────────────
    events.forEach(ev => {
      const p = ev.payload || {};
      if (p.hide_id) hideIds.add(String(p.hide_id));
      if (ev.kind === "webhook.received" && p.hide_id) {
        const lid = String(p.hide_id);
        rootHideIds.add(lid);
        if (!laneSet.has(lid)) {
          laneSet.add(lid);
          const wname = p.webhook_name || "webhook";
          const tsM   = lid.match(/(\d{8})_(\d{4})/);
          const tsStr = tsM
            ? " \u00b7 " + tsM[1].slice(0,4) + "-" + tsM[1].slice(4,6) + "-" + tsM[1].slice(6) + " " + tsM[2].slice(0,2) + ":" + tsM[2].slice(2)
            : "";
          lanes.push({ id: lid, label: wname + " #" + (lanes.length + 1) + tsStr });
        }
      }
    });

    // ── Pass 2: consumer map (queue → curing that dequeues from it) ───────────
    // Distinguishes enqueue SOURCE curing (producer) from TARGET curing (consumer).
    const consumers = {};
    events.forEach(ev => {
      if (ev.kind !== "queue.dequeue") return;
      const p = ev.payload || {};
      const q = p.queue || p.dest_queue || "";
      const c = p.curing || p.worker || "";
      if (q && c) consumers[q] = c;
    });

    // ── Helpers ────────────────────────────────────────────────────────────────
    function isEphemeralQueue(name) {
      for (const hid of hideIds) {
        if (hid && name.includes(hid)) return true;
      }
      return false;
    }

    function laneOf(p) {
      if (!rootHideIds.size) return null;
      const hid = p.hide_id ? String(p.hide_id) : "";
      if (hid && rootHideIds.has(hid)) return hid;
      const q = p.dest_queue || p.queue || "";
      for (const rid of rootHideIds) {
        if (rid && q.includes(rid)) return rid;
      }
      return null;
    }

    function sid(baseId, laneId) {
      return laneId ? baseId + "@" + laneId : baseId;
    }

    // Strip embedded hide_id from queue names so labels remain readable.
    function queueLabel(name) {
      for (const hid of rootHideIds) {
        const idx = name.indexOf(hid);
        if (idx > 0) return name.slice(0, idx).replace(/[\/_-]+$/, "");
      }
      for (const hid of hideIds) {
        if (rootHideIds.has(hid)) continue;
        const idx = name.indexOf(hid);
        if (idx > 0) return name.slice(0, idx).replace(/[\/_-]+$/, "");
      }
      return shortLabel(name, 15);
    }

    function addNode(baseId, kind, label, seq, laneId) {
      const fid = sid(baseId, laneId);
      if (seen[fid]) return fid;
      seen[fid] = true;
      // fullLabel: human-readable tooltip (strip type prefix, unscoped).
      const fullLabel = baseId.replace(/^[^:]+:/, "");
      nodes.push({ id: fid, kind, label, fullLabel, seq, lane: laneId });
      return fid;
    }
    function addEdge(from, to) {
      if (from && to && from !== to) edges.push({ from, to });
    }
    function shortLabel(s, max) {
      return s && s.length > max ? s.slice(0, max - 1) + "\u2026" : (s || "");
    }

    // ── Pass 3: build nodes and edges ──────────────────────────────────────────
    events.forEach(ev => {
      const p    = ev.payload || {};
      const seq  = ev.seq || 0;
      const lane = laneOf(p);

      // HTTP ──────────────────────────────────────────────────────────────────
      if (ev.kind === "http.request") {
        addNode("http:" + (p.path || "http"), "http",
          (p.method || "GET") + " " + shortLabel(p.path || "-", 11), seq, lane);
      }

      // Webhook ───────────────────────────────────────────────────────────────
      if (ev.kind === "webhook.received") {
        const name = p.webhook_name || ev.entity_id || "webhook";
        addNode("wh:" + name, "webhook", shortLabel(name, 15), seq, lane);
      }

      // Queue enqueue ─────────────────────────────────────────────────────────
      if (ev.kind === "queue.enqueue") {
        const q    = p.dest_queue || p.queue || "queue";
        const qnid = addNode("q:" + q, "queue", queueLabel(q), seq, lane);
        if (isEphemeralQueue(q)) ephemeralNodeIds.add(qnid);

        // Add curing→queue edge only when p.curing is the PRODUCER (not consumer).
        // When p.curing === consumers[q] the curing is the consumer side (wrong dir).
        if (p.curing && consumers[q] !== p.curing) {
          const baseId     = "c:" + p.curing;
          const preferredId = sid(baseId, lane);
          if (seen[preferredId]) {
            addEdge(preferredId, qnid);
          } else {
            // The enqueue event may carry no hide_id (e.g. decision → comments-in),
            // so laneOf() returns null and the lane-scoped curing node isn't found.
            // Find every existing curing node with this name across all lanes and
            // connect each to the queue (handles both serial and parallel fan-out).
            const matches = nodes.filter(n => n.id === baseId || n.id.startsWith(baseId + "@"));
            if (matches.length > 0) {
              matches.forEach(n => addEdge(n.id, qnid));
            } else {
              addEdge(preferredId, qnid); // forward-reference: resolved later
            }
          }
        }
      }

      // Queue dequeue ─────────────────────────────────────────────────────────
      if (ev.kind === "queue.dequeue") {
        const q    = p.queue || p.dest_queue || "queue";
        const qnid = addNode("q:" + q, "queue", queueLabel(q), seq, lane);
        if (isEphemeralQueue(q)) ephemeralNodeIds.add(qnid);
        const cname = p.curing || p.worker;
        if (cname) {
          const cnid = addNode("c:" + cname, "curing", shortLabel(cname, 15), seq, lane);
          addEdge(qnid, cnid);
        }
      }

      // Curing lifecycle events ───────────────────────────────────────────────
      if (ev.kind === "curing.start" || ev.kind === "curing.complete" || ev.kind === "curing.step") {
        const c = p.curing || ev.entity_id || "curing";
        addNode("c:" + c, "curing", shortLabel(c, 15), seq, lane);
      }

      // Agent events ──────────────────────────────────────────────────────────
      if (ev.kind === "agent.operation.start" || ev.kind === "agent.run" || ev.kind === "agent.response") {
        const aname = p.agent || ev.entity_id || "agent";
        const anid  = addNode("ag:" + aname, "agent", shortLabel(aname, 15), seq, lane);
        if (p.curing) addEdge(sid("c:" + p.curing, lane), anid);
      }

      // Hide cuts → agent ─────────────────────────────────────────────────────
      if (ev.kind === "hide.cut" || ev.kind === "cut.selected") {
        if (p.hide_id) {
          const hnid  = addNode("hide:" + p.hide_id, "hide",
            shortLabel(p.hide_id.replace(/^hide_/, ""), 14), seq, lane);
          const aname = ev.entity_id || p.agent || "";
          if (aname) {
            const anid = addNode("ag:" + aname, "agent", shortLabel(aname, 15), seq, lane);
            addEdge(hnid, anid);
          }
        }
      }

      // Tool calls: agent → tool ──────────────────────────────────────────────
      if (ev.kind === "tool.call") {
        const tname  = p.tool || "tool";
        const server = p.server || p.mcp_server || "";
        const tnid   = addNode("tool:" + tname + (server ? "@" + server : ""),
          "tool", shortLabel(tname, 15), seq, lane);
        const aname  = p.agent || (ev.entity_kind === "agent" ? ev.entity_id : "");
        if (aname) addEdge(sid("ag:" + aname, lane), tnid);
      }

      // Artifacts ─────────────────────────────────────────────────────────────
      if (ev.kind === "artifact.written") {
        const artname = p.artifact || ev.entity_id || "artifact";
        const artid   = addNode("art:" + artname, "artifact", shortLabel(artname, 15), seq, lane);
        const src = p.agent  ? sid("ag:" + p.agent, lane)
                  : p.curing ? sid("c:" + p.curing, lane)
                  : null;
        if (src) addEdge(src, artid);
      }
    });

    // ── Post-processing: webhook → initial queues ─────────────────────────────
    // Per-route enqueue events from the webhook handler don't carry webhook_name,
    // so we can't build that edge during the main pass. Fix: connect each
    // lane's webhook node to any queue node in that lane with no incoming edges.
    if (lanes.length > 0) {
      const hasIncoming = new Set(edges.map(e => e.to));
      lanes.forEach(lane => {
        const whNode = nodes.find(n => n.kind === "webhook" && n.lane === lane.id);
        if (!whNode) return;
        nodes.forEach(n => {
          if (n.kind !== "queue") return;
          if (n.lane !== lane.id) return;
          if (hasIncoming.has(n.id)) return;
          addEdge(whNode.id, n.id);
        });
      });
    }

    // ── Ghost-edge resolver ──────────────────────────────────────────────────
    // Forward-reference edges arise when a curing's enqueue event fires before
    // the curing's own lane-scoped node has been created (parallel processing).
    // E.g. "c:decision → q:comments-in" where only "c:decision@lane1" exists.
    // Replace each such ghost edge with edges from every matching lane-scoped node.
    if (rootHideIds.size > 0) {
      const resolved = [];
      const ghosts   = [];
      edges.forEach(e => (seen[e.from] ? resolved : ghosts).push(e));
      ghosts.forEach(ghost => {
        const base    = ghost.from;
        const matches = nodes.filter(n => n.id === base || n.id.startsWith(base + "@"));
        if (matches.length > 0) {
          matches.forEach(n => resolved.push({ from: n.id, to: ghost.to }));
        } else {
          resolved.push(ghost); // unresolvable; keep as-is
        }
      });
      edges.length = 0;
      resolved.forEach(e => edges.push(e));
    }

    // ── Lane propagation BFS ──────────────────────────────────────────────────
    // Propagate lane assignments forward through edges. If a node has no lane
    // but every assigned upstream predecessor agrees on the same lane, inherit
    // it. Repeat until stable. Ambiguous nodes (multiple upstream lanes) stay
    // in the shared bucket.
    if (rootHideIds.size > 0) {
      const nodeById = {};
      nodes.forEach(n => { nodeById[n.id] = n; });
      const preds = {};
      nodes.forEach(n => { preds[n.id] = []; });
      edges.forEach(e => { if (preds[e.to]) preds[e.to].push(e.from); });
      let changed = true;
      while (changed) {
        changed = false;
        nodes.forEach(n => {
          if (n.lane) return;
          const upLanes = new Set(
            (preds[n.id] || []).map(id => nodeById[id] && nodeById[id].lane).filter(Boolean)
          );
          if (upLanes.size === 1) { n.lane = [...upLanes][0]; changed = true; }
        });
      }
    }

    // ── Annotate curing nodes with child agents (before cloning) ─────────────
    // Done here so that cloned curing nodes inherit childAgents via Object.assign.
    nodes.forEach(n => {
      if (n.kind !== "curing") return;
      n.childAgents = [...(curingAgentNames[n.fullLabel] || [])].map(a => ({
        name: a,
        tools: [...(agentToolNames[a] || [])],
      }));
    });

    // ── Clone multi-lane shared nodes into each upstream lane ─────────────────
    // Nodes fed by multiple lanes (e.g. comments-in receiving enqueues from both
    // github#1 and github#2 decisions) are duplicated per upstream lane so they
    // appear in every swimlane instead of the shared bucket. Cascades until stable.
    if (rootHideIds.size > 0) {
      let stable = false;
      while (!stable) {
        stable = true;
        const nb = {};
        nodes.forEach(n => { nb[n.id] = n; });
        const preds2 = {};
        nodes.forEach(n => { preds2[n.id] = []; });
        edges.forEach(e => { if (preds2[e.to]) preds2[e.to].push(e.from); });

        const sharedMulti = nodes.filter(n => {
          if (n.lane) return false;
          const ul = new Set((preds2[n.id] || []).map(id => nb[id] && nb[id].lane).filter(Boolean));
          return ul.size > 1;
        });

        sharedMulti.forEach(orig => {
          const ul = [...new Set((preds2[orig.id] || []).map(id => nb[id] && nb[id].lane).filter(Boolean))];
          const baseId = orig.id.replace(/@[^@]*$/, "");
          const clones = ul.map(lane => Object.assign({}, orig, { id: sid(baseId, lane), lane }));
          clones.forEach(c => { nodes.push(c); nb[c.id] = c; });

          // Re-route each incoming edge to the clone matching the source lane.
          edges.forEach(e => {
            if (e.to !== orig.id) return;
            const srcLane = nb[e.from] && nb[e.from].lane;
            const target  = clones.find(c => c.lane === srcLane) || clones[0];
            e.to = target.id;
          });

          // Duplicate outgoing edges for every clone, then remove the originals.
          const out = edges.filter(e => e.from === orig.id);
          clones.forEach(c => out.forEach(e => edges.push({ from: c.id, to: e.to })));
          for (let i = edges.length - 1; i >= 0; i--) {
            if (edges[i].from === orig.id) edges.splice(i, 1);
          }

          // Remove the original shared node.
          const idx = nodes.indexOf(orig);
          if (idx >= 0) nodes.splice(idx, 1);
          stable = false;
        });
      }

      // Second BFS: pick up any remaining lane-less nodes whose single upstream
      // lane became deterministic only after the cloning above.
      const nb2 = {};
      nodes.forEach(n => { nb2[n.id] = n; });
      let bfsOk = true;
      while (bfsOk) {
        bfsOk = false;
        const preds3 = {};
        nodes.forEach(n => { preds3[n.id] = []; });
        edges.forEach(e => { if (preds3[e.to]) preds3[e.to].push(e.from); });
        nodes.forEach(n => {
          if (n.lane) return;
          const ul = new Set((preds3[n.id] || []).map(id => nb2[id] && nb2[id].lane).filter(Boolean));
          if (ul.size >= 1) { n.lane = [...ul][0]; bfsOk = true; }
        });
      }
    }

    // ── Compute owned node IDs (after cloning) ────────────────────────────────
    // Agent and tool nodes fully represented as chips inside a curing node are
    // excluded from standalone SVG rendering.
    const ownedNodeIds = new Set();
    const allCuringAgentNames = new Set();
    const allCuringToolNames  = new Set();
    nodes.forEach(n => {
      if (n.kind !== "curing" || !n.childAgents) return;
      n.childAgents.forEach(ag => {
        allCuringAgentNames.add(ag.name);
        ag.tools.forEach(t => allCuringToolNames.add(t));
      });
    });
    nodes.forEach(n => {
      if (n.kind === "agent" && allCuringAgentNames.has(n.fullLabel)) ownedNodeIds.add(n.id);
      if (n.kind === "tool"  && allCuringToolNames.has(n.fullLabel.replace(/@.*$/, ""))) ownedNodeIds.add(n.id);
    });

    // ── Deduplicate edges ─────────────────────────────────────────────────────
    const edgeSet = new Set();
    const deduped = edges.filter(e => {
      const k = e.from + "\u2192" + e.to;
      if (edgeSet.has(k)) return false;
      edgeSet.add(k);
      return true;
    });
    return { nodes, edges: deduped, lanes, ephemeralNodeIds, ownedNodeIds, hasSwimlanes: lanes.length > 0 };
  }

  // ── Topological depth (Kahn's BFS) ───────────────────────────────────────
  function computeDepths(nodes, edges) {
    const nodeIds = new Set(nodes.map(n => n.id));
    const depth   = {};
    nodes.forEach(n => { depth[n.id] = 0; });
    const adj   = {};
    const inDeg = {};
    nodes.forEach(n => { inDeg[n.id] = 0; });
    edges.forEach(e => {
      if (!nodeIds.has(e.from) || !nodeIds.has(e.to)) return;
      (adj[e.from] = adj[e.from] || []).push(e.to);
      inDeg[e.to]++;
    });
    const queue = [];
    nodes.forEach(n => { if (inDeg[n.id] === 0) queue.push(n.id); });
    let head = 0;
    while (head < queue.length) {
      const cur = queue[head++];
      for (const next of (adj[cur] || [])) {
        depth[next] = Math.max(depth[next], depth[cur] + 1);
        if (--inDeg[next] === 0) queue.push(next);
      }
    }
    return depth;
  }
  // ── Variable node height ────────────────────────────────────────────────
  // Curing nodes expand vertically to accommodate their agent chip rows.
  function nodeHeight(n) {
    if (n && n.childAgents && n.childAgents.length > 0) {
      return NODE_H + 1 + n.childAgents.length * AGENT_ROW_H;
    }
    return NODE_H;
  }
  // ── SVG renderer dispatcher ───────────────────────────────────────────────
  function renderSVG(model, state) {
    const selSeq = state.selection && state.selection.type === "event"
      ? state.selection.seq : -1;
    const { nodes, edges, lanes, ephemeralNodeIds, ownedNodeIds, hasSwimlanes } = model;
    if (hasSwimlanes && lanes.length > 0) {
      return renderSwim(nodes, edges, lanes, ephemeralNodeIds, ownedNodeIds, selSeq);
    }
    return renderFlat(nodes, edges, ephemeralNodeIds, ownedNodeIds, selSeq);
  }

  // ── Swimlane renderer ─────────────────────────────────────────────────────
  function renderSwim(nodes, edges, lanes, ephemeralNodeIds, ownedNodeIds, selSeq) {
    const ownedIds = ownedNodeIds || new Set();
    const depths = computeDepths(nodes, edges);
    const colW   = NODE_W + COL_GAP;

    // Group nodes by lane, skipping owned (agent/tool) nodes shown inside curings.
    const byLane = {};
    lanes.forEach(l => { byLane[l.id] = []; });
    byLane["__"] = [];
    nodes.forEach(n => {
      if (ownedIds.has(n.id)) return;
      const lid = n.lane || "__";
      (byLane[lid] = byLane[lid] || []).push(n);
    });

    // Only render non-empty lanes.
    const activeLids = [...lanes.map(l => l.id), "__"]
      .filter(lid => (byLane[lid] || []).length > 0);

    const laneLayouts = [];
    let curY = PAD_Y;

    activeLids.forEach(lid => {
      const lnodes    = byLane[lid] || [];
      const isShared  = lid === "__";
      const laneLabel = isShared ? "shared" : (lanes.find(l => l.id === lid) || { label: lid }).label;

      const byDepth = {};
      lnodes.forEach(n => {
        const d = depths[n.id] || 0;
        (byDepth[d] = byDepth[d] || []).push(n);
      });

      // Barycenter sort: order nodes within each depth column by the mean row
      // index of their predecessors to reduce edge crossings (left-to-right pass).
      {
        const baryRow = {};  // nodeId → row index in its (lane, depth) column
        Object.keys(byDepth).map(Number).sort((a, b) => a - b).forEach(d => {
          byDepth[d].sort((a, b) => {
            const mean = n => {
              const rows = edges
                .filter(e => e.to === n.id && baryRow[e.from] !== undefined)
                .map(e => baryRow[e.from]);
              return rows.length ? rows.reduce((s, r) => s + r, 0) / rows.length : null;
            };
            const da = mean(a), db = mean(b);
            if (da !== null && db !== null) return da - db;
            if (da === null && db === null) return (a.seq || 0) - (b.seq || 0);
            return da !== null ? -1 : 1;
          });
          byDepth[d].forEach((n, i) => { baryRow[n.id] = i; });
        });
      }

      // Variable-height columns: sum actual node heights per depth column.
      const laneH = LANE_PT + LANE_PB + Math.max(
        NODE_H,
        ...Object.values(byDepth).map(arr =>
          arr.reduce((sum, n) => sum + nodeHeight(n) + ROW_GAP, -ROW_GAP)
        )
      );

      const nodePos = {};
      Object.entries(byDepth).forEach(([d, arr]) => {
        let rowY = curY + LANE_PT;
        arr.forEach(n => {
          nodePos[n.id] = {
            x: PAD_X + LABEL_W + Number(d) * colW,
            y: rowY,
          };
          rowY += nodeHeight(n) + ROW_GAP;
        });
      });

      laneLayouts.push({ lid, laneLabel, nodePos, y: curY, height: laneH });
      curY += laneH + LANE_GAP;
    });

    const pos    = {};
    laneLayouts.forEach(ll => Object.assign(pos, ll.nodePos));

    const maxDepth = nodes.filter(n => !ownedIds.has(n.id))
      .reduce((m, n) => Math.max(m, depths[n.id] || 0), 0);
    const totalW   = PAD_X * 2 + LABEL_W + (maxDepth + 1) * colW;
    const totalH   = curY - LANE_GAP + PAD_Y;
    const bandW    = totalW - PAD_X * 2 - LABEL_W;

    const bandSVG = laneLayouts.map(ll =>
      "<rect x=\"" + (PAD_X + LABEL_W) + "\" y=\"" + ll.y + "\""
      + " width=\"" + bandW + "\" height=\"" + ll.height + "\""
      + " rx=\"6\" fill=\"rgba(255,255,255,0.018)\""
      + " stroke=\"rgba(120,140,200,0.18)\" stroke-width=\"1\"/>"
      + "<text x=\"" + (PAD_X + LABEL_W - 8) + "\" y=\"" + (ll.y + 14) + "\""
      + " fill=\"" + (ll.lid === "__" ? "rgba(140,160,200,0.35)" : "rgba(180,200,240,0.55)") + "\" font-size=\"10\" font-family=\"monospace\""
      + " text-anchor=\"end\">" + (ll.lid === "__" ? "\u2014 " : "")
      + ltEscapeHtml(ll.laneLabel)
      + "</text>"
    ).join("");

    return "<svg width=\"" + Math.max(520, totalW) + "\""
      + " height=\"" + Math.max(120, totalH) + "\""
      + " viewBox=\"0 0 " + Math.max(520, totalW) + " " + Math.max(120, totalH) + "\""
      + " style=\"display:block;overflow:visible\">"
      + arrowDefs() + bandSVG
      + renderEdges(edges, pos, nodes, ownedIds)
      + renderNodes(nodes, pos, ephemeralNodeIds, ownedIds, selSeq)
      + "</svg>";
  }

  // ── Flat (no-webhook) renderer ────────────────────────────────────────────
  function renderFlat(nodes, edges, ephemeralNodeIds, ownedNodeIds, selSeq) {
    const ownedIds = ownedNodeIds || new Set();
    const byCol = {};
    nodes.forEach(n => {
      if (ownedIds.has(n.id)) return;
      const c = Math.max(0, KIND_ORDER.indexOf(n.kind));
      (byCol[c] = byCol[c] || []).push(n);
    });

    const pos     = {};
    const colKeys = Object.keys(byCol).map(Number).sort((a, b) => a - b);
    let totalW = PAD_X;
    const colX = {};
    colKeys.forEach(c => { colX[c] = totalW; totalW += NODE_W + COL_GAP; });
    totalW = totalW - COL_GAP + PAD_X;

    colKeys.forEach(c => {
      let rowY = PAD_Y;
      byCol[c].forEach(n => {
        pos[n.id] = { x: colX[c], y: rowY };
        rowY += nodeHeight(n) + ROW_GAP;
      });
    });

    const totalH = PAD_Y * 2 + Math.max(
      NODE_H,
      ...colKeys.map(c => byCol[c].reduce((sum, n) => sum + nodeHeight(n) + ROW_GAP, -ROW_GAP))
    );

    return "<svg width=\"" + Math.max(420, totalW) + "\""
      + " height=\"" + Math.max(120, totalH) + "\""
      + " viewBox=\"0 0 " + Math.max(420, totalW) + " " + Math.max(120, totalH) + "\""
      + " style=\"display:block;overflow:visible\">"
      + arrowDefs()
      + renderEdges(edges, pos, nodes, ownedIds)
      + renderNodes(nodes, pos, ephemeralNodeIds, ownedIds, selSeq)
      + "</svg>";
  }

  // ── Shared drawing primitives ─────────────────────────────────────────────
  function arrowDefs() {
    return "<defs>"
      + "<marker id=\"" + ARROW + "\" markerWidth=\"9\" markerHeight=\"7\""
      + " refX=\"8\" refY=\"3.5\" orient=\"auto\">"
      + "<polygon points=\"0 0, 9 3.5, 0 7\" fill=\"#4a6fa5\" opacity=\"0.85\"/>"
      + "</marker>"
      + "</defs>";
  }

  function renderEdges(edges, pos, nodes, ownedNodeIds) {
    const ownedIds = ownedNodeIds || new Set();
    const nodeMap = {};
    nodes.forEach(n => { nodeMap[n.id] = n; });
    return edges.map(e => {
      if (ownedIds.has(e.from) || ownedIds.has(e.to)) return "";
      const from = pos[e.from];
      const to   = pos[e.to];
      if (!from || !to) return "";
      const x1 = from.x + NODE_W;
      const y1 = from.y + NODE_H / 2;
      const x2 = to.x;
      const y2 = to.y + NODE_H / 2;
      const mx = (x1 + x2) / 2;
      const sn = nodeMap[e.from];
      const dn = nodeMap[e.to];
      const crossLane = sn && dn && sn.lane !== dn.lane;
      const opacity = crossLane ? "0.35" : "0.65";
      const dash    = crossLane ? " stroke-dasharray=\"5,3\"" : "";
      return "<path d=\"M" + x1 + "," + y1
        + " C" + mx + "," + y1 + " " + mx + "," + y2 + " " + x2 + "," + y2 + "\""
        + " fill=\"none\" stroke=\"#4a6fa5\" stroke-width=\"1.5\" opacity=\"" + opacity + "\""
        + dash
        + " marker-end=\"url(#" + ARROW + ")\"/>";
    }).join("");
  }

  function renderNodes(nodes, pos, ephemeralNodeIds, ownedNodeIds, selSeq) {
    const ephIds   = ephemeralNodeIds || new Set();
    const ownedIds = ownedNodeIds || new Set();
    return nodes.map(n => {
      if (ownedIds.has(n.id)) return "";  // rendered as chip inside parent curing
      const p = pos[n.id];
      if (!p) return "";
      const color       = KIND_COLORS[n.kind] || "#91a9c8";
      const isActive    = n.seq === selSeq;
      const isEphemeral = ephIds.has(n.id);
      const nh          = nodeHeight(n);
      const stroke      = isActive ? color
                        : isEphemeral ? "rgba(167,139,250,0.55)"
                        : "rgba(96,147,205,0.3)";
      const strokeW     = isActive ? 2 : 1;
      const strokeDash  = isEphemeral && !isActive ? "stroke-dasharray=\"5,3\"" : "";
      const fill        = isActive ? "rgba(87,212,255,0.08)"
                        : isEphemeral ? "rgba(167,139,250,0.04)"
                        : "#121b2f";
      const labelColor  = isEphemeral ? "#b5a0ff" : color;

      // Agent chips rendered inside expanded curing nodes.
      let chips = "";
      if (n.kind === "curing" && n.childAgents && n.childAgents.length > 0) {
        chips += "<line x1=\"" + (p.x + 1) + "\" y1=\"" + (p.y + NODE_H) + "\""
          + " x2=\"" + (p.x + NODE_W - 1) + "\" y2=\"" + (p.y + NODE_H) + "\""
          + " stroke=\"rgba(255,255,255,0.07)\" stroke-width=\"1\"/>";
        n.childAgents.forEach((ag, i) => {
          const ry    = p.y + NODE_H + 1 + i * AGENT_ROW_H;
          const agCol = KIND_COLORS.agent;
          const tlCol = KIND_COLORS.tool;
          const aLbl  = ag.name.length > 13 ? ag.name.slice(0, 12) + "\u2026" : ag.name;
          const tPart = ag.tools.length
            ? " \u2192 " + ag.tools.slice(0, 2).join(", ") + (ag.tools.length > 2 ? "\u2026" : "")
            : "";
          const tLbl  = tPart.length > 18 ? tPart.slice(0, 17) + "\u2026" : tPart;
          // approx x offset for tool text: 13px left pad + ~5.5px per char of agent name
          const tX    = p.x + 13 + Math.min(ag.name.length, 13) * 5.5;
          chips += "<g class=\"dt-flow-agent-chip\""
            + " data-agent=\"" + ltEscapeHtml(ag.name) + "\""
            + " style=\"cursor:pointer;\">"
            // invisible hit target
            + "<rect x=\"" + p.x + "\" y=\"" + ry + "\""
            + " width=\"" + NODE_W + "\" height=\"" + AGENT_ROW_H + "\" fill=\"transparent\"/>"
            // left accent bar
            + "<rect x=\"" + (p.x + 6) + "\" y=\"" + (ry + 4) + "\""
            + " width=\"2\" height=\"" + (AGENT_ROW_H - 8) + "\""
            + " rx=\"1\" fill=\"" + agCol + "\" opacity=\"0.75\"/>"
            // agent name
            + "<text x=\"" + (p.x + 13) + "\" y=\"" + (ry + 12) + "\""
            + " fill=\"" + agCol + "\" font-size=\"9\" font-family=\"monospace\">"
            + ltEscapeHtml(aLbl) + "</text>"
            // tool names
            + (ag.tools.length
              ? "<text x=\"" + tX + "\" y=\"" + (ry + 12) + "\""
                + " fill=\"" + tlCol + "\" font-size=\"9\" font-family=\"monospace\">"
                + ltEscapeHtml(tLbl) + "</text>"
              : "")
            + "</g>";
        });
      }

      return "<g class=\"dt-flow-node\" data-id=\"" + ltEscapeHtml(n.id) + "\""
        + " data-seq=\"" + n.seq + "\" style=\"cursor:pointer\">"
        + "<rect x=\"" + p.x + "\" y=\"" + p.y + "\""
        + " width=\"" + NODE_W + "\" height=\"" + nh + "\""
        + " rx=\"6\" fill=\"" + fill + "\" stroke=\"" + stroke
        + "\" stroke-width=\"" + strokeW + "\" " + strokeDash + "/>"
        + "<text x=\"" + (p.x + 8) + "\" y=\"" + (p.y + 16) + "\""
        + " fill=\"" + labelColor + "\" font-size=\"10\" font-family=\"monospace\">"
        + ltEscapeHtml(n.kind.toUpperCase()) + "</text>"
        + "<text x=\"" + (p.x + 8) + "\" y=\"" + (p.y + 34) + "\""
        + " fill=\"#e9f2ff\" font-size=\"13\" font-family=\"system-ui,sans-serif\">"
        + ltEscapeHtml(n.label) + "</text>"
        + chips
        + "</g>";
    }).join("");
  }

  // ── Event binding ─────────────────────────────────────────────────────────
  function bindNodes(container, state) {
    // ── Tooltip ──────────────────────────────────────────────────────────────
    let tip = document.getElementById("dt-flow-tip");
    if (!tip) {
      tip = document.createElement("div");
      tip.id = "dt-flow-tip";
      tip.style.cssText = [
        "position:fixed", "display:none", "pointer-events:none", "z-index:9999",
        "background:#1a2540", "color:#e9f2ff",
        "border:1px solid rgba(96,147,205,0.45)", "border-radius:5px",
        "padding:4px 10px", "font-family:monospace", "font-size:11px",
        "max-width:480px", "word-break:break-all", "line-height:1.5",
        "box-shadow:0 3px 12px rgba(0,0,0,0.5)",
      ].join(";");
      document.body.appendChild(tip);
    }

    function moveTip(e) {
      tip.style.left = (e.clientX + 14) + "px";
      tip.style.top  = (e.clientY - 8)  + "px";
    }

    // ── Modal helpers ─────────────────────────────────────────────────────────
    // Map event kind → accent colour for modal event rows.
    function kindColor(kind) {
      if (kind.startsWith("webhook")) return KIND_COLORS.webhook;
      if (kind.startsWith("queue"))   return KIND_COLORS.queue;
      if (kind.startsWith("agent"))   return KIND_COLORS.agent;
      if (kind.startsWith("tool"))    return KIND_COLORS.tool;
      if (kind.startsWith("artifact"))return KIND_COLORS.artifact;
      if (kind.startsWith("hide") || kind.startsWith("cut")) return KIND_COLORS.hide;
      if (kind.startsWith("curing"))  return KIND_COLORS.curing;
      return "#94a3b8";
    }

    // One-line human summary for a raw event.
    function eventSummary(ev) {
      const p = ev.payload || {};
      const k = ev.kind || "";
      if (k === "webhook.received")
        return (p.webhook_name || "") + " \u2190 " + (p.path || "");
      if (k === "queue.enqueue")
        return "\u2192 " + shortQueueName(p.dest_queue || p.queue || "");
      if (k === "queue.dequeue")
        return "\u2190 " + shortQueueName(p.queue || "") + (p.curing ? "  (" + p.curing + ")" : "");
      if (k === "agent.run" || k === "agent.response")
        return (p.agent || "") + (p.response ? ": " + String(p.response).slice(0, 100) : "");
      if (k === "tool.call")
        return (p.tool || "") + "(" + String(p.args || "").slice(0, 80) + ")";
      if (k === "tool.result")
        return (p.tool || "") + " \u2192 " + (p.error ? "\u2717 " + p.error : (p.result_preview || "ok"));
      if (k === "artifact.written")
        return p.artifact || "";
      if (k === "curing.start" || k === "curing.complete" || k === "curing.step")
        return p.curing || ev.entity_id || "";
      const skip = new Set(["curing","agent","tool","queue","dest_queue","webhook_name","path","hide_id"]);
      const extras = Object.entries(p)
        .filter(([k2]) => !skip.has(k2))
        .slice(0, 3).map(([k2, v]) => k2 + "=" + String(v).slice(0, 24)).join("  ");
      return extras || k;
    }

    // Collect events related to the clicked node.
    function relatedEvents(pfx, name, seq) {
      return (state.events || []).filter(ev => {
        const p = ev.payload || {};
        if (pfx === "c")    return p.curing === name || (ev.entity_kind === "curing" && ev.entity_id === name);
        if (pfx === "ag")   return p.agent  === name || (ev.entity_kind === "agent"  && ev.entity_id === name);
        if (pfx === "tool") return p.tool   === name && (ev.kind || "").startsWith("tool.");
        if (pfx === "q")    return p.queue  === name || p.dest_queue === name;
        if (pfx === "wh")   return p.webhook_name === name;
        return ev.seq === seq;
      });
    }

    function pfxToKind(pfx) {
      return { c:"curing", ag:"agent", q:"queue", wh:"webhook",
               tool:"tool", art:"artifact", hide:"hide", http:"http" }[pfx] || pfx;
    }

    // ── Modal open/close ──────────────────────────────────────────────────────
    function closeModal() {
      const m = document.getElementById("dt-flow-modal");
      if (m) m.style.display = "none";
      if (_modalEscCtrl) { _modalEscCtrl.abort(); _modalEscCtrl = null; }
    }

    // Strip embedded hide_ids from a queue name for display (e.g.
    // "analysis/hide_github_pull_request_20260524_1038_7d21" → "analysis").
    function shortQueueName(s) { return s.replace(/\/hide_[^\s/]+/g, ""); }

    function openModal(pfx, name, seq) {
      const kind  = pfxToKind(pfx);
      const evs   = relatedEvents(pfx, name, seq);
      const color = KIND_COLORS[kind] || "#94a3b8";
      const title = shortQueueName(name);

      const rows = evs.map(ev => {
        const ts  = ev.at ? ltFormatTs(ev.at).split(" ")[1] : "--:--";
        const col = kindColor(ev.kind || "");
        const sum = ltEscapeHtml(eventSummary(ev));
        return "<tr style=\"border-bottom:1px solid rgba(40,70,120,0.35)\">"
          + "<td style=\"color:rgba(160,185,230,0.45);font-size:10px;padding:4px 10px;"
          +              "font-family:monospace;white-space:nowrap\">#" + ev.seq + " " + ltEscapeHtml(ts) + "</td>"
          + "<td style=\"color:" + col + ";font-size:10px;padding:4px 10px;"
          +              "font-family:monospace;white-space:nowrap\">" + ltEscapeHtml(ev.kind || "") + "</td>"
          + "<td style=\"color:#c8d8f0;font-size:12px;padding:4px 10px;"
          +              "max-width:340px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap\">" + sum + "</td>"
          + "</tr>";
      }).join("");

      const body = evs.length
        ? "<table style=\"width:100%;border-collapse:collapse\">" + rows + "</table>"
        : "<div style=\"color:rgba(160,185,230,0.35);padding:24px;text-align:center;"
          + "font-family:monospace;font-size:12px\">No events captured for this node</div>";

      const html = "<div id=\"dt-flow-modal-panel\""
        + " style=\"background:#0d1828;border:1px solid rgba(80,120,200,0.4);border-radius:8px;"
        + "width:min(700px,92vw);max-height:72vh;display:flex;flex-direction:column;"
        + "box-shadow:0 12px 48px rgba(0,0,0,0.8)\">"
        // header
        + "<div style=\"display:flex;align-items:center;gap:10px;padding:14px 18px;"
        +             "border-bottom:1px solid rgba(50,80,140,0.45);flex-shrink:0\">"
        + "<span style=\"font-family:monospace;font-size:10px;color:" + color + "\">"
        +   ltEscapeHtml(kind.toUpperCase()) + "</span>"
        + "<span style=\"font-size:16px;font-weight:600;color:#e9f2ff;flex:1;"
        +             "font-family:system-ui,sans-serif\">" + ltEscapeHtml(title) + "</span>"
        + "<span style=\"color:rgba(140,165,220,0.5);font-size:11px;font-family:monospace\">"
        +   evs.length + " event" + (evs.length !== 1 ? "s" : "") + "</span>"
        + "<button id=\"dt-flow-modal-close\""
        +   " style=\"background:none;border:none;color:#6b7fa8;cursor:pointer;font-size:20px;"
        +           "line-height:1;padding:0 4px;margin-left:8px\" title=\"Close (Esc)\">\u00d7</button>"
        + "</div>"
        // scrollable body
        + "<div style=\"overflow-y:auto;flex:1\">" + body + "</div>"
        // footer hint
        + "<div style=\"padding:6px 18px;border-top:1px solid rgba(40,70,120,0.35);flex-shrink:0;"
        +             "color:rgba(130,155,210,0.38);font-size:10px;font-family:monospace\">"
        + "Esc or click outside to close</div>"
        + "</div>";

      let overlay = document.getElementById("dt-flow-modal");
      if (!overlay) {
        overlay = document.createElement("div");
        overlay.id = "dt-flow-modal";
        overlay.style.cssText = [
          "position:fixed", "inset:0", "z-index:9990",
          "display:flex", "align-items:center", "justify-content:center",
          "background:rgba(0,0,0,0.68)",
        ].join(";");
        overlay.onclick = e => { if (e.target === overlay) closeModal(); };
        document.body.appendChild(overlay);
      }
      overlay.innerHTML = html;
      overlay.style.display = "flex";
      document.getElementById("dt-flow-modal-close").onclick = closeModal;

      // Escape key — one AbortController per open so listeners do not
      // accumulate across opens or across SSE-driven re-renders. closeModal
      // aborts it; openModal replaces any previously-installed handler.
      if (_modalEscCtrl) _modalEscCtrl.abort();
      _modalEscCtrl = new AbortController();
      document.addEventListener(
        "keydown",
        e => { if (e.key === "Escape") closeModal(); },
        { signal: _modalEscCtrl.signal },
      );
    }

    // ── Wire nodes ────────────────────────────────────────────────────────────
    container.querySelectorAll(".dt-flow-node").forEach(el => {
      el.onmouseenter = e => {
        const rawId = el.getAttribute("data-id") || "";
        const base  = rawId.replace(/@[^@]+$/, "");
        const name  = base.replace(/^[^:]+:/, "");
        const pfx   = (rawId.match(/^([^:@]+):/) || [])[1] || "";
        tip.textContent = (KIND_LABELS[pfxToKind(pfx)] || pfx) + ": " + shortQueueName(name);
        tip.style.display = "block";
        moveTip(e);
      };
      el.onmousemove  = moveTip;
      el.onmouseleave = () => { tip.style.display = "none"; };
      el.onclick = () => {
        tip.style.display = "none";
        const rawId = el.getAttribute("data-id") || "";
        const seq   = Number(el.getAttribute("data-seq"));
        const base  = rawId.replace(/@[^@]+$/, "");
        const name  = base.replace(/^[^:]+:/, "");
        const pfx   = (rawId.match(/^([^:@]+):/) || [])[1] || "";
        openModal(pfx, name, seq);
      };
    });

    // ── Wire agent chips (inside curing nodes) ────────────────────────────────
    container.querySelectorAll(".dt-flow-agent-chip").forEach(el => {
      el.onmouseenter = e => {
        tip.textContent = "Agent: " + (el.getAttribute("data-agent") || "");
        tip.style.display = "block";
        moveTip(e);
      };
      el.onmousemove  = moveTip;
      el.onmouseleave = () => { tip.style.display = "none"; };
      el.onclick = e => {
        e.stopPropagation();
        tip.style.display = "none";
        openModal("ag", el.getAttribute("data-agent") || "", -1);
      };
    });
  }

  // ── Viewport pan/zoom ─────────────────────────────────────────────────────
  // Transform state persists across re-renders (SSE updates, selection changes).
  let _vpState  = { tx: 0, ty: 0, scale: 1 };
  let _vpClean  = null;   // cleanup fn from previous initViewport call
  // Escape-key AbortController for the node-detail modal. Module-scoped so a
  // re-render (which calls bindNodes again) can abort any stale listener
  // installed by the previous render before adding a fresh one. Was missing
  // entirely, which threw ReferenceError on every bindNodes call and aborted
  // initViewport — breaking zoom buttons, wheel zoom, pan, and tooltips.
  let _modalEscCtrl = null;

  function initViewport(container) {
    if (_vpClean) { _vpClean(); _vpClean = null; }

    const vp   = container.querySelector(".dt-flow-viewport");
    const wrap = container.querySelector(".dt-flow-wrap");
    if (!vp || !wrap) return;

    let { tx, ty, scale } = _vpState;
    let dragging = false, lastX = 0, lastY = 0;
    let alive = true;

    function applyTransform() {
      _vpState = { tx, ty, scale };
      wrap.style.transform = "translate(" + tx + "px," + ty + "px) scale(" + scale + ")";
    }
    applyTransform();   // restore from previous state

    function onWheel(e) {
      if (!alive) return;
      e.preventDefault();
      const rect   = vp.getBoundingClientRect();
      const mx     = e.clientX - rect.left;
      const my     = e.clientY - rect.top;
      const factor = e.deltaY < 0 ? 1.12 : (1 / 1.12);
      const ns     = Math.max(0.08, Math.min(6, scale * factor));
      tx    = mx - (mx - tx) * (ns / scale);
      ty    = my - (my - ty) * (ns / scale);
      scale = ns;
      applyTransform();
    }

    function onMouseDown(e) {
      if (e.button !== 0 || !alive) return;
      dragging = true;
      lastX = e.clientX; lastY = e.clientY;
      vp.classList.add("dragging");
    }
    function onMouseMove(e) {
      if (!dragging || !alive) return;
      tx += e.clientX - lastX;
      ty += e.clientY - lastY;
      lastX = e.clientX; lastY = e.clientY;
      applyTransform();
    }
    function onMouseUp() {
      if (!alive) return;
      dragging = false;
      vp.classList.remove("dragging");
    }

    vp.addEventListener("wheel", onWheel, { passive: false });
    vp.addEventListener("mousedown", onMouseDown);
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup",   onMouseUp);

    const btnIn    = container.querySelector(".dt-flow-btn-zoomin");
    const btnOut   = container.querySelector(".dt-flow-btn-zoomout");
    const btnFit   = container.querySelector(".dt-flow-btn-fit");
    const btnReset = container.querySelector(".dt-flow-btn-reset");

    if (btnIn)  btnIn.onclick  = () => {
      if (!alive) return;
      const cx = vp.clientWidth / 2, cy = vp.clientHeight / 2;
      const ns = Math.min(6, scale * 1.3);
      tx = cx - (cx - tx) * (ns / scale);
      ty = cy - (cy - ty) * (ns / scale);
      scale = ns; applyTransform();
    };
    if (btnOut) btnOut.onclick = () => {
      if (!alive) return;
      const cx = vp.clientWidth / 2, cy = vp.clientHeight / 2;
      const ns = Math.max(0.08, scale / 1.3);
      tx = cx - (cx - tx) * (ns / scale);
      ty = cy - (cy - ty) * (ns / scale);
      scale = ns; applyTransform();
    };
    if (btnReset) btnReset.onclick = () => {
      if (!alive) return;
      tx = 0; ty = 0; scale = 1; applyTransform();
    };
    if (btnFit) btnFit.onclick = () => {
      if (!alive) return;
      const svg = vp.querySelector("svg");
      if (!svg) return;
      const cw = vp.clientWidth  || 600;
      const ch = vp.clientHeight || 400;
      const sw = parseFloat(svg.getAttribute("width"))  || cw;
      const sh = parseFloat(svg.getAttribute("height")) || ch;
      const s  = Math.min(cw / sw, ch / sh) * 0.93;
      scale = s;
      tx = (cw - sw * s) / 2;
      ty = Math.max(8, (ch - sh * s) / 2);
      applyTransform();
    };

    _vpClean = () => {
      alive = false;
      vp.removeEventListener("wheel",     onWheel);
      vp.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup",   onMouseUp);
    };
  }

  return { render };
})();
