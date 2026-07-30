# 14-sig-triage — run everything from inside the example directory.
# The numbered targets in ../Makefile (14, 14-live, 14-eval, ...) remain the
# fresh-clone entry points; this file is the same surface for people working
# in here, plus the eval-suite targets that grew out of the ablation campaign.
#
# Endpoint/model default to the values ../Makefile uses; override per-call:
#   make eval LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000 LEATHER_MODEL=my-model

LEATHER            ?= ../../leather
SHELLMCP           ?= ../../shell-mcp
LEATHER_LLM_ENDPOINT ?= http://localhost:11434
LEATHER_MODEL      ?=

EVALENV = LEATHER="$(LEATHER)" SHELLMCP="$(SHELLMCP)" \
          LEATHER_LLM_ENDPOINT="$(LEATHER_LLM_ENDPOINT)" LEATHER_MODEL="$(LEATHER_MODEL)"

.PHONY: help demo live eval eval-fetch eval-lint eval-splits eval-test \
        taxonomy-check preflight battery noise overnight table verdicts watch \
        matrix

help:                    ## this list
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

demo:                    ## dry pipeline over the sample issue (no side effects)
	@LEATHER_DEMO_MODE=dry bash scripts/run-demo.sh

live:                    ## LIVE pipeline — posts real /sig comments/labels via gh
	@LEATHER_DEMO_MODE=live bash scripts/run-demo.sh

eval:                    ## full 250-issue eval run + classification gate
	@$(EVALENV) bash eval/run-eval.sh

eval-fetch:              ## (re)build the labeled corpus from GitHub (cached)
	@PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh

eval-lint:               ## gold hygiene: content-free rows must permit abstention
	@bash eval/scripts/gold-sanity-lint.sh

eval-splits:             ## rebuild the smoke/acceptance/holdout tier manifest
	@bash eval/scripts/make-splits.sh

eval-test:               ## scorer self-test
	@go test ./eval/

taxonomy-check:          ## detect upstream SIG drift (exit 10 = drift)
	@bash scripts/check-taxonomy-currency.sh

preflight:               ## 26 harness checks on 5 issues — run before GPU-hours
	@$(EVALENV) bash eval/scripts/preflight.sh

battery:                 ## remaining ablation cells for one rig: make battery RIG=35b
	@$(EVALENV) bash eval/scripts/run-battery.sh $(RIG)

noise:                   ## repeat-draw noise battery: make noise RIG=35b
	@$(EVALENV) bash eval/scripts/noise-battery.sh $(RIG)

overnight:               ## optional curiosity battery, both rigs (see script header)
	@$(EVALENV) bash eval/scripts/overnight-battery.sh both

table:                   ## leaderboard rendered from archives
	@python3 eval/scripts/table.py

verdicts:                ## paired McNemar verdict for every declared comparison
	@python3 eval/scripts/paired-verdicts.py

watch:                   ## live battery status, non-interactive (FILTER=4b-*-c* to narrow)
	@LOOP=1 bash eval/scripts/watch-matrix.sh

matrix:                  ## INTERACTIVE results browser: filter, rankings, McNemar pairs, per-draw detail
	@python3 eval/scripts/matrix-tui.py $(FILTER)
