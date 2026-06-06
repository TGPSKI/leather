# examples/Makefile — clone -> demonstrate value, fast.
#
# Usage:
#   make help            list every example with a one-line description
#   make smoke		     build + run example 01 (no LLM)
#   make all             validate every example config with LLM
#   make 01              run example 01 (and so on)
#   make rpi-01          run RPi/Hailo example rpi-01 (and so on)
#   make reset           wipe per-example state
#

.PHONY: help all clean reset ensure-bin ensure-shell-mcp \
	01 02 03 04 05 06 07 08 08-requeue 09 10 11 12 13 \
	09-live 10-live 11-live 12-live \
	rpi-01 rpi-02 rpi-03 \
	validate-02 validate-rpi-01 validate-rpi-02 validate-rpi-03 summary view

# Resolve absolute path to the leather binary at repo root.
ROOT     := $(abspath ..)
LEATHER  := $(ROOT)/leather
SHELLMCP := $(ROOT)/shell-mcp

# Load local environment overrides from examples/.env when present.
# CLI/env still wins because defaults use ?= below.
ifneq (,$(wildcard .env))
include .env
export
endif

# Sensible defaults; override via env on the command line.
LEATHER_LLM_ENDPOINT ?= http://localhost:11434
LEATHER_MODEL        ?= llama3
LEATHER_RPI_LLM_ENDPOINT ?= http://localhost:8080
LEATHER_RPI_MODEL        ?= qwen3:1.7b

export LEATHER_LLM_ENDPOINT LEATHER_MODEL

help:
	@echo "leather examples"
	@echo ""
	@echo "Mainline targets:"
	@echo "  01   hello-mock          no-LLM smoke test (test-agent + MockLLM)"
	@echo "  02   scheduled-agent     leather serve with a cron'd agent"
	@echo "  03   shell-skill         agent calling shell tools via shell-mcp"
	@echo "  04   tannery-ingest      CLI ingest -> curing -> artifact"
	@echo "  05   tannery-webhook     webhook -> curing -> artifact"
	@echo "  06   multi-agent-curing  two curings chained via output.queue"
	@echo "  07   external-routing    webhook source+event_type route selection"
	@echo "  08   dead-letter-queue   retry then DLQ promotion demo"
	@echo "  08-requeue               DLQ requeue operator recovery demo"
	@echo "  09   land-tracker        fetch real property URLs, diff prices/status, notify Telegram"
	@echo "  10   ci-gate             GitHub PR webhook -> decide FULL_EVAL or SKIP (voice eval pipeline)"
	@echo "  11   high-volume-ci      25-100 webhooks with variable timing + parallel bursts (load experiment)"
	@echo "  12   spa-maintenance     scheduled site-health + dep-audit + content-drift + deploy-check webhook"
	@echo "  13   git-workflow-commit concurrent fan-out: planner enqueues per-file GPG commits"
	@echo ""
	@echo "Live-mode variants (real APIs; require auth / network — see per-example READMEs):"
	@echo "  09-live  10-live  11-live  12-live"
	@echo "  (plain '09'..'12' run in dry mode by default: outbound calls are mocked with fixtures.)"
	@echo ""
	@echo "RPi/Hailo targets (require Hailo-Ollama at http://localhost:8080):"
	@echo "  rpi-01  hailo-endpoint-canary      local OpenAI-compatible endpoint canary"
	@echo "  rpi-02  hailo-local-status-digest  local status snapshot -> scheduled digest"
	@echo "  rpi-03  hailo-local-status-ingest  local status snapshot -> hide -> curing -> artifact"
	@echo ""
	@echo "  smoke                    smoke check: build + run example 01 (no LLM)"
	@echo "  all                      run every mainline example end-to-end"
	@echo "  validate-rpi-01 validate-rpi-02 validate-rpi-03   validate RPi configs without the endpoint"
	@echo "  reset                    wipe per-example runtime state (.state/ dirs)"
	@echo "  summary                  print runtime overview of last run (.state/ rollup)"
	@echo "  view EX=NN               start a live viewer (DevTools UI) against example NN's .state"
	@echo ""
	@echo "LLM endpoint: $$LEATHER_LLM_ENDPOINT  model: $$LEATHER_MODEL"
	@echo "RPi endpoint: $$LEATHER_RPI_LLM_ENDPOINT  model: $$LEATHER_RPI_MODEL"
	@echo ""

ensure-bin:
	@if [ ! -x "$(LEATHER)" ]; then \
	  echo "leather binary not found at $(LEATHER)"; \
	  echo "run 'make build' from the repo root first"; exit 1; fi

ensure-shell-mcp:
	@if [ ! -x "$(SHELLMCP)" ]; then \
	  echo "shell-mcp binary not found at $(SHELLMCP)"; \
	  echo "run 'make build-shell-mcp' from the repo root first"; exit 1; fi

# --- per-example targets ------------------------------------------------------

01: ensure-bin
	@echo "==> 01-hello-mock (no LLM required)"
	$(LEATHER) test-agent \
	  --mock-response "Hello from leather. Everything is wired." \
	  01-hello-mock/agents/greeter.agent.md

