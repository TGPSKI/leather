"use strict";

const DtRouter = (() => {
  function parseHash() {
    const h = window.location.hash || "";
    if (h.startsWith("#/devtools/event/")) {
      return { type: "event", id: h.slice("#/devtools/event/".length) };
    }
    if (h.startsWith("#/devtools/entity/")) {
      const parts = h.slice("#/devtools/entity/".length).split("/");
      if (parts.length >= 2) return { type: "entity", kind: parts[0], id: decodeURIComponent(parts.slice(1).join("/")) };
    }
    return { type: "root" };
  }

  function toEvent(seq) {
    window.location.hash = "#/devtools/event/" + seq;
  }

  function toEntity(kind, id) {
    window.location.hash = "#/devtools/entity/" + encodeURIComponent(kind) + "/" + encodeURIComponent(id);
  }

  return { parseHash, toEvent, toEntity };
})();
