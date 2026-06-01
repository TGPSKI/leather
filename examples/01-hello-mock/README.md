# 01-hello-mock

The smallest possible leather invocation. **No LLM, no network, nothing to
install beyond the binary itself.** `leather test-agent` loads the agent,
swaps in `MockLLM`, runs one turn, and prints the transcript.

If this works, your build is healthy.

## Run

```bash
make 01
```

## What you should see

```
--- test-agent: greeter ---
[user]      mock prompt
[assistant] Hello from leather. Everything is wired.
```

## Files

- `agents/greeter.agent.md` — the agent definition (markdown + YAML front matter).
- `config.yaml` — only used by `make validate-01` to prove the workspace parses.
