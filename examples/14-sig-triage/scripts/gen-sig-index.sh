#!/usr/bin/env bash
# gen-sig-index.sh — derive the term index from the SIG feature catalog.
#
# The catalog (sigs.reference.yaml) is prose: each SIG owns a list of feature
# phrases. That shape is readable but it is not queryable, so the match stage
# could only ever consume it whole -- which is why "read the catalog" measured
# barely better than no catalog at all (+2.2 points): the whole file in context
# is the same scaling problem as inline rules, just relocated.
#
# The index is the queryable form: one association per line, so a lookup returns
# the two or three candidate SIGs for an issue instead of all twenty-two.
#
#   term <TAB> MATCH|NOT_MATCH <TAB> sig-name <TAB> provenance
#
# TSV rather than YAML deliberately. This file is written by a generator, read
# by grep, and legible to a future maintenance loop (measured null as an accuracy lever — see eval/results/lessons-eval-methodology.md): one association per line makes an
# edit a one-line diff that a human can review and the eval gate can bisect.
#
# MATCH rows are generated from the catalog and are safe to regenerate.
# NOT_MATCH rows are LEARNED from eval confusion pairs and are NOT derivable
# from upstream -- they are preserved across regeneration. Deleting this file
# discards learned boundaries; edit, don't recreate.
#
#   bash scripts/gen-sig-index.sh          # print to stdout
#   WRITE=1 bash scripts/gen-sig-index.sh  # rewrite sigs.index.tsv, keeping NOT_MATCH
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$EX_DIR"
CATALOG="${CATALOG:-sigs.reference.yaml}"
INDEX="${INDEX:-sigs.index.tsv}"

[ -f "$CATALOG" ] || { echo "gen-sig-index: no catalog at $CATALOG" >&2; exit 2; }

# Preserve learned NOT_MATCH rows: they cannot be regenerated from the catalog.
learned=""
if [ -f "$INDEX" ]; then
  learned="$(grep -P '\tNOT_MATCH\t' "$INDEX" || true)"
fi

generated="$(awk -F'\n' '
  /^[[:space:]]*-[[:space:]]*name:[[:space:]]*sig-/ {
    sub(/^.*name:[[:space:]]*/, ""); sub(/[[:space:]]*$/, ""); sig = $0; next
  }
  /^[[:space:]]*features:[[:space:]]*$/ { infeat = 1; next }
  /^[[:space:]]*[a-z_]+:[[:space:]]*/ && !/^[[:space:]]*-/ { if ($0 !~ /features:/) infeat = 0 }
  infeat && /^[[:space:]]*-[[:space:]]/ {
    line = $0
    sub(/^[[:space:]]*-[[:space:]]*/, "", line)
    n = split(line, parts, /,[[:space:]]*/)
    for (i = 1; i <= n; i++) {
      term = parts[i]
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", term)
      if (length(term) < 3) continue
      print tolower(term) "\tMATCH\t" sig "\tcatalog"
    }
  }
' "$CATALOG" | sort -u)"

[ -n "$generated" ] || { echo "gen-sig-index: catalog parse produced no terms (format change?)" >&2; exit 2; }

out="$(printf '%s\n' "$generated"; [ -n "$learned" ] && printf '%s\n' "$learned"; true)"
if [ "${WRITE:-0}" = "1" ]; then
  printf '%s\n' "$out" > "$INDEX"
  echo "wrote $INDEX: $(grep -cP '\tMATCH\t' "$INDEX" || true) MATCH, $(grep -cP '\tNOT_MATCH\t' "$INDEX" || true) NOT_MATCH" >&2
else
  printf '%s\n' "$out"
fi
