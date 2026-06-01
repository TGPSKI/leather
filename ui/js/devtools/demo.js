"use strict";

// Demo data module: synthesizes the rich DevTools event stream + entities
// shown in the design spec, so the UI can render its full intended experience
// even when no backend is producing those event kinds yet.
const DtDemo = (() => {
  const baseTs = Math.floor(Date.now() / 1000);
  let seqCounter = 18540;

  function nextSeq() {
    seqCounter += 1;
    return seqCounter;
  }

  function ts(offsetSeconds) {
    return baseTs + offsetSeconds;
  }

  function ev(kind, source, entity_kind, entity_id, payload, offset) {
    return {
      seq: nextSeq(),
      kind: kind,
      source: source,
      entity_kind: entity_kind,
      entity_id: entity_id,
      at: ts(offset || 0),
      payload: payload || {},
    };
  }

  // Build a CI-gate fan-out causal chain showing single-use queues.
  // Scenario:
  //   http.request → webhook.received (3 routes matched)
  //   → 3× queue.enqueue (single-use: pr-meta-*, pr-diff-*, pr-ctx-*)
  //   → 3× queue.dequeue → 3× agent.run + tool calls
  //   → 3× queue.enqueue (analysis-CORREID shared collector queue)
  //   → queue.dequeue (collect) → agent.run (decision)
  //   → queue.enqueue (comments-in) → queue.dequeue → agent.run (pr-comments)
  //     with tool.call/tool.result → artifact.written
  function causalChain() {
    const corrId  = "hide_github_pull_request_20260601_1245_a3f2";
    const itemIds = ["item_aa01", "item_bb02", "item_cc03"];
    const analysisQ = "analysis-" + corrId;
    const prMetaQ   = "pr-meta-" + corrId;
    const prDiffQ   = "pr-diff-" + corrId;
    const prCtxQ    = "pr-ctx-" + corrId;
    const artifactId = "ci-gate-decision_" + corrId + ".md";

    return [
      // --- HTTP receipt ---
      ev("http.request", "intake", "endpoint", "/webhook/github", {
        method:      "POST",
        path:        "/webhook/github",
        action:      "pull_request.synchronize",
        delivery_id: "ci-78213482",
      }, 0),

      // --- Webhook received (1 event for the full request) ---
      ev("webhook.received", "tannery", "webhook", "github.pull_request", {
        webhook_name: "github.pull_request",
        path:         "/webhook/github",
        hide_id:      corrId,
        hide_kind:    "github_pull_request",
        route_count:  3,
      }, 1),

      // --- Fan-out: 3 single-use queues ---
      ev("queue.enqueue", "tannery", "queue", prMetaQ, {
        dest_queue: prMetaQ,
        hide_id:    corrId,
        hide_kind:  "github_pull_request",
        curing:     "pr-metadata",
        item_id:    itemIds[0],
      }, 1),
      ev("queue.enqueue", "tannery", "queue", prDiffQ, {
        dest_queue: prDiffQ,
        hide_id:    corrId,
        hide_kind:  "github_pull_request",
        curing:     "pr-diff",
        item_id:    itemIds[1],
      }, 1),
      ev("queue.enqueue", "tannery", "queue", prCtxQ, {
        dest_queue: prCtxQ,
        hide_id:    corrId,
        hide_kind:  "github_pull_request",
        curing:     "pr-context",
        item_id:    itemIds[2],
      }, 1),

      // --- Workers pick up single-use queues ---
      ev("queue.dequeue", "worker", "queue", prMetaQ, {
        queue:   prMetaQ,
        hide_id: corrId,
        curing:  "pr-metadata",
        item_id: itemIds[0],
      }, 2),
      ev("queue.dequeue", "worker", "queue", prDiffQ, {
        queue:   prDiffQ,
        hide_id: corrId,
        curing:  "pr-diff",
        item_id: itemIds[1],
      }, 2),
      ev("queue.dequeue", "worker", "queue", prCtxQ, {
        queue:   prCtxQ,
        hide_id: corrId,
        curing:  "pr-context",
        item_id: itemIds[2],
      }, 2),

      // --- Parallel agent runs (metadata, diff, context) ---
      ev("agent.operation.start", "runner", "agent", "pr-metadata", {
        agent: "pr-metadata", model: "llama3.2",
        operation: "op_meta_01",
        hide_id: corrId,
      }, 3),
      ev("cut.selected", "runner", "hide", corrId, {
        hide_id: corrId, page: 1, total_pages: 4, tokens: 1820,
      }, 3),
      ev("agent.operation.start", "runner", "agent", "pr-diff", {
        agent: "pr-diff", model: "llama3.2",
        operation: "op_diff_01",
        hide_id: corrId,
      }, 3),
      ev("cut.selected", "runner", "hide", corrId, {
        hide_id: corrId, page: 1, total_pages: 8, tokens: 3840,
      }, 3),
      ev("agent.operation.start", "runner", "agent", "pr-context", {
        agent: "pr-context", model: "llama3.2",
        operation: "op_ctx_01",
        hide_id: corrId,
      }, 3),
      ev("tool.call", "runner", "tool", "list_files", {
        tool: "list_files", agent: "pr-context",
        path: "internal/", hide_id: corrId,
      }, 4),
      ev("tool.result", "runner", "tool", "list_files", {
        tool: "list_files", status: "ok",
        result_bytes: 2048, lines: 72, hide_id: corrId,
      }, 4),

      // --- Three agents enqueue into shared analysis collector queue ---
      ev("queue.enqueue", "tannery", "queue", analysisQ, {
        dest_queue: analysisQ,
        hide_id:    corrId,
        curing:     "collect",
        item_id:    "item_ana01",
      }, 5),
      ev("queue.enqueue", "tannery", "queue", analysisQ, {
        dest_queue: analysisQ,
        hide_id:    corrId,
        curing:     "collect",
        item_id:    "item_ana02",
      }, 5),
      ev("queue.enqueue", "tannery", "queue", analysisQ, {
        dest_queue: analysisQ,
        hide_id:    corrId,
        curing:     "collect",
        item_id:    "item_ana03",
      }, 5),

      // --- Collector dequeue and decision agent ---
      ev("queue.dequeue", "worker", "queue", analysisQ, {
        queue:   analysisQ,
        hide_id: corrId,
        curing:  "collect",
        item_id: "item_ana01",
      }, 6),
      ev("agent.operation.start", "runner", "agent", "decision", {
        agent: "decision", model: "llama3.2",
        operation: "op_dec_01",
        hide_id: corrId,
      }, 7),
      ev("curing.step.complete", "tannery", "curing", "collect", {
        curing: "collect", agent: "decision",
        completed_cuts: 3, total_cuts: 3, hide_id: corrId,
      }, 8),

      // --- Output to comments queue (static) ---
      ev("queue.enqueue", "tannery", "queue", "comments-in", {
        dest_queue: "comments-in",
        hide_id:    corrId,
        curing:     "pr-comments",
        item_id:    "item_cmt01",
      }, 8),
      ev("queue.dequeue", "worker", "queue", "comments-in", {
        queue:   "comments-in",
        hide_id: corrId,
        curing:  "pr-comments",
        item_id: "item_cmt01",
      }, 9),

      // --- pr-comments agent: posts review via tool ---
      ev("agent.operation.start", "runner", "agent", "pr-comments", {
        agent: "pr-comments", model: "llama3.2",
        operation: "op_cmt_01",
        hide_id: corrId,
      }, 10),
      ev("tool.call", "runner", "tool", "gh_pr_review", {
        tool: "gh_pr_review", agent: "pr-comments",
        operation: "post_comment",
        hide_id: corrId,
      }, 11),
      ev("tool.result", "runner", "tool", "gh_pr_review", {
        tool: "gh_pr_review", status: "ok",
        result_preview: "PR #412 review posted",
        hide_id: corrId,
      }, 11),

      // --- Artifact ---
      ev("artifact.written", "curing.worker", "artifact", artifactId, {
        artifact:  artifactId,
        path:      "./tannery/artifacts/" + artifactId,
        size_bytes: 4821,
        curing:    "pr-comments",
        agent:     "pr-comments",
        operation: "op_cmt_01",
        hide_id:   corrId,
      }, 12),
    ];
  }

  function nearby() {
    return [
      ev("queue.enqueue", "tannery", "queue", "comments-in", {
        dest_queue: "comments-in",
        hide_id:    "hide_github_pull_request_20260601_1200_b1c2",
        curing:     "pr-comments",
      }, 0),
      ev("http.request", "intake", "endpoint", "/webhook/github", {
        method: "POST", path: "/webhook/github",
      }, 0),
      ev("tool.result", "runner", "tool", "write_file", {
        tool: "write_file", status: "ok", path: "decision.md",
      }, 0),
      ev("agent.operation.end", "runner", "agent", "decision", {
        agent: "decision", duration_ms: 4200,
      }, 0),
      ev("cache.store", "cache", "cache", "8a3c...", {
        key: "8a3c...", size_bytes: 22400,
      }, 0),
      ev("system.metric", "metrics", "system", "metrics", {
        memory_mb: 142, goroutines: 38,
      }, 0),
    ];
  }

  function snapshot() {
    const chain = causalChain();
    const more = nearby();
    return {
      version: "demo",
      bus_stats: {
        capacity: 4096,
        size: 3912,
        dropped: 0,
      },
      recent_events: chain.concat(more).sort((a, b) => a.seq - b.seq),
      directory: directory(),
    };
  }

  // Curated entity directory the tree renders even when no events reference them.
  function directory() {
    const corrId    = "hide_github_pull_request_20260601_1245_a3f2";
    const analysisQ = "analysis-" + corrId;
    const prMetaQ   = "pr-meta-" + corrId;
    const prDiffQ   = "pr-diff-" + corrId;
    const prCtxQ    = "pr-ctx-" + corrId;
    return {
      curings: [
        { id: "pr-metadata",  status: "running",   count: 1 },
        { id: "pr-diff",      status: "running",   count: 1 },
        { id: "pr-context",   status: "running",   count: 1 },
        { id: "collect",      status: "running",   count: 1 },
        { id: "pr-comments",  status: "running",   count: 1 },
        { id: "issue-triage", status: "idle",      count: 0 },
        { id: "daily-brief",  status: "scheduled", count: 0 },
      ],
      queues: [
        { id: "comments-in",  status: "active",    count: 2 },
        { id: prMetaQ,        status: "ephemeral",  count: 1 },
        { id: prDiffQ,        status: "ephemeral",  count: 1 },
        { id: prCtxQ,         status: "ephemeral",  count: 1 },
        { id: analysisQ,      status: "active",    count: 3 },
        { id: "issue-triage", status: "idle",      count: 0 },
      ],
      workers: [
        { id: "curing-worker-1", status: "busy", count: 3 },
        { id: "curing-worker-2", status: "busy", count: 2 },
      ],
      webhooks: [
        { id: "github.pull_request", status: "live", count: 4 },
        { id: "github.issues",       status: "live", count: 1 },
        { id: "intake.http",         status: "live", count: 2 },
      ],
      agents: [
        { id: "pr-metadata",  status: "busy",  count: 1 },
        { id: "pr-diff",      status: "busy",  count: 1 },
        { id: "pr-context",   status: "busy",  count: 1 },
        { id: "decision",     status: "busy",  count: 1 },
        { id: "pr-comments",  status: "busy",  count: 1 },
        { id: "issue-triage", status: "idle",  count: 0 },
        { id: "summarizer",   status: "idle",  count: 0 },
      ],
      toolsets: [
        { id: "github-repo", status: "live", count: 5 },
        { id: "shell-git",   status: "live", count: 2 },
      ],
      hides: [
        { id: corrId, status: "buffered", count: 12 },
      ],
      artifacts: [
        { id: "ci-gate-decision_" + corrId + ".md", status: "stored", count: 1 },
      ],
      cache: { hitRate: 0.88 },
    };
  }

  return { snapshot, causalChain, nearby };
})();
