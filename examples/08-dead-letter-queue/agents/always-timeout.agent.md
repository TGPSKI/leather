---
name: always-timeout
timeout: 1ms
---

You are intentionally configured to time out immediately.
This example demonstrates DLQ behavior in the curing worker:
retry once (max_attempts=2), then route the item to fail-in-dlq.
