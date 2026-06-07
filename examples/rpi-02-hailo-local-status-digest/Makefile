LEATHER ?= $(shell if command -v leather >/dev/null 2>&1; then command -v leather; elif test -x ../../leather; then printf '%s\n' ../../leather; else printf '%s\n' leather; fi)
LEATHER_LLM_ENDPOINT ?= http://localhost:8080
LEATHER_MODEL ?= qwen3:1.7b

.PHONY: snapshot run clean show-runs doctor validate

doctor:
	@echo "endpoint: $(LEATHER_LLM_ENDPOINT)"
	@echo "model:    $(LEATHER_MODEL)"
	@echo "leather:  $(LEATHER)"
	@curl -fsS "$(LEATHER_LLM_ENDPOINT)/health" >/dev/null && echo "proxy: ok" || echo "proxy: not reachable"

snapshot:
	LEATHER_LLM_ENDPOINT=$(LEATHER_LLM_ENDPOINT) \
	LEATHER_MODEL=$(LEATHER_MODEL) \
	./scripts/collect-snapshot.sh
	./scripts/build-facts.sh
	./scripts/render-lifecycle.sh
	@echo
	@echo "facts:"
	@cat .snapshot/facts.env

validate: snapshot
	$(LEATHER) validate --config config.yaml

run: snapshot
	LEATHER_LLM_ENDPOINT=$(LEATHER_LLM_ENDPOINT) \
	LEATHER_MODEL=$(LEATHER_MODEL) \
	$(LEATHER) serve --config config.yaml --pretty --stats --run-duration 75s

clean:
	rm -rf .state .snapshot agents/local-status.lifecycle.yaml

show-runs:
	find .state/runs -maxdepth 2 -type f 2>/dev/null | sort
