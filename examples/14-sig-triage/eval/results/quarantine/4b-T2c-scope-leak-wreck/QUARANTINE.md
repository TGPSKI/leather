# Quarantined: 4b-T2c first attempt (run-fatal scope refusal)

(Directory name "scope-leak" is historical and imprecise — kept to match
committed references. The evidence log shows the OPPOSITE of a leak.)

12.8% / 214 of 250 rows no-output — not a measurement of context clearing.
On 435/471 decide rounds the 4B called get_sig_reference on a turn declared
`tools: []` — recalled from the system prompt, which survives the clear and
says "call it once". The executor did NOT run it: the run-evidence log carries
435 "not in current tool scope" rejections and zero out-of-scope executions
(all 473 get_sig_reference executions are fetch-turn). The security boundary
held.

The defect is the failure mode: the rejection was RUN-FATAL — one recoverable
model mistake dead-lettered the whole work item, twice (temp-0 retry repeats
it deterministically), killing 214/250 rows. The 35B ran the identical config
to completion because it never attempted the out-of-scope call, which is why
the failure mode survived until a tool-happy small model hit it.

Fixed post-wreck: the runner now refuses out-of-scope calls with a
tool-result error the model can recover from ("tool X is not available on
this turn"), still never executing them; a model that keeps calling is bounded
by max tool rounds. See internal/runner tests
TestRunner_OutOfScopeTurnToolRefusedWithoutExecution (this exact shape) and
TestRunner_UnknownToolRejected. Re-run mitigated at the prompt level instead
(decide turn states no tool exists) — with the runner fix, that wording is
belt-and-braces rather than load-bearing.
