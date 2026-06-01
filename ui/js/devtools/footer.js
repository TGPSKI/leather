"use strict";

const DtFooter = (() => {
  function render(container, state) {
    const cls = state.connectionState === "live"
      ? "dt-state-live"
      : (state.connectionState === "error" ? "dt-state-err" : "dt-state-warn");
    const stats = state.stats || {};
    container.innerHTML = ""
      + "<span class=\"" + cls + "\">● " + esc(state.connectionState) + "</span>"
      + "<span>seq: " + (state.lastSeq || 0) + "</span>"
      + "<span>dropped: " + (state.dropped || 0) + "</span>"
      + "<span>ring: " + (stats.size || 0) + "/" + (stats.capacity || 0) + "</span>";
  }

  function esc(s) {
    return ltEscapeHtml(s == null ? "" : String(s));
  }

  return { render };
})();
