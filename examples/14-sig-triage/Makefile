# 14-sig-triage — the EXAMPLE surface (the pipeline itself).
# The numbered targets in ../Makefile (14, 14-live, ...) remain the
# fresh-clone entry points; this file is the same surface for people working
# in here.
#
# The eval suite has its own Makefiles, split by consequence:
#   cd eval && make            # read-only views (matrix, watch, table, verdicts)
#   cd eval && make -f Makefile.run help    # things that spend GPU-hours
#
# api_addr in config.yaml is 7749, which is a common leather default and so
# a likely collision with an already-running instance on this machine (it
# was, once). Both demo targets bind a private port instead; override with
# DEMO_API_ADDR= if that one is taken too.

LEATHER          ?= ../../leather
SHELLMCP         ?= ../../shell-mcp
DEMO_API_ADDR    ?= 127.0.0.1:7769

.PHONY: help demo live taxonomy-check eval-help

help:                    ## this list
	@printf '\n  \033[1m14-sig-triage — the example pipeline\033[0m\n\n'
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
	@printf '\n  \033[1meval suite\033[0m (separate, split by consequence)\n'
	@printf '  %-14s %s\n' "cd eval && make" "read-only: matrix, watch, table, verdicts"
	@printf '  %-14s %s\n\n' "…-f Makefile.run" "runs: preflight, confirmatory, battery"

demo:                    ## dry pipeline over the sample issue (no side effects)
	@LEATHER_DEMO_MODE=dry LEATHER_API_ADDR=$(DEMO_API_ADDR) bash scripts/run-demo.sh

live:                    ## LIVE — posts real /sig comments+labels via gh, on a repo you own
	@LEATHER_DEMO_MODE=live LEATHER_API_ADDR=$(DEMO_API_ADDR) bash scripts/run-demo.sh

taxonomy-check:          ## detect upstream SIG drift (exit 10 = drift)
	@bash scripts/check-taxonomy-currency.sh

eval-help:               ## show both eval Makefiles' targets
	@$(MAKE) -C eval help
	@$(MAKE) -C eval -f Makefile.run help
