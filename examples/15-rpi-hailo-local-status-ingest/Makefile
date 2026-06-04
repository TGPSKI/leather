LEATHER ?= $(shell if command -v leather >/dev/null 2>&1; then command -v leather; elif test -x ../../leather; then printf '%s\n' ../../leather; else printf '%s\n' leather; fi)
LEATHER_LLM_ENDPOINT ?= http://localhost:8080
LEATHER_MODEL ?= qwen3:1.7b

.PHONY: run collect ingest serve artifacts clean doctor validate check-curings

doctor:
	@echo "endpoint: $(LEATHER_LLM_ENDPOINT)"
	@echo "model:    $(LEATHER_MODEL)"
	@echo "leather:  $(LEATHER)"
	@curl -fsS "$(LEATHER_LLM_ENDPOINT)/health" >/dev/null && echo "proxy: ok" || echo "proxy: not reachable"
	@test -f curings/local-status-digest.curing.yaml || \
	  (echo "missing curings/local-status-digest.curing.yaml" && exit 1)

check-curings:
	@test -f curings/local-status-digest.curing.yaml || \
	  (echo "missing curings/local-status-digest.curing.yaml" && exit 1)

validate:
	$(LEATHER) validate --config config.yaml --tannery tannery.yaml

collect:
	LEATHER_LLM_ENDPOINT=$(LEATHER_LLM_ENDPOINT) \
	LEATHER_MODEL=$(LEATHER_MODEL) \
	./scripts/collect-status.sh

ingest: collect check-curings
	$(LEATHER) ingest \
		--config config.yaml \
		--tannery tannery.yaml \
		--kind local.status \
		--source local \
		--curing local-status-digest \
		--queue default \
		sample/status.snapshot.txt

serve: check-curings
	$(LEATHER) serve \
		--config config.yaml \
		--tannery tannery.yaml \
		--llm-endpoint $(LEATHER_LLM_ENDPOINT) \
		--model $(LEATHER_MODEL) \
		--pretty \
		--stats \
		--run-duration 120s

artifacts:
	@echo
	@echo "artifacts:"
	@find .state/artifacts -type f 2>/dev/null | sort
	@echo
	@find .state/artifacts -type f 2>/dev/null -exec sh -c 'echo "--- $$1"; cat "$$1"; echo' _ {} \;

run: clean ingest serve artifacts

clean:
	rm -rf .state
