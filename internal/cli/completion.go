package cli

import (
	"fmt"
	"io"
)

// RunCompletion prints a shell completion script for the requested shell to
// stdout. Usage: leather completion <bash|zsh|fish>
//
// The scripts below are static and hand-maintained: they mirror the
// subcommand table in cli.go's Run dispatcher and the flag registrations in
// each cmd_*.go file. Update them alongside those when commands or flags
// change, the same way the `usage` string in help.go is kept in sync.
func RunCompletion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, completionUsage)
		return 2
	}

	switch args[0] {
	case "bash":
		fmt.Fprint(stdout, bashCompletionScript)
	case "zsh":
		fmt.Fprint(stdout, zshCompletionScript)
	case "fish":
		fmt.Fprint(stdout, fishCompletionScript)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, completionUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "leather completion: unknown shell %q (want bash, zsh, or fish)\n\n", args[0])
		fmt.Fprint(stderr, completionUsage)
		return 2
	}
	return 0
}

const completionUsage = `leather completion — print a shell completion script

Usage:
  leather completion bash
  leather completion zsh
  leather completion fish

Install:
  # bash (current session)
  source <(leather completion bash)

  # bash (persistent, Linux)
  leather completion bash > /etc/bash_completion.d/leather

  # zsh (system-wide; the target dir is already on $fpath on most distros)
  leather completion zsh | sudo tee /usr/share/zsh/site-functions/_leather >/dev/null

  # zsh (per-user; the dir must be added to $fpath in ~/.zshrc before compinit)
  mkdir -p ~/.zsh/completions && leather completion zsh > ~/.zsh/completions/_leather
  echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc

  # fish (persistent; completions dir is loaded automatically)
  mkdir -p ~/.config/fish/completions && leather completion fish > ~/.config/fish/completions/leather.fish
`

// bashCompletionScript completes leather subcommands, dlq/snapshot/completion
// sub-subcommands, and per-command flags. It has no dependency on the
// bash-completion package so it works from a bare `source`.
const bashCompletionScript = `# leather bash completion
#
# Install (current session):
#   source <(leather completion bash)
#
# Install (persistent, Linux):
#   leather completion bash > /etc/bash_completion.d/leather
#
# bash lays completions out in a column grid; the number of columns is a
# readline setting, not something a completion script controls. For a
# one-per-line (vertical) listing, add to ~/.inputrc:
#   set completion-display-width 0
#
# Install (persistent, Homebrew bash-completion@2):
#   leather completion bash > "$(brew --prefix)/etc/bash_completion.d/leather"

_leather_flags_for() {
    local cmd="$1" sub="$2"
    local config_flags="--config --agent-dir --model --temperature --log-level --log-format --max-tokens --completion-reserve --reasoning-reserve --summarize-threshold --llm-endpoint --llm-timeout --scheduler-tick --max-concurrent-jobs --run-duration --max-jobs --state-dir --api --api-addr --log-file --pretty --pretty-mode --stats --tokens-per-turn --show-vars --show-context --persist-runs --run-history-dir --run-max-bytes --replay --replay-live --replay-speed --tool-dir --default-toolsets --max-tool-rounds --worker-dir --cache-dir --mcp-servers-file --loop --tannery --llm-api-key"

    case "$cmd" in
        serve|run|validate|status|doctor)
            echo "$config_flags" ;;
        attach)
            echo "$config_flags --filter --no-reconnect" ;;
        ingest)
            echo "$config_flags --kind --source --curing --queue --dry-run" ;;
        workflow)
            echo "$config_flags --curing --queue --source --kind --settle --timeout" ;;
        replay)
            echo "$config_flags --live" ;;
        dlq)
            case "$sub" in
                inspect) echo "$config_flags --queue" ;;
                requeue) echo "$config_flags --queue --work-queue" ;;
            esac ;;
        snapshot)
            case "$sub" in
                save) echo "$config_flags --output" ;;
                restore) echo "$config_flags --input --force" ;;
            esac ;;
        init)
            echo "--dir --overwrite" ;;
        test-agent)
            echo "--lifecycle --mock-response --max-tokens --tool-response" ;;
    esac
}

_leather_complete() {
    local cur cmd sub
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    cmd="${COMP_WORDS[1]}"
    sub="${COMP_WORDS[2]}"

    local commands="doctor init serve run validate test-agent status dlq ingest workflow replay snapshot attach version completion help"

    if [[ $COMP_CWORD -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$commands" -- "$cur"))
        return 0
    fi

    local subcommands=""
    case "$cmd" in
        dlq) subcommands="inspect requeue" ;;
        snapshot) subcommands="save restore" ;;
        completion) subcommands="bash zsh fish" ;;
    esac

    if [[ -n "$subcommands" && $COMP_CWORD -eq 2 ]]; then
        COMPREPLY=($(compgen -W "$subcommands" -- "$cur"))
        return 0
    fi

    if [[ "$cur" == -* ]]; then
        local flags
        flags="$(_leather_flags_for "$cmd" "$sub")"
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
        return 0
    fi

    COMPREPLY=($(compgen -f -- "$cur"))
}

complete -F _leather_complete leather
`

