LEATHER ?= $(shell if command -v leather >/dev/null 2>&1; then command -v leather; elif test -x ../../leather; then printf '%s\n' ../../leather; elif test -x ../../../../leather; then printf '%s\n' ../../../../leather; else printf '%s\n' leather; fi)
LEATHER_LLM_ENDPOINT ?= http://localhost:8080
LEATHER_MODEL ?= qwen3:1.7b

.PHONY: run clean doctor validate

doctor:
	@echo "endpoint: $(LEATHER_LLM_ENDPOINT)"
	@echo "model:    $(LEATHER_MODEL)"
	@echo "leather:  $(LEATHER)"
	@curl -fsS "$(LEATHER_LLM_ENDPOINT)/health" >/dev/null && echo "proxy: ok" || echo "proxy: not reachable"

validate:
	$(LEATHER) validate --config config.yaml

run:
	$(LEATHER) serve \
		--config config.yaml \
		--llm-endpoint $(LEATHER_LLM_ENDPOINT) \
		--model $(LEATHER_MODEL) \
		--pretty \
		--stats \
		--max-jobs 1 \
		--run-duration 180s

clean:
	rm -rf .state
