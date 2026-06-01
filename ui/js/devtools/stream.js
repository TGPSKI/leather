"use strict";

const DtStream = (() => {
  let es = null;
  let retryMs = 200;
  let reconnectTimer = null;
  const eventKinds = [
    "http.request",
    "webhook.received",
    "intake.received",
    "queue.enqueue",
    "queue.dequeue",
    "queue.retry",
    "queue.dlq",
    "agent.turn.event",
    "agent.turn.context",
    "agent.turn.extract",
    "agent.turn.skill",
    "agent.response",
    "agent.run",
    "agent.operation.start",
    "agent.operation.end",
    "tool.call",
    "tool.result",
    "cut.selected",
    "hide.cut",
    "curing.step.complete",
    "artifact.written",
    "cache.hit",
    "cache.miss",
    "cache.store",
    "system.metric",
  ];

  // Keep exactly one EventSource() constructor in this file.
  function connect(url, handlers) {
    disconnect();
    if (!url) return;

    handlers.onState("connecting");
    es = new EventSource(url);

    es.onopen = () => {
      retryMs = 200;
      handlers.onState("live");
    };

    const onEventMessage = ev => {
      try {
        const parsed = JSON.parse(ev.data);
        handlers.onEvent(parsed);
      } catch (_) {
        // Ignore malformed frames.
      }
    };
    eventKinds.forEach(kind => es.addEventListener(kind, onEventMessage));

    es.addEventListener("gap", ev => {
      try {
        handlers.onGap(JSON.parse(ev.data));
      } catch (_) {
        handlers.onGap({});
      }
    });

    es.onerror = () => {
      handlers.onState("error");
      disconnect();
      reconnectTimer = setTimeout(() => connect(url, handlers), retryMs);
      retryMs = Math.min(retryMs * 2, 8000);
    };
  }

  function disconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (es) {
      es.close();
      es = null;
    }
  }

  return { connect, disconnect };
})();
