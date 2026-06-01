"use strict";

const DtIcons = {
  iconForKind(kind) {
    if (!kind) return "◦";
    if (kind.startsWith("http.")) return "◎";
    if (kind.startsWith("webhook.")) return "⌁";
    if (kind === "queue.enqueue") return "↥";
    if (kind === "queue.dequeue") return "↧";
    if (kind.startsWith("queue.")) return "≣";
    if (kind === "agent.run") return "▶";
    if (kind === "agent.response") return "✉";
    if (kind.startsWith("agent.")) return "⌬";
    if (kind.startsWith("tool.")) return "✦";
    if (kind.startsWith("hide.")) return "▣";
    if (kind.startsWith("artifact.")) return "◇";
    if (kind.startsWith("cache.")) return "◌";
    return "·";
  },

  iconForEntity(kind) {
    switch ((kind || "").toLowerCase()) {
      case "tannery": return "⌂";
      case "queue": return "≣";
      case "worker": return "⚙";
      case "webhook": return "⌁";
      case "curing": return "✶";
      case "agent": return "⌬";
      case "tool": return "✦";
      case "hide": return "▣";
      case "artifact": return "◇";
      case "cache": return "◌";
      case "server": return "▤";
      default: return "•";
    }
  },

  kindClass(kind) {
    if (!kind) return "dt-kind-plain";
    if (kind.startsWith("http.")) return "dt-kind-http";
    if (kind.startsWith("webhook.")) return "dt-kind-webhook";
    if (kind.startsWith("queue.")) return "dt-kind-queue";
    if (kind.startsWith("agent.")) return "dt-kind-agent";
    if (kind.startsWith("tool.")) return "dt-kind-tool";
    if (kind === "cut.selected" || kind.startsWith("hide.")) return "dt-kind-hide";
    if (kind.startsWith("curing.")) return "dt-kind-curing";
    if (kind.startsWith("artifact.")) return "dt-kind-artifact";
    if (kind.startsWith("cache.")) return "dt-kind-cache";
    if (kind.startsWith("system.")) return "dt-kind-plain";
    return "dt-kind-plain";
  },
};
