# Security Policy

Aura is an autonomous agent runtime: it executes LLM-directed tool calls, runs
user/model-supplied code in sandboxed workers, and brokers access to a Postgres
database, a per-identity ArcadeDB memory store, and external APIs. That makes its security surface
unusually load-bearing — we take reports seriously.

## Supported versions

Only the **latest commit on `master`** (the default branch) receives security
fixes. The current release line is **v1.x** (latest tag `v1.0.1`); **v2.0.0** is
in development on `master`. Older tags and pre-release builds are not separately
maintained — please report against `master`.

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Report privately via one of:

1. **GitHub Security Advisories** — the "Report a vulnerability" button under the
   repository's **Security** tab (preferred; keeps the report private and
   tracked).
2. **Email** — dvdmarchetto@gmail.com with subject `aura security: <summary>`.

Please include: affected component/file, a description of the impact, and the
minimal steps or proof-of-concept to reproduce. If you have a suggested fix,
include it — but never include live credentials or third-party data.

### What to expect

- **Acknowledgement** within 72 hours.
- An initial assessment (severity + whether accepted) within 7 days.
- Coordinated disclosure: we agree a timeline with you before any public
  write-up, and credit you unless you prefer to remain anonymous.

## In scope (high-value targets)

- **Sandbox escape** — breaking out of the per-user container sandbox
  (`internal/sandbox/usersandbox`) that runs Python/shell workers: capability,
  seccomp, ulimit, or network-egress bypass.
- **Cross-identity isolation break** — one identity reaching another's sandbox,
  conversations, documents, or object-store bucket.
- **Tool-policy bypass** — executing a mutating tool without the reservation or
  operator approval its ToolGateway verdict requires (`internal/gateway`).
- **Tool-call / prompt injection** leading to unintended capability use,
  credential disclosure, or destructive DB operations.
- **Credential leakage** — secrets (DSNs, API keys, passwords) surfacing in
  logs, errors, events, or MCP traffic. DSN/secret redaction is a documented
  mitigation (`internal/db` `redactDSN`, the shared `internal/secret` denylist
  applied at every egress).
- **Cypher / SQL injection** through MCP or query construction.
- **Supply chain** — malicious or vulnerable dependencies (tracked by
  `govulncheck` in CI).
- **Privilege separation** — the `aura_app` runtime role must never perform DDL;
  `aura_migrate` is migrate-only.

## Out of scope

- Vulnerabilities in third-party services run as sidecars (ArcadeDB, the
  embedding model server, SearXNG, the MCP recipe servers) — report those
  upstream; tell us if Aura's configuration makes them exploitable. Aura's own
  `cmd/arcadedb-mcp` is **in** scope: it is first-party code.
- Findings that require an already-compromised host or physical access.
- Missing hardening that is explicitly scheduled in the PRD roadmap rather than
  implemented (note it, but it is not a vulnerability in shipped code).

## Hardening already in place

CI enforces `gosec`, `govulncheck`, race detection, a coverage floor, and a
threat-model verification pass per phase (`/gsd-secure-phase`). See `CLAUDE.md`
§Quality tooling & gates and each phase's `*-SECURITY.md`.
