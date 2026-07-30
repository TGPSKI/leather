#!/usr/bin/env bash
# run-instance.sh <arm> <family/defect/variant> [tag]
#
# One trust-repair instance, end to end:
#   1. copy the pristine fixture repo (skeptic testdata/repair) to a workdir
#   2. compose the arm's task input (B: task only; R: +rule; E: +rule+evidence;
#      V/V2: +rule, verify tools in the agent)
#   3. run the arm agent against the workdir (or REPAIR_SCRIPTED=<script> to
#      apply a scripted patcher instead — plumbing test, no model)
#   4. score with skeptic's conjunction scorer (the oracle stays in skeptic;
#      the tests under tests/ are held out from the agent)
#   5. archive verdict + patch diff + a manifest that pins the skeptic commit
#      and the fixture tree hash (fixture drift = corpus drift)
#
# Env: SKEPTIC_ROOT (skeptic checkout), LEATHER (binary), LEATHER_MODEL,
#      LEATHER_LLM_ENDPOINT, REPAIR_SCRIPTED (optional scripted patcher).
set -euo pipefail

ARM="${1:?usage: run-instance.sh <arm> <family/defect/variant> [tag]}"
REL="${2:?usage: run-instance.sh <arm> <family/defect/variant> [tag]}"
TAG="${3:-15-${ARM}-$(echo "$REL" | tr '/' '-')}"

EX_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKEPTIC_ROOT="${SKEPTIC_ROOT:-$HOME/git/TGPSKI/skeptic}"
LEATHER="${LEATHER:-$EX_DIR/../../leather}"
export SKEPTIC_BIN="${SKEPTIC_BIN:-$SKEPTIC_ROOT/bin/skeptic}"

INSTANCE="$SKEPTIC_ROOT/testdata/repair/$REL"
ARM_FILE="$EX_DIR/eval/arms/repair.$ARM.agent.md"
[ -d "$INSTANCE/repo" ] || { echo "no instance at $INSTANCE" >&2; exit 2; }
[ -f "$ARM_FILE" ] || { echo "no arm file $ARM_FILE" >&2; exit 2; }
[ -x "$SKEPTIC_BIN" ] || { echo "no skeptic binary at $SKEPTIC_BIN" >&2; exit 2; }

RUN_DIR="$EX_DIR/eval/results/runs/$TAG"
WORKDIR="$EX_DIR/.state/work/$TAG"
mkdir -p "$RUN_DIR"
rm -rf "$WORKDIR" && mkdir -p "$WORKDIR"
cp -a "$INSTANCE/repo/." "$WORKDIR/"
export REPAIR_WORKDIR="$WORKDIR"

# --- compose the arm's task input --------------------------------------------
task_prompt="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['prompt'])" "$INSTANCE/task.json")"
constraints="$(python3 -c "
import json,sys
for c in json.load(open(sys.argv[1]))['constraints']: print('- ' + c)" "$INSTANCE/task.json")"

input_file="$RUN_DIR/task-input.txt"
{
  echo "TASK: $task_prompt"
  case "$ARM" in
    R|V|V2|E)
      echo "RULE: every finding class listed below must have zero occurrences,"
      echo "while the repository's documented behavior is preserved:"
      python3 -c "
import json, subprocess, sys
inst, bin_ = sys.argv[1], sys.argv[2]
task = json.load(open(f'{inst}/task.json'))
scan = json.loads(subprocess.run([bin_,'scan','--format','json','--mode','ir', f'{inst}/repo'],
        capture_output=True, text=True).stdout)
