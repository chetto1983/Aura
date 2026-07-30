---
status: resolved
trigger: "Use every Aura tool directly against production runtime and report quality, issues, speed, and metrics; unit/mock or Cockpit results do not count."
created: 2026-07-30T15:39:44+02:00
updated: 2026-07-30T20:00:00+02:00
---

# Direct Production Tool Audit

## Resolution

- root_cause: The original green checks did not exercise the production
  composition root or cross-process domain outcomes. Concrete defects included
  Memory schema/identity mismatches, semantic failures wrapped as MCP success,
  a direct runner and doctor using different composition, stale live loaders,
  an invalid task session ID, slow serial/cold retrieval, an unavailable
  discovery embedder, and a web User-Agent refused by the real manufacturer
  site.
- fix: Production-composed `toolpipe`; resolved-runtime doctor; exact Memory
  identity and relationship handling; domain-failure decoding for
  `success:false`, top-level `error`, and `deleted:null + reason`; typed swarm
  errors; skill invalidation; valid task session UUID; concurrent/cached Memory
  and document retrieval; BM25 discovery fallback; bounded 10-second web fetch
  with complete previews and a browser-compatible default User-Agent.
- verification: Exact deployed revision
  `b303e125a8d8382ea2146ae090d500d96b4523bb`, healthy image
  `sha256:6474c3d02048323b2f637ecd7f41f5b825f260ba08859a18f96b3b62dbf827f4`.
  Manifest count is 58 (22 native, 8 Memory, 14 Calendar, 14 WhatsApp); every
  name was directly invoked. Implementation score is 9.8/10.
- external_gate: Calendar has no configured account. WhatsApp is
  `waiting_qr`, `paired=false`, `qr_available=true`. Provider/device delivery
  certification therefore remains conditional and is not represented as a
  pass.

## Final evidence

- `aura doctor`: Postgres, Neo4j, embedder, MCP binary, LLM key, and 3/3 HTTP MCP
  servers pass.
- Web incident URLs: B&R English 413 ms and Italian 3,395 ms, both full
  technical data and not truncated. Deliberate slow endpoint: typed timeout at
  10,004 ms.
- Document search: 21/21 correct; cold 4,071 ms; warm n=20 min 472, p50 497,
  p95 559, max 671 ms.
- Calendar: 14 calls in 168 ms; empty account inventory and explicit typed
  gates for all dependent operations.
- WhatsApp: 14 calls in 192 ms; local reads work and all mutation failures are
  typed, including the formerly false-success media download.
- Memory cleanup discovered the final `deleted:null + reason` false success;
  red test failed before the fix, then focused tests/build/vet/lint and
  pre-commit hooks passed. Deployed malformed deletion is now `status:error` in
  4 ms; valid relationship deletions are `status:ok`.
- All temporary filesystem, document graph/job, task, skill, process, and
  Memory fixtures were removed and verified absent.

## Report

See `docs/audit/direct-production-tool-audit-2026-07-30.md`.
