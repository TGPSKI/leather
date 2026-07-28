#!/usr/bin/env python3
"""logprob-proxy.py — an eval instrument that sits between leather and vLLM.

Two jobs, neither of which requires changing leather:

1. UNCERTAINTY A/B. Verbalized confidence ("CONFIDENCE: high") is a prompted
   self-report, and the cascade-routing literature is consistent that it
   underperforms token-level uncertainty. vLLM will return per-token logprobs,
   but leather has no user-facing knob for `logprobs` (ExtraBody is populated
   internally only). So the proxy injects `logprobs: true, top_logprobs: N` and
   records the TOP-TOKEN MARGIN at the position where the SIG is decided:

       margin = logprob(chosen token) - logprob(runner-up token)

   at the first token after "SIG:". A large margin means the model was not
   close to emitting a different SIG; a small one means it nearly did. That is
   the signal to route on, and it is measured on the same run as the verbalized
   confidence so the two are directly comparable.

2. REQUEST PROVENANCE. Records whether each request actually carried a `tools`
   array. Counting `executing tool` in the log cannot distinguish "the model
   declined to call the catalog" from "the tool was never offered"; the request
   body can.

Records one JSONL line per chat completion to $LOGPROB_OUT. Forwards everything
else verbatim and fails open -- on any proxy-side error the upstream response is
still returned, because an instrument must not change the thing it measures.

    UPSTREAM=http://127.0.0.1:8000 PORT=8010 \\
    LOGPROB_OUT=eval/.state-eval/logprobs.jsonl \\
      python3 eval/scripts/logprob-proxy.py
"""
import json, os, re, sys, threading, urllib.request, urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UPSTREAM = os.environ.get("UPSTREAM", "http://127.0.0.1:8000").rstrip("/")
PORT = int(os.environ.get("PORT", "8010"))
OUT = os.environ.get("LOGPROB_OUT", "logprobs.jsonl")
TOP_N = int(os.environ.get("TOP_LOGPROBS", "5"))
FORCE_TOOL = os.environ.get("FORCE_TOOL", "0") == "1"
FORCE_TOOL_CAP = int(os.environ.get("FORCE_TOOL_CAP", "128"))

_lock = threading.Lock()


def _record(row):
    with _lock:
        with open(OUT, "a") as f:
            f.write(json.dumps(row) + "\n")


def _issue_of(messages):
    """The analyze note carries `ISSUE: <number>` verbatim into the match prompt."""
    for m in messages:
        c = m.get("content")
        if isinstance(c, str):
            hit = re.search(r"^ISSUE:\s*(\d+)", c, re.M)
            if hit:
                return int(hit.group(1))
    return None


def _tok_margin(t):
    """logprob(chosen) - logprob(best alternative) at one token position."""
    alts = t.get("top_logprobs") or []
    chosen = t.get("logprob")
    runner = next((a.get("logprob") for a in alts if a.get("token") != t.get("token")), None)
    margin = None if (chosen is None or runner is None) else chosen - runner
    return margin, [{"token": a.get("token"), "logprob": a.get("logprob")} for a in alts[:TOP_N]]


