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

# Targets are grouped by CONSEQUENCE, not by topic. `make live` posts real
# comments and `make eval` spends GPU-hours; they must not sit in the same
# undifferentiated list as a read-only viewer, because the cost of picking
# the wrong line is not symmetric.
#   ##@1 read-only   ##@2 local checks   ##@3 spends GPU   ##@4 side effects
help:                    ##@1 this list
	@awk -F':.*##@?' ' \
	  /^[a-z][a-z-]*:.*##@[0-9]/ { \
	    n=substr($$2,1,1); d=substr($$2,2); \
	    tgt[n]=tgt[n] sprintf("  %-14s %s\n", $$1, d) } \
	  END { \
	    printf "\n  \033[1mREAD-ONLY\033[0m — safe any time, touches nothing\n%s", tgt[1]; \
	    printf "\n  \033[1mLOCAL CHECKS\033[0m — fast, no model calls\n%s", tgt[2]; \
	    printf "\n  \033[1mSPENDS GPU\033[0m — model calls, minutes to hours\n%s", tgt[3]; \
	    printf "\n  \033[1mSIDE EFFECTS\033[0m — writes outside this repo\n%s\n", tgt[4] }' \
	  $(MAKEFILE_LIST)

# ---- read-only viewers -----------------------------------------------------
matrix:                  ##@1 INTERACTIVE browser: filter, rankings, McNemar pairs, per-draw detail
	@python3 eval/scripts/matrix-tui.py $(FILTER)

watch:                   ##@1 live battery status, non-interactive (FILTER=4b-*-c* narrows)
	@LOOP=1 bash eval/scripts/watch-matrix.sh

table:                   ##@1 leaderboard rendered from archives (one-shot)
	@python3 eval/scripts/table.py

verdicts:                ##@1 paired McNemar verdict for every declared comparison
	@python3 eval/scripts/paired-verdicts.py

# ---- local checks ----------------------------------------------------------
eval-lint:               ##@2 gold hygiene: content-free rows must permit abstention
	@bash eval/scripts/gold-sanity-lint.sh

eval-splits:             ##@2 rebuild the smoke/acceptance/holdout tier manifest
	@bash eval/scripts/make-splits.sh

eval-test:               ##@2 scorer self-test
	@go test ./eval/

taxonomy-check:          ##@2 detect upstream SIG drift (exit 10 = drift)
	@bash scripts/check-taxonomy-currency.sh

# ---- spends GPU ------------------------------------------------------------
preflight:               ##@3 26 harness checks on 5 issues — run BEFORE any battery
	@$(EVALENV) bash eval/scripts/preflight.sh

demo:                    ##@3 dry pipeline over the sample issue (no side effects)
	@LEATHER_DEMO_MODE=dry bash scripts/run-demo.sh

eval:                    ##@3 full 250-issue eval run + classification gate
	@$(EVALENV) bash eval/run-eval.sh

battery:                 ##@3 remaining ablation cells for one rig: make battery RIG=35b
	@$(EVALENV) bash eval/scripts/run-battery.sh $(RIG)

noise:                   ##@3 repeat-draw noise battery: make noise RIG=35b
	@$(EVALENV) bash eval/scripts/noise-battery.sh $(RIG)

overnight:               ##@3 optional curiosity battery, both rigs (see script header)
	@$(EVALENV) bash eval/scripts/overnight-battery.sh both

# ---- side effects outside this repo ----------------------------------------
live:                    ##@4 LIVE pipeline — posts real /sig comments+labels via gh
	@LEATHER_DEMO_MODE=live bash scripts/run-demo.sh

eval-fetch:              ##@4 (re)build the labeled corpus from the GitHub API (cached)
	@PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh
