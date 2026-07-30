# ADR 0041 — Tool consequence policy

- **Status:** Accepted
- **Date:** 2026-07-31
- **Requirement:** OPS-06 / F-025
- **Relates to:** `prd.md` Amendments #96, #97, #105.3, #106

## Context

Treating every write as destructive makes an agent unusable; trusting connector annotations makes
policy bypassable. Aura needs one consequence model across built-ins, MCP, scheduled work, CLI, and
web surfaces.

## Decision

Aura classifies the effective operation from the source tool and normalized arguments. MCP
annotations are advisory only. Destructive, irreversible, broad-scope, credential/security-boundary,
or externally costly actions require explicit confirmation/resume or are denied when no responder
exists. Ordinary tenant-scoped or sandbox-contained reversible writes do not prompt solely because
they mutate.

Every permitted mutation still requires authenticated ownership, a durable policy
decision/reservation, an idempotency key bound to the normalized operation, bounded egress, and a
terminal ledger outcome. Strict profiles route host-power tools into the per-identity sandbox.
Unknown consequence or missing ownership fails closed.

## Consequences

- Creating a scoped document or authorized calendar fixture may proceed without universal prompts.
- Deletion, credential rotation, device unlink, broad publication, and equivalent impact are
  confirmed or withheld.
- Scheduled/headless work cannot manufacture a human confirmation.
- Gateway, policy, idempotency, and sandbox regression tests are release evidence.