def _sig_margins(content_toks):
    """Two different margins at the `SIG:` field, because they mean different things.

    Every catalog name starts `sig-`, so the FIRST token after `SIG:` is almost
    always the shared prefix (" sig"). Its margin measures "commit to a SIG at
    all vs abstain" -- useful, but it is NOT the uncertainty we want to route on,
    and mistaking it for one would report near-total confidence on every row.

    The DISCRIMINATING token is the first one that pushes the accumulated value
    past `sig-`, i.e. the token that actually chooses between network / node /
    storage. That margin is the label-level uncertainty.
    """
    text, commit, discrim = "", (None, None, None), (None, None, None)
    started, value = False, ""
    for t in content_toks:
        tok = t.get("token", "")
        prev, text = text, text + tok
        if not started:
            if re.search(r"(?:^|\n)SIG:[ \t]*$", prev) and tok.strip():
                started = True
                m, alts = _tok_margin(t)
                commit = (m, tok, alts)
                value = tok.strip().lower()
                if not "sig-".startswith(value) and value != "sig-":
                    return commit, (commit[0], tok, alts)  # single-token SIG name
            continue
        if "\n" in tok:
            break
        value += tok.strip().lower()
        # Still inside the shared "sig-" prefix? Then this token decided nothing.
        if "sig-".startswith(value) or value == "sig-":
            continue
        m, alts = _tok_margin(t)
        discrim = (m, tok, alts)
        break
    return commit, discrim


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):  # keep the eval's own progress output readable
        pass

    def do_POST(self):
        self._proxy()

    def do_GET(self):
        self._proxy()

    def _proxy(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        is_chat = self.path.endswith("/chat/completions")

        req = None
        if is_chat and body:
            try:
                req = json.loads(body)
                # Ask for logprobs. Harmless for any request; only read for match.
                req["logprobs"] = True
                req["top_logprobs"] = TOP_N
                # FORCE_TOOL=1 is ablation arm D: make the catalog fetch actually
                # happen. leather has no tool_choice knob, but the decision is a
                # per-request field, so the proxy can set it without touching core.
                #
                # Only on the FIRST round: `required` compels a tool call on every
                # request it is set on, so leaving it on would trap the model in a
                # loop that can never emit its answer. A round that already carries
                # a tool result is left alone.
                if FORCE_TOOL and req.get("tools"):
                    already = any(m.get("role") == "tool"
                                  for m in (req.get("messages") or []))
                    if not already:
                        req["tool_choice"] = "required"
                        # ...and cap the round. Under `required` this model never
                        # emits a stop token: it produces the tool call within the
                        # first few tokens and then generates filler to the limit.
                        # Measured: 8192 tokens / 41s per call, versus 64 tokens /
                        # 0.7s with the SAME parsed tool call. Uncapped, every
                        # round-1 request breaches llm_timeout under concurrency and
                        # the whole run abstains. The cap costs nothing because the
                        # only thing wanted from this round is the call itself.
                        req["max_tokens"] = FORCE_TOOL_CAP
                body = json.dumps(req).encode()
            except Exception:
                req = None  # fail open: forward the original bytes

        headers = {k: v for k, v in self.headers.items()
                   if k.lower() not in ("host", "content-length", "accept-encoding")}
        headers["Content-Length"] = str(len(body))
        try:
            r = urllib.request.Request(UPSTREAM + self.path, data=body or None,
                                       headers=headers, method=self.command)
            with urllib.request.urlopen(r, timeout=600) as resp:
                out, status = resp.read(), resp.status
        except urllib.error.HTTPError as e:
            out, status = e.read(), e.code
        except Exception as e:
            msg = json.dumps({"error": {"message": f"proxy: {e}"}}).encode()
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(msg)))
            self.end_headers()
            self.wfile.write(msg)
            return

        if is_chat and req is not None and status == 200:
            try:
                self._observe(req, json.loads(out))
            except Exception:
                pass  # never let instrumentation break the run

        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    def _observe(self, req, resp):
        msgs = req.get("messages") or []
        choice = (resp.get("choices") or [{}])[0]
        content = (choice.get("message") or {}).get("content") or ""
        toks = ((choice.get("logprobs") or {}).get("content")) or []
        (cmar, ctok, calts), (dmar, dtok, dalts) = _sig_margins(toks)
        sysmsg = next((m.get("content", "") for m in msgs if m.get("role") == "system"), "")
        # Which stage produced this call, inferred from the system prompt.
        # Stage is inferred from the system prompt, and the order of these tests is
        # load-bearing. adjudicate goes first because it also carries
        # get_sig_reference. match must be recognised by EITHER catalog tool --
        # ablation arm E swaps get_sig_reference for lookup_sig, and since its
        # prompt mentions COMPONENTS (the terms it queries with), the analyze test
        # silently swallowed every one of its match calls.
        # A match response is the one that emits a SIG: line; that is the check of
        # last resort, so a future prompt rewrite degrades to "other" rather than
        # to a wrong bucket.
        stage = ("adjudicate" if "CANDIDATE_1" in sysmsg
                 else "match" if ("get_sig_reference" in sysmsg or "lookup_sig" in sysmsg)
                 else "match" if re.search(r"^SIG:", content, re.M)
                 else "analyze" if "COMPONENTS:" in sysmsg
                 else "other")
        _record({
            "issue": _issue_of(msgs),
            "stage": stage,
            # What the request actually carried, not what the config claimed.
            # verify-run.sh asserts on these; an agent whose frontmatter says
            # temperature 0 while the runtime sends 0.7 is otherwise invisible.
            "temperature": req.get("temperature"),
            "model": req.get("model"),
            # Provenance: was the catalog tool actually OFFERED on this request?
            "tools_offered": [t.get("function", {}).get("name")
                              for t in (req.get("tools") or [])],
            "tool_calls_made": [c.get("function", {}).get("name")
                                for c in ((choice.get("message") or {}).get("tool_calls") or [])],
            # commit_* = "a SIG at all vs unknown"; sig_* = "WHICH SIG" (route on this)
            "commit_margin": cmar,
            "commit_token": ctok,
            "sig_margin": dmar,
            "sig_token": dtok,
            "sig_alts": dalts,
            "verbalized": (re.search(r"^CONFIDENCE:\s*(\w+)", content, re.M) or [None, None])[1],
            "predicted": (re.search(r"^SIG:\s*(\S+)", content, re.M) or [None, None])[1],
            "runner_up": (re.search(r"^RUNNER_UP:\s*(\S+)", content, re.M) or [None, None])[1],
        })


if __name__ == "__main__":
    open(OUT, "a").close()
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"logprob-proxy: 127.0.0.1:{PORT} -> {UPSTREAM}, recording to {OUT}", file=sys.stderr)
    srv.serve_forever()
