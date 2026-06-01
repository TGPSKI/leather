"use strict";

const DtApi = (() => {
  function base() {
    // 1. ?api= query param wins (set by gear-icon input or direct link)
    const params = new URLSearchParams(window.location.search);
    const q = params.get("api");
    if (q) return q.replace(/\/$/, "");
    // 2. localStorage persisted address
    const stored = localStorage.getItem("leather.devtools.apiAddr");
    if (stored) return stored.replace(/\/$/, "");
    // 3. main UI server-url input (backwards compat when embedded in index.html)
    const input = document.getElementById("server-url");
    if (input && input.value) return input.value.replace(/\/$/, "");
    // 4. default
    return window.LEATHER_API_BASE || "http://127.0.0.1:7749";
  }

  // T1.10: capture per-launch DevTools token from URL hash (#token=...) on
  // first load, persist to sessionStorage, then strip from the visible URL so
  // it does not leak through referrers, screenshots, or bookmarks.
  function captureTokenFromHash() {
    try {
      const h = window.location.hash || "";
      const m = h.match(/[#&]token=([0-9a-fA-F]+)/);
      if (m && m[1]) {
        sessionStorage.setItem("leather.devtools.token", m[1]);
        const cleaned = h.replace(/([#&])token=[0-9a-fA-F]+/, "$1").replace(/^#&/, "#").replace(/^#$/, "");
        history.replaceState(null, "", window.location.pathname + window.location.search + cleaned);
      }
    } catch (_) { /* ignore */ }
  }
  captureTokenFromHash();

  function token() {
    try { return sessionStorage.getItem("leather.devtools.token") || ""; }
    catch (_) { return ""; }
  }

  // Keep exactly one fetch() call in this file by routing all requests here.
  async function request(path) {
    const headers = {};
    const t = token();
    if (t) headers["Authorization"] = "Bearer " + t;
    let res;
    try {
      res = await fetch(base() + path, { cache: "no-store", headers });
    } catch (netErr) {
      const e = new Error("Network error contacting " + base() + path + ": " + (netErr && netErr.message ? netErr.message : netErr));
      e.status = 0;
      e.networkError = true;
      throw e;
    }
    if (!res.ok) {
      const e = new Error("HTTP " + res.status + " for " + path);
      e.status = res.status;
      throw e;
    }
    return res.json();
  }

  // setToken persists a user-supplied token in sessionStorage (matching the
  // existing capture-from-hash convention). Empty string clears it.
  function setToken(t) {
    try {
      if (t) sessionStorage.setItem("leather.devtools.token", t);
      else sessionStorage.removeItem("leather.devtools.token");
    } catch (_) { /* ignore */ }
  }

  return {
    snapshot() { return request("/api/devtools/snapshot?recent=300"); },
    inspect(kind, id) { return request("/api/devtools/inspect/" + encodeURIComponent(kind) + "/" + encodeURIComponent(id)); },
    trace(seq) { return request("/api/devtools/trace/" + encodeURIComponent(seq) + "?depth=8"); },
    eventsURL() {
      // EventSource cannot set headers; pass the token as a query parameter.
      const t = token();
      const sep = t ? ("?token=" + encodeURIComponent(t)) : "";
      return base() + "/api/devtools/events" + sep;
    },
    getEntityConfig(kind, id) {
      return request("/api/config/entity?kind=" + encodeURIComponent(kind) + "&id=" + encodeURIComponent(id));
    },
    base,
    token,
    setToken,
  };
})();
