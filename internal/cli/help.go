package cli

const usage = `leather — local agent orchestrator

Usage:
  leather <command> [flags]

Commands:
  doctor      print effective config values with source attribution
  init        scaffold a new project directory with config, agent, and Makefile
  serve       run the scheduler loop (primary operating mode)
  chat        interactive chat session with session context management
  run         execute a single agent definition once and exit
  validate    parse and validate agent definition files; report errors
  test-agent  run an agent with a mock LLM and print the turn transcript
  status      show scheduler state, job history, token budget usage
  dlq         inspect and requeue outbound dead-letter queue items
  ingest      store raw bytes as a hide and optionally enqueue for curing
  workflow    run a curing workflow synchronously end-to-end (parallel queues)
  replay      replay a captured snapshot or runs directory via the API
  snapshot    save or restore a point-in-time archive of runtime state
  attach      join a running serve instance and stream pretty-printed runtime logs
  version     print build version information
  help        print this message

Use "leather <command> --help" for per-command flag details.
`
