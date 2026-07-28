# Quarantined: 4b-T2c first attempt (turn-scope execution leak)

12.8% / 214 of 250 rows no-output — not a measurement of context clearing.
435/471 decide rounds called get_sig_reference on a turn declared `tools: []`,
and leather EXECUTED it: per-turn tool scope gates what is OFFERED in the
request, but the executor honors any registered tool name. The cleared context
leaves only the system prompt, whose sig-catalog skill text says "Call it
once" — the 4B obeys that over the turn prompt, refetches every round, and
dies at the 8-round cap. The 35B ran the same config to completion because it
never attempted the out-of-scope call (its wreck-free twin archived at 74.8).

Same shape as the G/G2 finding, pointed at the harness: a boundary delivered
as advice is ignored; only enforcement in code binds. Core fix wanted: reject
out-of-scope tool calls with a tool-result error instead of executing.
Re-run mitigates at the prompt level (decide turn states no tool exists).
