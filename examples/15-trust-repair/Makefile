# 15-trust-repair — run everything from inside the example directory.
# Pre-pilot scaffold: graders live in skeptic (testdata/repair); model runs
# wait on the pilot gate. See README.md.

LEATHER              ?= ../../leather
SKEPTIC_ROOT         ?= $(HOME)/git/TGPSKI/skeptic
LEATHER_LLM_ENDPOINT ?= http://localhost:11434
LEATHER_MODEL        ?=

RUNENV = LEATHER="$(LEATHER)" SKEPTIC_ROOT="$(SKEPTIC_ROOT)" \
         LEATHER_LLM_ENDPOINT="$(LEATHER_LLM_ENDPOINT)" LEATHER_MODEL="$(LEATHER_MODEL)"

.PHONY: help selftest smoke clean

help:                    ## this list
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

selftest:                ## bidirectional grader selftest (in skeptic; must be GREEN before any model run)
	bash "$(SKEPTIC_ROOT)/testdata/repair/tools/selftest.sh"

smoke:                   ## no-model plumbing test: scripted saint + liar through the full runner
	@$(RUNENV) REPAIR_SCRIPTED="$(SKEPTIC_ROOT)/testdata/repair/gh-actions/mutable-action-pin/selftest/saint.sh" \
	  bash eval/run-instance.sh B gh-actions/mutable-action-pin/v1 smoke-saint
	@$(RUNENV) REPAIR_SCRIPTED="$(SKEPTIC_ROOT)/testdata/repair/gh-actions/unsafe-prt-checkout/selftest/liar-trigger-remover.sh" \
	  bash eval/run-instance.sh B gh-actions/unsafe-prt-checkout/v1 smoke-liar; \
	  test $$? -ne 0 || { echo "smoke-liar unexpectedly PASSED — grader hole"; exit 1; }
	@echo "smoke OK: saint passed, liar failed"

clean:                   ## remove state and smoke artifacts
	rm -rf .state eval/results/runs/smoke-*
