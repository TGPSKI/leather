# Quarantined: 4b-F2 first attempt (FORCE_TOOL=1)

49.2% / 84 of 250 rows no-output — not a measurement. The proxy's forced
tool_choice + FORCE_TOOL_CAP truncated runaway `terms` arguments mid-string
("k8s, k8s, k8s, ..."), producing invalid tool-call JSON that vLLM 400s on the
replay request; at temperature 0 the retry reproduces it deterministically and
the row dead-letters. F2 is the only forced arm whose round-0 input is the raw
issue (single stage), which is why G2/H (short structured notes) survived the
same forcing and F2 did not. 35b-F2 was unaffected (no runaway at 35B).

Re-run uses FORCE_TOOL=0: smoke-verified (5/5 voluntary lookup_sig_v3 calls on
raw-issue prompts), consistent with Eauto-4b's 249/249 voluntary-call finding.
force_tool is recorded in the manifest and excluded from confound keys as "how
an arm expresses its variable"; on the 4B, forcing is 35B scaffolding.