// zshCompletionScript mirrors the bash script's logic using zsh's native
// completion widgets (_describe, _files).
const zshCompletionScript = `#compdef leather
# leather zsh completion
#
# Install (system-wide; /usr/share/zsh/site-functions is already on $fpath on most distros):
#   leather completion zsh | sudo tee /usr/share/zsh/site-functions/_leather >/dev/null
#
# Install (per-user; the target dir must be added to $fpath before compinit runs):
#   mkdir -p ~/.zsh/completions && leather completion zsh > ~/.zsh/completions/_leather
#   echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc
#
# Install (current session; requires compinit to have already run):
#   source <(leather completion zsh)

_leather() {
    # Force the annotated, one-per-line listing (vertical) for leather's own
    # completions regardless of the user's global zstyles.
    zstyle ':completion:*:*:leather:*' verbose yes
    zstyle ':completion:*:*:leather:*' list-packed false

    # value:description pairs — descriptions make zsh list matches vertically.
    local -a commands
    commands=(
        'doctor:print effective config values with source attribution'
        'init:scaffold a new project directory with config, agent, and Makefile'
        'serve:run the scheduler loop (primary operating mode)'
        'run:execute a single agent definition once and exit'
        'validate:parse and validate agent definition files; report errors'
        'test-agent:run an agent with a mock LLM and print the turn transcript'
        'status:show scheduler state, job history, token budget usage'
        'dlq:inspect and requeue outbound dead-letter queue items'
        'ingest:store raw bytes as a hide and optionally enqueue for curing'
        'workflow:ingest a hide and drain all curing queues to completion'
        'replay:replay a captured snapshot or runs directory via the API'
        'snapshot:save or restore a point-in-time archive of runtime state'
        'attach:join a running serve instance and stream runtime logs'
        'version:print build version information'
        'completion:print a shell completion script (bash, zsh, fish)'
        'help:print top-level usage'
    )

    if (( CURRENT == 2 )); then
        _describe -V 'command' commands
        return
    fi

    local cmd="${words[2]}"
    local -a subcommands
    case "$cmd" in
        dlq) subcommands=(
            'inspect:list items currently in the DLQ'
            'requeue:move a DLQ item back to a work queue for re-processing'
        ) ;;
        snapshot) subcommands=(
            'save:write a point-in-time tar.gz archive of runtime state'
            'restore:restore runtime state from a snapshot archive'
        ) ;;
        completion) subcommands=(
            'bash:print the bash completion script'
            'zsh:print the zsh completion script'
            'fish:print the fish completion script'
        ) ;;
    esac

    if (( ${#subcommands} > 0 )) && (( CURRENT == 3 )); then
        _describe -V 'subcommand' subcommands
        return
    fi

    local sub="${words[3]}"
    local -a config_flags flags
    config_flags=(
        '--config:path to config file'
        '--agent-dir:directory for *.agent.md files'
        '--model:global default model name'
        '--temperature:global default sampling temperature'
        '--log-level:log verbosity (debug, info, warn, error)'
        '--log-format:log format (text, json)'
        '--max-tokens:global token budget ceiling'
        '--completion-reserve:tokens reserved for model completion answer'
        '--reasoning-reserve:tokens reserved for a reasoning trace'
        '--summarize-threshold:summarization trigger fraction'
        '--llm-endpoint:LLM base URL'
        '--llm-timeout:LLM request timeout'
        '--scheduler-tick:scheduler wake interval'
        '--max-concurrent-jobs:max simultaneous jobs'
        '--run-duration:exit cleanly after this duration (0=unlimited)'
        '--max-jobs:exit cleanly after this many completed jobs'
        '--state-dir:job state directory'
        '--api:enable HTTP status API'
        '--api-addr:HTTP API bind address'
        '--log-file:write full structured logs to a file'
        '--pretty:render turns-only output to the console'
        '--pretty-mode:pretty console rendering (messages or all)'
        '--stats:show per-turn token counts and a shutdown summary'
        '--tokens-per-turn:print token usage after each turn'
        '--show-vars:print extracted turn variables as events'
        '--show-context:print the message window before each LLM call'
        '--persist-runs:persist run records to JSONL files'
        '--run-history-dir:directory for per-agent JSONL run logs'
        '--run-max-bytes:rotate the run log at this size in bytes'
        '--replay:start in replay mode from a snapshot JSON file'
        '--replay-live:start in live replay mode from a JSONL runs dir'
        '--replay-speed:live replay speed multiplier'
        '--tool-dir:directory of *.skill.yaml tool definitions'
        '--default-toolsets:comma-separated global default toolsets'
        '--max-tool-rounds:global default max tool call cycles per run'
        '--worker-dir:directory of *.worker.yaml definitions'
        '--cache-dir:directory for response cache JSON files'
        '--mcp-servers-file:path to mcp-servers.yaml'
        '--loop:repeat the run command N times'
        '--tannery:path to tannery.yaml; enables tannery mode'
        '--llm-api-key:bearer token for the LLM endpoint'
    )

    case "$cmd" in
        serve|run|validate|status|doctor)
            flags=("${config_flags[@]}") ;;
        attach)
            flags=("${config_flags[@]}"
                '--filter:comma-separated event kinds or sources to include'
                '--no-reconnect:exit instead of reconnecting on stream close') ;;
        ingest)
            flags=("${config_flags[@]}"
                '--kind:hide kind label (required)'
                '--source:source label'
                '--curing:explicit curing name (optional)'
                '--queue:explicit queue name (requires --curing)'
                '--dry-run:print what would be created without writing') ;;
        workflow)
            flags=("${config_flags[@]}"
                '--curing:explicit curing name'
                '--queue:explicit queue name (required with --curing)'
                '--source:source label for route matching'
                '--kind:hide kind label (required for route matching)'
                '--settle:settle delay after all queues go empty'
                '--timeout:total wall-clock deadline (0 = none)') ;;
        replay)
            flags=("${config_flags[@]}"
                '--live:start in live replay mode from a JSONL runs dir') ;;
        dlq)
            case "$sub" in
                inspect) flags=("${config_flags[@]}" '--queue:DLQ queue name to inspect') ;;
                requeue) flags=("${config_flags[@]}"
                    '--queue:DLQ queue name to read from'
                    '--work-queue:destination work queue') ;;
            esac ;;
        snapshot)
            case "$sub" in
                save) flags=("${config_flags[@]}" '--output:destination archive path') ;;
                restore) flags=("${config_flags[@]}"
                    '--input:snapshot archive to restore (required)'
                    '--force:overwrite existing state without prompting') ;;
            esac ;;
        init)
            flags=(
                '--dir:target directory for the new project'
                '--overwrite:overwrite existing files') ;;
        test-agent)
            flags=(
                '--lifecycle:apply a *.lifecycle.yaml before running'
                '--mock-response:LLM response text'
                '--max-tokens:token budget'
                '--tool-response:tool name=response text mapping') ;;
    esac

    if [[ "${words[CURRENT]}" == -* ]]; then
        _describe -V 'flag' flags
        return
    fi

    _files
}

compdef _leather leather
`

