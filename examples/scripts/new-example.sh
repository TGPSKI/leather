#!/usr/bin/env bash
# new-example.sh — scaffold a new mainline example.
#
# Usage (from examples/):  make new-example NAME=<slug>
#            or directly:  bash scripts/new-example.sh <slug>
#
# Allocates the next free NN index, creates NN-<slug>/ with the standard
# tree (README skeleton, config.yaml, agents/, tools/, sample/, scripts/
# with the pretty.sh copy and a preflight-sourcing run-demo.sh), and
# appends the NN / NN-live targets to examples/Makefile in the generated-
# targets section. The README index row and `make help` line are printed
# for you to paste — those two tables carry prose judgment and stay
# hand-written.
set -euo pipefail

cd "$(dirname "$0")/.."   # examples/

slug="${1:-}"
[ -n "$slug" ] || { echo "usage: make new-example NAME=<slug>" >&2; exit 2; }
case "$slug" in
  *[!a-z0-9-]*) echo "error: slug must be lowercase [a-z0-9-]: $slug" >&2; exit 2 ;;
esac

# Next free two-digit index across existing mainline examples.
last=$(ls -d [0-9][0-9]-*/ 2>/dev/null | sed 's/^\([0-9][0-9]\).*/\1/' | sort -n | tail -1)
next=$(printf '%02d' $((10#${last:-0} + 1)))
dir="${next}-${slug}"
[ ! -e "$dir" ] || { echo "error: $dir already exists" >&2; exit 2; }

echo "==> scaffolding $dir"
mkdir -p "$dir"/{agents,tools,sample,scripts}

# pretty.sh has no central copy — every example carries its own. Clone the
# newest sibling's copy so fixes propagate forward.
donor=$(ls [0-9][0-9]-*/scripts/pretty.sh 2>/dev/null | sort | tail -1)
[ -n "$donor" ] || { echo "error: no sibling scripts/pretty.sh found to copy" >&2; exit 1; }
cp "$donor" "$dir/scripts/pretty.sh"

cat > "$dir/config.yaml" <<EOF
agent_dir: agents
tool_dir: tools
state_dir: .state
log_level: info
model: llama3            # override: LEATHER_MODEL
api: true
api_addr: 127.0.0.1:7749
scheduler_tick: 1s
max_concurrent_jobs: 2
persist_runs: true
EOF

cat > "$dir/agents/${slug}.agent.md" <<EOF
---
name: ${slug}
model: llama3
---
You are the ${slug} agent. Replace this prompt.

---

Describe the first turn's task here.
EOF

cat > "$dir/scripts/run-demo.sh" <<EOF
#!/usr/bin/env bash
# run-demo.sh — ${dir} demo.
set -euo pipefail
cd "\$(dirname "\$0")/.."

# shellcheck source=../scripts/preflight.sh
. ../scripts/preflight.sh
lth_mode_banner "${next}"

echo "TODO: drive the demo (see sibling examples for the shape)."
EOF
chmod +x "$dir/scripts/run-demo.sh"

cat > "$dir/README.md" <<EOF
# ${next} — ${slug}

One-sentence purpose.

## Run

\`\`\`
make ${next}         # dry mode (mocked outbound calls)
make ${next}-live    # real API calls (opt-in)
\`\`\`

## What to look for

- TODO
EOF

# Register targets in the generated section of the Makefile.
marker="# --- generated example targets (make new-example) ---"
grep -qF "$marker" Makefile || printf '\n%s\n' "$marker" >> Makefile
cat >> Makefile <<EOF

.PHONY: ${next} ${next}-live
${next}: ensure-bin ensure-shell-mcp
	@echo "==> ${dir}  [dry mode]"
	@LEATHER_DEMO_MODE=dry bash ${dir}/scripts/run-demo.sh

${next}-live: ensure-bin ensure-shell-mcp
	@echo "==> ${dir}  [LIVE mode — real API calls]"
	@LEATHER_DEMO_MODE=live bash ${dir}/scripts/run-demo.sh
EOF

echo "==> done. Now hand-finish the prose registrations:"
echo
echo "  1. examples/README.md index row:"
echo "     | [${next}](${dir}/) | \`${dir}\` | yes | TODO one-line description |"
echo
echo "  2. examples/Makefile help text (under 'Mainline targets:'):"
echo "     @echo \"  ${next}   ${slug}   TODO one-line description\""
echo
echo "  3. Replace the TODOs in ${dir}/ (agent prompt, run-demo.sh, README)."