02: ensure-bin
	@echo "==> 02-scheduled-agent (serve, ~60s; Ctrl-C to stop)"
	cd 02-scheduled-agent && $(LEATHER) serve --config config.yaml \
	  --pretty --stats --run-duration 60s

03: ensure-bin ensure-shell-mcp
	@echo "==> 03-shell-skill (one-shot run)"
	cd 03-shell-skill && $(LEATHER) run --config config.yaml --pretty \
	  --stats --tokens-per-turn --show-vars \
	  agents/inspector.agent.md

04: ensure-bin
	@echo "==> 04-tannery-ingest (ingest + serve for 15s)"
	cd 04-tannery-ingest && mkdir -p .state && \
	  $(LEATHER) ingest \
	    --config config.yaml \
	    --tannery tannery.yaml \
	    --kind build.log --source ci \
	    sample/build.log
	cd 04-tannery-ingest && $(LEATHER) serve \
	  --config config.yaml \
	  --tannery tannery.yaml \
	  --pretty --stats --persist-runs --run-duration 45s --api

05: ensure-bin
	@echo "==> 05-tannery-webhook (serve + curl + serve)"
	@bash 05-tannery-webhook/scripts/run-demo.sh

06: ensure-bin
	@echo "==> 06-multi-agent-curing (ingest + serve for 30s)"
	@echo ""
	@echo "--- sample/pr-thread.txt ---"
	@head -n 50 06-multi-agent-curing/sample/pr-thread.txt \
	  | while IFS= read -r line; do printf '%s\n' "$$line"; sleep 0.15; done
	@echo "---"
	@echo ""
	@echo "==> ingest: pr-thread.txt → triage queue"
	cd 06-multi-agent-curing && mkdir -p .state && \
	  $(LEATHER) ingest \
	    --config config.yaml \
	    --tannery tannery.yaml \
	    --kind pr.thread --source cli \
	    --curing triage --queue triage-in \
	    sample/pr-thread.txt
	cd 06-multi-agent-curing && $(LEATHER) serve \
	  --config config.yaml \
	  --tannery tannery.yaml \
	  --pretty --stats --run-duration 60s --api

07: ensure-bin
	@echo "==> 07-external-routing (webhook external routing demo)"
	@bash 07-external-routing/scripts/run-demo.sh

08: ensure-bin
	@echo "==> 08-dead-letter-queue (retry + DLQ demo)"
	@bash 08-dead-letter-queue/scripts/run-demo.sh

08-requeue: ensure-bin
	@echo "==> 08-dead-letter-queue (requeue recovery demo)"
	@bash 08-dead-letter-queue/scripts/run-requeue-demo.sh

09: ensure-bin ensure-shell-mcp
	@echo "==> 09-land-tracker (live property fetch + change detection)  [dry mode]"
	@LEATHER_DEMO_MODE=dry bash 09-land-tracker/scripts/run-demo.sh

09-live: ensure-bin ensure-shell-mcp
	@echo "==> 09-land-tracker  [LIVE mode — real network fetches]"
	@LEATHER_DEMO_MODE=live bash 09-land-tracker/scripts/run-demo.sh

10: ensure-bin ensure-shell-mcp
	@echo "==> 10-ci-gate (GitHub PR webhook → eval pipeline gating)  [dry mode]"
	@LEATHER_DEMO_MODE=dry bash 10-ci-gate/scripts/run-demo.sh

10-live: ensure-bin ensure-shell-mcp
	@echo "==> 10-ci-gate  [LIVE mode — uses gh CLI, posts real PR comments]"
	@LEATHER_DEMO_MODE=live bash 10-ci-gate/scripts/run-demo.sh

11: ensure-bin ensure-shell-mcp
	@echo "==> 11-high-volume-ci (high-volume webhook load experiment)  [dry mode]"
	@LEATHER_DEMO_MODE=dry bash 11-high-volume-ci/scripts/run-demo.sh

11-live: ensure-bin ensure-shell-mcp
	@echo "==> 11-high-volume-ci  [LIVE mode — WILL rate-limit GitHub at default volume]"
	@LEATHER_DEMO_MODE=live bash 11-high-volume-ci/scripts/run-demo.sh

12: ensure-bin ensure-shell-mcp
	@echo "==> 12-spa-maintenance (scheduled health + dep-audit + content-drift + deploy-check)  [dry mode]"
	@LEATHER_DEMO_MODE=dry bash 12-spa-maintenance/scripts/run-demo.sh

12-live: ensure-bin ensure-shell-mcp
	@echo "==> 12-spa-maintenance  [LIVE mode — real HTTP/TLS checks against configured URLs]"
	@LEATHER_DEMO_MODE=live bash 12-spa-maintenance/scripts/run-demo.sh

13: ensure-bin ensure-shell-mcp
	@echo "==> 13-git-workflow-commit (concurrent fan-out: planner + executor curings)"
	@LEATHER="$(LEATHER)" SHELLMCP="$(SHELLMCP)" \
	  LEATHER_LLM_ENDPOINT="$(LEATHER_LLM_ENDPOINT)" \
	  LEATHER_MODEL="$(LEATHER_MODEL)" \
	  bash 13-git-workflow-commit/scripts/run-demo.sh