seen = {}
for f in scan['findings']:
    if f['rule_id'] in task['targeted_rules'] and f['rule_id'] not in seen:
        seen[f['rule_id']] = f
        print(f\"- {f['rule_id']}: {f.get('title','')} — {f.get('description','')}\")" \
        "$INSTANCE" "$SKEPTIC_BIN"
      ;;
  esac
  if [ "$ARM" = E ]; then
    echo "EVIDENCE: scanner findings in the current repository state:"
    python3 -c "
import json, subprocess, sys
inst, bin_ = sys.argv[1], sys.argv[2]
task = json.load(open(f'{inst}/task.json'))
scan = json.loads(subprocess.run([bin_,'scan','--format','json','--mode','ir', f'{inst}/repo'],
        capture_output=True, text=True).stdout)
for f in scan['findings']:
    if f['rule_id'] in task['targeted_rules']:
        loc = f.get('file_path') or f.get('file') or '?'
        print(f\"- {loc}:{f.get('line','?')} [{f['rule_id']}] {f.get('match','').strip()[:120]}\")
        print(f\"  why: {f.get('description','')}\")" \
        "$INSTANCE" "$SKEPTIC_BIN"
  fi
  echo "CONSTRAINTS:"
  echo "$constraints"
} > "$input_file"

# --- run the agent (or the scripted patcher) ---------------------------------
agent_mode="model"
if [ -n "${REPAIR_SCRIPTED:-}" ]; then
  agent_mode="scripted:$(basename "$REPAIR_SCRIPTED")"
  echo "scripted patcher: $REPAIR_SCRIPTED"
  (cd "$WORKDIR" && bash "$REPAIR_SCRIPTED")
else
  cp "$ARM_FILE" "$EX_DIR/agents/repair.agent.md"
  want=$(sha256sum "$ARM_FILE" | cut -c1-12)
  got=$(sha256sum "$EX_DIR/agents/repair.agent.md" | cut -c1-12)
  [ "$want" = "$got" ] || { echo "ABORT $TAG: agent copy did not take" >&2; exit 3; }
  (cd "$EX_DIR" && "$LEATHER" workflow run --config config.yaml --tannery tannery.yaml \
      --curing repair --queue repair-in --kind repair.task --source cli --settle 5s \
      < "$input_file" 2> "$RUN_DIR/run.log")
fi

# --- score -------------------------------------------------------------------
verdict_rc=0
bash "$SKEPTIC_ROOT/testdata/repair/tools/score-instance.sh" "$INSTANCE" "$WORKDIR" \
  > "$RUN_DIR/verdict.json" || verdict_rc=$?
diff -ruN --exclude=.git "$INSTANCE/repo" "$WORKDIR" > "$RUN_DIR/patch.diff" || true

# --- manifest: identity + pins -----------------------------------------------
python3 - "$RUN_DIR" <<PYEOF
import json, os, subprocess, sys, datetime

run_dir = sys.argv[1]
def sha(path):
    import hashlib
    return hashlib.sha256(open(path,'rb').read()).hexdigest()[:12]
def git(repo, *args):
    return subprocess.run(['git','-C',repo]+list(args), capture_output=True, text=True).stdout.strip()

manifest = {
    'tag': '$TAG',
    'arm': '$ARM',
    'instance': '$REL',
    'agent_mode': '$agent_mode',
    'agent_sha': sha('$ARM_FILE'),
    'task_sha': sha('$INSTANCE/task.json'),
    'expected_findings_sha': sha('$INSTANCE/expected-findings.json'),
    'skeptic_commit': git('$SKEPTIC_ROOT','rev-parse','HEAD'),
    'fixture_tree_sha': git('$SKEPTIC_ROOT','rev-parse','HEAD:testdata/repair/$REL'),
    'leather_commit': git('$EX_DIR','rev-parse','HEAD'),
    'model': os.environ.get('LEATHER_MODEL',''),
    'endpoint': os.environ.get('LEATHER_LLM_ENDPOINT',''),
    'timestamp': datetime.datetime.now().astimezone().isoformat(timespec='seconds'),
}
json.dump(manifest, open(os.path.join(run_dir,'run-manifest.json'),'w'), indent=2)
PYEOF

pass_str="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['pass'])" "$RUN_DIR/verdict.json" 2>/dev/null || echo parse-error)"
echo "$TAG  pass=$pass_str  (verdict + patch + manifest in eval/results/runs/$TAG/)"
exit "$verdict_rc"
