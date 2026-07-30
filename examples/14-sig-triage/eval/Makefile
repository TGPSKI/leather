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
# Three scoping knobs, all composable:
#   RIG=4b|35b          one rig
#   SCOPE=confirmatory  registered -cN draws only ('exploratory' = the rest)
#   FILTER=<pattern>    bare prefix, glob, substring, comma-OR, '!' negates
# e.g.  RIG=4b SCOPE=confirmatory make watch

.PHONY: help matrix watch table verdicts confirm

help:                    ## this list
	@printf '\n  \033[1m14-sig-triage eval — READ-ONLY\033[0m  (runs: make -f Makefile.run help)\n\n'
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'
	@printf '\n  RIG=4b|35b  SCOPE=confirmatory|exploratory  FILTER=<pattern>\n'
	@printf '  e.g.  RIG=4b SCOPE=confirmatory make watch\n\n'

matrix:                  ## INTERACTIVE browser: filter, rankings, McNemar pairs, per-draw detail
	@cd .. && python3 eval/scripts/matrix-tui.py $(FILTER)

watch:                   ## live status, redraws in place, no keys
	@cd .. && LOOP=1 RIG="$(RIG)" SCOPE="$(SCOPE)" FILTER="$(FILTER)" \
	  bash eval/scripts/watch-matrix.sh

table:                   ## one-shot leaderboard from archives
	@cd .. && RIG="$(RIG)" SCOPE="$(SCOPE)" FILTER="$(FILTER)" \
	  python3 eval/scripts/table.py

verdicts:                ## paired McNemar for every arms.json comparison
	@cd .. && python3 eval/scripts/paired-verdicts.py

confirm:                 ## registered contrasts under Holm (confirmatory cells only)
	@cd .. && python3 eval/scripts/confirmatory-verdicts.py