# --- RPi/Hailo targets --------------------------------------------------------

rpi-01: ensure-bin
	@echo "==> rpi-01-hailo-endpoint-canary (requires Hailo proxy at http://localhost:8080)"
	@cd rpi-01-hailo-endpoint-canary && $(MAKE) LEATHER="$(LEATHER)" \
	  LEATHER_LLM_ENDPOINT="$(LEATHER_RPI_LLM_ENDPOINT)" \
	  LEATHER_MODEL="$(LEATHER_RPI_MODEL)" run

rpi-02: ensure-bin
	@echo "==> rpi-02-hailo-local-status-digest (requires Hailo proxy at http://localhost:8080)"
	@cd rpi-02-hailo-local-status-digest && $(MAKE) LEATHER="$(LEATHER)" \
	  LEATHER_LLM_ENDPOINT="$(LEATHER_RPI_LLM_ENDPOINT)" \
	  LEATHER_MODEL="$(LEATHER_RPI_MODEL)" run

rpi-03: ensure-bin
	@echo "==> rpi-03-hailo-local-status-ingest (requires Hailo proxy at http://localhost:8080)"
	@cd rpi-03-hailo-local-status-ingest && $(MAKE) LEATHER="$(LEATHER)" \
	  LEATHER_LLM_ENDPOINT="$(LEATHER_RPI_LLM_ENDPOINT)" \
	  LEATHER_MODEL="$(LEATHER_RPI_MODEL)" run

# --- validation-only targets (no LLM, no network) ----------------------------
# Each validate target cd's into its example dir so relative paths in
# config.yaml (agent_dir, tool_dir, mcp_servers_file, ...) resolve correctly.
#
# NOTE: `leather validate` is strict — it requires every agent to declare a
# `schedule`. Most examples have curing- or run-driven agents (no schedule),
# so we only run full validate against examples that ship a *.lifecycle.yaml.

validate-02: ensure-bin; @cd 02-scheduled-agent && $(LEATHER) validate --config config.yaml
validate-rpi-01: ensure-bin; @cd rpi-01-hailo-endpoint-canary && $(LEATHER) validate --config config.yaml
validate-rpi-02: ensure-bin; @cd rpi-02-hailo-local-status-digest && $(MAKE) LEATHER="$(LEATHER)" \
	  LEATHER_LLM_ENDPOINT="$(LEATHER_RPI_LLM_ENDPOINT)" \
	  LEATHER_MODEL="$(LEATHER_RPI_MODEL)" validate
validate-rpi-03: ensure-bin; @cd rpi-03-hailo-local-status-ingest && $(LEATHER) validate --config config.yaml --tannery tannery.yaml

# `smoke` is the cheap CI smoke: builds + runs the no-LLM example end-to-end.
smoke: ensure-bin 01 validate-02 validate-rpi-01 validate-rpi-02 validate-rpi-03
	@echo ""; echo "examples: smoke checks passed"

all: 01 02 03 04 05 06 07 08 08-requeue 09 10 11 12 13
	@echo ""; echo "examples: validation checks passed"
	@bash $(ROOT)/examples/scripts/examples-summary.sh || true

# Standalone post-run summary (does not re-run any example).
summary:
	@bash $(ROOT)/examples/scripts/examples-summary.sh

# Live viewer: start `leather serve` against an example's existing .state/ so
# the DevTools UI is browsable after `make all` has torn down per-example
# servers. Usage: make view EX=NN [ADDR=0.0.0.0:7749]
EX ?=
ADDR ?= 127.0.0.1:7749
view: ensure-bin
	@if [ -z "$(EX)" ]; then \
	  echo "usage: make view EX=NN [ADDR=host:port]   (e.g. EX=10, ADDR=0.0.0.0:7749 for LAN access)"; \
	  echo "available:"; ls -1d [0-9]*-* rpi-*-* 2>/dev/null | sed 's/^/  /'; exit 2; fi
	@dir=$$(ls -1d $(EX)-* 2>/dev/null | head -1); \
	if [ -z "$$dir" ] || [ ! -d "$$dir" ]; then \
	  echo "view: no example matches EX=$(EX)"; exit 2; fi; \
	if [ ! -f "$$dir/config.yaml" ]; then \
	  echo "view: $$dir has no config.yaml"; exit 2; fi; \
	tannery_arg=""; [ -f "$$dir/tannery.yaml" ] && tannery_arg="--tannery tannery.yaml"; \
	mcp_arg=""; [ -f "$$dir/mcp-servers.yaml" ] && mcp_arg="--mcp-servers-file mcp-servers.yaml"; \
	echo "==> view  $$dir   bind=$(ADDR)   DevTools URL prints below (Ctrl-C to stop)"; \
	cd $$dir && $(LEATHER) serve --config config.yaml $$tannery_arg $$mcp_arg \
	  --api --api-addr $(ADDR) --pretty

clean: reset

reset:
	@find . -type d -name .state -exec rm -rf {} +
	@echo "examples: runtime state reset"
