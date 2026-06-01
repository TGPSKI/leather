# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

If you discover a potential security vulnerability, please submit a GitHub issue.

## Trust model and known limits in v0.1

Leather is a **single-user runtime**. The threat model assumes a single
trusted operator on the local machine; there is no isolation between agents
running inside one `leather serve` process.

- **HTTP API has no built-in authentication.** Bind to `127.0.0.1` (the
  default) and front with an authenticating reverse proxy if you need to
  reach it from another host. The DevTools UI is gated by a per-launch
  bearer token written to `<state-dir>/devtools.token`; the rest of the API
  surface is not.
- **No per-agent sandboxing.** Every agent in one serve process shares the
  same filesystem, environment, network, and the same set of available
  tools/MCP servers. A malicious agent definition can do anything the
  operator can do.
- **Multi-user / multi-tenant deployments are unsupported.** A team or
  family running multiple users' agents on one server should run **one
  `leather serve` process per user** with its own `--state-dir`, its own
  `--api-addr`, and its own OS-level user account. Do not share one serve
  process between distrusting users.
- **Hide and artifact stores are not encrypted at rest.** They are
  filesystem-permission protected (0600 files, 0700 directories). Encrypt
  the underlying volume if you store sensitive payloads.
- **Webhook payloads are validated with HMAC** when `secret:` is set on the
  route. A missing or empty secret means signatures are not checked — fail
  closed by always setting one in production.
- **Prompt injection is in scope but not solved.** Tool output, hide
  content, and webhook payloads are untrusted text. Curing agents must be
  written defensively; the runtime does not strip or sanitize tool output
  before showing it to the model.
