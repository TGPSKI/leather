"use strict";

const DtCommon = (() => {
  function matchesEntity(ev, kind, id) {
    const p = ev.payload || {};
    if (kind === "queue") return String(p.queue || p.dest_queue || "") === id;
    if (kind === "webhook") return String(p.webhook_name || "") === id;
    if (kind === "curing") return String(p.curing || "") === id;
    if (kind === "agent") return ev.entity_kind === "agent" && ev.entity_id === id;
    if (kind === "artifact") return ev.entity_kind === "artifact" && ev.entity_id === id;
    if (kind === "hide") {
      // Match any event that carries this hide_id, regardless of entity_kind.
      if (String(p.hide_id || "") === id && id !== "") return true;
      // Also match hide-typed entity events (demo/cut.selected path).
      const isHideEv = ev.entity_kind === "hide" || ev.kind === "cut.selected";
      return isHideEv && ev.entity_id === id;
    }
    if (kind === "tool") return (ev.kind || "").startsWith("tool.") && String(p.tool || "") === id;
    if (kind === "worker") return String(p.worker || "") === id;
    return false;
  }

  return { matchesEntity };
})();
