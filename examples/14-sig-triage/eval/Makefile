# 14-sig-triage eval — READ-ONLY views.
#
# Everything in this file only READS the archives under results/runs/. No
# model is called, no archive is written, nothing outside this repo is
# touched. That is the whole point of the split: reaching for a viewer must
# never be one typo away from spending GPU-hours or posting to GitHub.
#
# Things that RUN live in Makefile.run and take a deliberate flag:
#   make -f Makefile.run help
#
# FILTER accepts a bare prefix ('4b'), a glob ('4b-*-c1'), a substring
# ('T2c'), comma-OR, or a leading '!' to negate.

.PHONY: help matrix watch table verdicts confirm

help:                    ## this list
	@printf '\n  \033[1m14-sig-triage eval — READ-ONLY\033[0m  (runs: make -f Makefile.run help)\n\n'
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'
	@printf '\n  FILTER=<pattern> narrows matrix/watch/table, e.g. FILTER=4b-*-c*\n\n'

matrix:                  ## INTERACTIVE browser: filter, rankings, McNemar pairs, per-draw detail
	@cd .. && python3 eval/scripts/matrix-tui.py $(FILTER)

watch:                   ## live status, redraws in place, no keys
	@cd .. && LOOP=1 FILTER="$(FILTER)" bash eval/scripts/watch-matrix.sh

table:                   ## one-shot leaderboard from archives
	@cd .. && FILTER="$(FILTER)" python3 eval/scripts/table.py

verdicts:                ## paired McNemar for every arms.json comparison
	@cd .. && python3 eval/scripts/paired-verdicts.py

confirm:                 ## registered contrasts under Holm (confirmatory cells only)
	@cd .. && python3 eval/scripts/confirmatory-verdicts.py