// fishCompletionScript is a static, declarative set of `complete` directives.
// Fish falls back to file completion for any position not covered below.
const fishCompletionScript = `# leather fish completion
#
# Install:
#   mkdir -p ~/.config/fish/completions && leather completion fish > ~/.config/fish/completions/leather.fish

set -l leather_commands doctor init serve run validate test-agent status dlq ingest workflow replay snapshot attach version completion help

# Top-level commands, each with a description so fish lists them one per line.
set -l top "not __fish_seen_subcommand_from $leather_commands"
complete -c leather -f -n "$top" -a doctor -d "print effective config values with source attribution"
complete -c leather -f -n "$top" -a init -d "scaffold a new project directory with config, agent, and Makefile"
complete -c leather -f -n "$top" -a serve -d "run the scheduler loop (primary operating mode)"
complete -c leather -f -n "$top" -a run -d "execute a single agent definition once and exit"
complete -c leather -f -n "$top" -a validate -d "parse and validate agent definition files; report errors"
complete -c leather -f -n "$top" -a test-agent -d "run an agent with a mock LLM and print the turn transcript"
complete -c leather -f -n "$top" -a status -d "show scheduler state, job history, token budget usage"
complete -c leather -f -n "$top" -a dlq -d "inspect and requeue outbound dead-letter queue items"
complete -c leather -f -n "$top" -a ingest -d "store raw bytes as a hide and optionally enqueue for curing"
complete -c leather -f -n "$top" -a workflow -d "ingest a hide and drain all curing queues to completion"
complete -c leather -f -n "$top" -a replay -d "replay a captured snapshot or runs directory via the API"
complete -c leather -f -n "$top" -a snapshot -d "save or restore a point-in-time archive of runtime state"
complete -c leather -f -n "$top" -a attach -d "join a running serve instance and stream runtime logs"
complete -c leather -f -n "$top" -a version -d "print build version information"
complete -c leather -f -n "$top" -a completion -d "print a shell completion script (bash, zsh, fish)"
complete -c leather -f -n "$top" -a help -d "print top-level usage"

complete -c leather -f -n "__fish_seen_subcommand_from dlq; and not __fish_seen_subcommand_from inspect requeue" -a inspect -d "list items currently in the DLQ"
complete -c leather -f -n "__fish_seen_subcommand_from dlq; and not __fish_seen_subcommand_from inspect requeue" -a requeue -d "move a DLQ item back to a work queue for re-processing"
complete -c leather -f -n "__fish_seen_subcommand_from snapshot; and not __fish_seen_subcommand_from save restore" -a save -d "write a point-in-time tar.gz archive of runtime state"
complete -c leather -f -n "__fish_seen_subcommand_from snapshot; and not __fish_seen_subcommand_from save restore" -a restore -d "restore runtime state from a snapshot archive"
complete -c leather -f -n "__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish" -a bash -d "print the bash completion script"
complete -c leather -f -n "__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish" -a zsh -d "print the zsh completion script"
complete -c leather -f -n "__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from bash zsh fish" -a fish -d "print the fish completion script"

set -l shared_flag_cmds serve run validate status doctor attach ingest workflow replay
set -l config_flags config agent-dir model temperature log-level log-format max-tokens completion-reserve reasoning-reserve summarize-threshold llm-endpoint llm-timeout scheduler-tick max-concurrent-jobs run-duration max-jobs state-dir api api-addr log-file pretty pretty-mode stats tokens-per-turn show-vars show-context persist-runs run-history-dir run-max-bytes replay replay-live replay-speed tool-dir default-toolsets max-tool-rounds worker-dir cache-dir mcp-servers-file loop tannery llm-api-key

for f in $config_flags
    complete -c leather -n "__fish_seen_subcommand_from $shared_flag_cmds" -l $f
end

complete -c leather -n "__fish_seen_subcommand_from attach" -l filter -d "comma-separated event kinds or sources to include"
complete -c leather -n "__fish_seen_subcommand_from attach" -l no-reconnect -d "exit instead of reconnecting on stream close"

complete -c leather -n "__fish_seen_subcommand_from ingest" -l kind -d "hide kind label (required)"
complete -c leather -n "__fish_seen_subcommand_from ingest" -l source -d "source label"
complete -c leather -n "__fish_seen_subcommand_from ingest" -l curing -d "explicit curing name (optional)"
complete -c leather -n "__fish_seen_subcommand_from ingest" -l queue -d "explicit queue name (requires --curing)"
complete -c leather -n "__fish_seen_subcommand_from ingest" -l dry-run -d "print what would be created without writing to disk"

complete -c leather -n "__fish_seen_subcommand_from workflow" -l curing -d "explicit curing name"
complete -c leather -n "__fish_seen_subcommand_from workflow" -l queue -d "explicit queue name (required with --curing)"
complete -c leather -n "__fish_seen_subcommand_from workflow" -l source -d "source label for route matching"
complete -c leather -n "__fish_seen_subcommand_from workflow" -l kind -d "hide kind label (required for route matching)"
complete -c leather -n "__fish_seen_subcommand_from workflow" -l settle -d "settle delay after all queues go empty"
complete -c leather -n "__fish_seen_subcommand_from workflow" -l timeout -d "total wall-clock deadline (0 = none)"

complete -c leather -n "__fish_seen_subcommand_from replay" -l live -d "start in live replay mode from a JSONL runs directory"

complete -c leather -n "__fish_seen_subcommand_from dlq; and __fish_seen_subcommand_from inspect" -l queue -d "DLQ queue name to inspect"
complete -c leather -n "__fish_seen_subcommand_from dlq; and __fish_seen_subcommand_from requeue" -l queue -d "DLQ queue name to read from"
complete -c leather -n "__fish_seen_subcommand_from dlq; and __fish_seen_subcommand_from requeue" -l work-queue -d "destination work queue"

complete -c leather -n "__fish_seen_subcommand_from snapshot; and __fish_seen_subcommand_from save" -l output -d "destination archive path"
complete -c leather -n "__fish_seen_subcommand_from snapshot; and __fish_seen_subcommand_from restore" -l input -d "snapshot archive to restore (required)"
complete -c leather -n "__fish_seen_subcommand_from snapshot; and __fish_seen_subcommand_from restore" -l force -d "overwrite existing state without prompting"

complete -c leather -n "__fish_seen_subcommand_from init" -l dir -d "target directory for the new project"
complete -c leather -n "__fish_seen_subcommand_from init" -l overwrite -d "overwrite existing files"

complete -c leather -n "__fish_seen_subcommand_from test-agent" -l lifecycle -d "apply a *.lifecycle.yaml before running"
complete -c leather -n "__fish_seen_subcommand_from test-agent" -l mock-response -d "LLM response text"
complete -c leather -n "__fish_seen_subcommand_from test-agent" -l max-tokens -d "token budget"
complete -c leather -n "__fish_seen_subcommand_from test-agent" -l tool-response -d "tool name=response text mapping"
`
