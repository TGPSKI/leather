#!/usr/bin/env python3
"""Audit every archived cell for contradictions, failures and anomalies.

Reads only results/runs/<tag>/ — no live state — so a completed cell stays
auditable after the next cell overwrites its state dir. This is the prototype of
verify-run.sh's archive mode (task #14).

Reports, per cell:
  FAIL         a claim the archive contradicts, or a run-integrity failure
  CONTRADICT   two instruments disagreeing (proxy rounds vs leather tool log)
  ANOMALY      not necessarily wrong, but unexplained and worth a look
"""
import glob, gzip, json, os, re, sys

import os
root = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "results", "runs")
issues = []


def read_jsonl_gz(p):
    if not os.path.exists(p):
        return []
    out = []
    with gzip.open(p, "rt", errors="replace") as f:
        for l in f:
            l = l.strip()
            if not l:
                continue
            try:
                out.append(json.loads(l))
            except Exception:
                pass
    return out


for d in sorted(glob.glob(os.path.join(root, "*/"))):
    tag = os.path.basename(d.rstrip("/"))
    if tag.startswith("_"):
        continue
    say = lambda kind, msg: issues.append((tag, kind, msg))

    mpath = os.path.join(d, "run-manifest.json")
    if not os.path.exists(mpath):
        say("FAIL", "no run-manifest.json — cell is unidentifiable")
        continue
    man = json.load(open(mpath))

    preds = [json.loads(l) for l in open(os.path.join(d, "predictions.jsonl"))
             if l.strip()] if os.path.exists(os.path.join(d, "predictions.jsonl")) else []
    if not preds:
        say("FAIL", "no predictions")
        continue
    if len(preds) != 250:
        say("FAIL", f"{len(preds)}/250 prediction rows")
    lost = [p["number"] for p in preds
            if p.get("predicted") == "unknown" and p.get("confidence") == "no-output"]
    if lost:
        say("FAIL", f"{len(lost)} rows with no usable match artifact: {lost[:5]}")

    lp = read_jsonl_gz(os.path.join(d, "logprobs.jsonl.gz"))
    m = [r for r in lp if r.get("stage") == "match"]
    iss = {r.get("issue") for r in m if r.get("issue") is not None}
    rpi = len(m) / len(iss) if iss else 0.0
    offered = sum(1 for r in m if r.get("tools_offered"))
    proxy_calls = sum(1 for r in m if r.get("tool_calls_made"))

    ev = os.path.join(d, "run-evidence.log.gz")
    log_calls = failed = hidemiss = 0
    if os.path.exists(ev):
        with gzip.open(ev, "rt", errors="replace") as f:
            for line in f:
                if "executing tool" in line:
                    log_calls += 1
                if "process failed" in line:
                    failed += 1
                if "hide missing" in line:
                    hidemiss += 1

    # Hide pagination silently changes the experiment: an oversized payload puts
    # leather into reflection mode (paging preamble injected, tools stripped per
    # turn, N+1 alternating turns). An arm that believes it is single-turn but
    # paginates is measuring a different delivery mechanism than its name says.
    if os.path.exists(ev):
        with gzip.open(ev, "rt", errors="replace") as f:
            pages = sum(1 for line in f if "tool=hide_next" in line or "tool=hide_jump" in line)
        if pages:
            say("FAIL", f"{pages} hide-navigation calls — this arm PAGINATED and ran in "
                        f"reflection mode, not as the single-turn arm it claims to be")

    if hidemiss:
        say("FAIL", f"{hidemiss} 'hide missing' (queue isolation)")
    if failed:
        say("ANOMALY", f"{failed} stage failures in the evidence log")

    # The core cross-check: a tool call forces a second match round, so
    # rounds/issue > 1 and a nonzero tool log must agree.
    if iss:
        if (proxy_calls > 0) != (log_calls > 0):
            say("CONTRADICT",
                f"proxy saw {proxy_calls} rounds with a tool call, leather logged {log_calls}")
        elif proxy_calls and abs(proxy_calls - log_calls) > max(2, 0.05 * log_calls):
            say("CONTRADICT",
                f"tool counts disagree: proxy {proxy_calls} vs leather log {log_calls}")
        expected_rounds = len(iss) + proxy_calls
        if abs(len(m) - expected_rounds) > max(2, 0.02 * len(m)):
            say("ANOMALY",
                f"{len(m)} match rounds but {len(iss)} issues + {proxy_calls} calls = {expected_rounds}")
    else:
        say("FAIL", "proxy recorded no match-stage rounds — telemetry void for this cell")

    temps = {r.get("temperature") for r in m if r.get("temperature") is not None}
    if not temps:
        say("ANOMALY", "temperature not recorded")
    elif temps != {0}:
        say("FAIL", f"temperature was {temps}, not 0 — run is not reproducible")

    if iss and len(iss) != 250:
        say("ANOMALY", f"proxy saw {len(iss)}/250 issues at the match stage")
    if iss and offered not in (0, len(m)):
        say("ANOMALY", f"tools offered on {offered}/{len(m)} rounds — expected all or none")

    # A match-only cell must name the cache it replayed.
    if man.get("analyze_cache", "none") != "none":
        cache = man["analyze_cache"]
        base = "examples/14-sig-triage"
        if not (os.path.exists(cache) or os.path.exists(os.path.join(base, cache))):
            say("ANOMALY", f"analyze_cache {cache} no longer on disk")
    notes = os.path.join(d, "analyze-notes.jsonl")
    n_notes = sum(1 for _ in open(notes)) if os.path.exists(notes) else 0
    if man.get("analyze_cache", "none") == "none" and n_notes != 250:
        say("ANOMALY", f"full-pipeline cell archived {n_notes}/250 analyze notes")

    corpus_p = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "corpus.jsonl")
    if os.path.exists(corpus_p):
        corpus = {json.loads(l)["number"] for l in open(corpus_p) if l.strip()}
        notes_p = os.path.join(d, "analyze-notes.jsonl")
        if os.path.exists(notes_p):
            nn = [json.loads(l)["number"] for l in open(notes_p) if l.strip()]
            orphans = sorted(set(nn) - corpus)
            dupes = sorted({n for n in nn if nn.count(n) > 1})
            if orphans:
                say("FAIL", f"artifacts attributed to issues not in the corpus (model "
                            f"mis-transcribed an ID): {orphans[:5]}")
            if dupes:
                say("FAIL", f"duplicate ISSUE attribution — one issue's answer may have "
                            f"overwritten another's: {dupes[:5]}")

    print(f"  {tag:14s} rows={len(preds):3d} rounds/issue={rpi:.2f} "
          f"offered={offered:3d} proxy_calls={proxy_calls:3d} log_calls={log_calls:3d} "
          f"temp={sorted(temps) if temps else '?'}")

print()
if not issues:
    print("AUDIT CLEAN — no failures, contradictions or anomalies")
else:
    for tag, kind, msg in issues:
        print(f"  [{kind}] {tag}: {msg}")
    print(f"\n{len(issues)} finding(s)")
sys.exit(1 if any(k in ("FAIL", "CONTRADICT") for _, k, _ in issues) else 0)
