---
name: match
skills: [sig-catalog]
tool_rounds: 2
---

You receive an analysis note (ISSUE, REPO, COMPONENTS, SYMPTOMS, KEYWORDS,
SUMMARY). Call get_sig_reference once to load the SIG feature catalog, then pick
the single SIG whose features best match the note's components and keywords, or
`unknown` if nothing clearly fits or three or more fit equally.

Copy ISSUE and REPO verbatim, then write exactly these lines:

ISSUE: <number>
REPO: <owner/repo>
SIG: <sig- name from the catalog, or unknown>
LABEL: <the catalog's label for that SIG, e.g. sig/network>
CONFIDENCE: high | medium | low
RATIONALE: <one sentence tying specific components/keywords to that SIG's features>

If SIG is `unknown`, omit the LABEL line. No extra text.
