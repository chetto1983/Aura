# Compaction durable-memory privacy review

## Disposition

**Approved for disabled-by-default deployment.** Promotion and retrieval remain fail-closed unless both `Policy.ReviewApproved` and a class-specific allow policy are present. This approval does not authorize enabling any class in production; class enablement requires its own purpose, consent, retention, sensitivity, and regional assessment.

## Threat boundary

Continuation checkpoints are working-context projections and are never durable memories. The memory boundary accepts only typed candidate metadata, an ephemeral evidence sample used for validation, immutable minimized source references, and SHA-256 evidence/source digests. It never persists summary prose. PostgreSQL is the durability and transaction boundary. The live API is an operator control plane behind `governance.write`; no model tool can reach it. Store policy predicates still treat tenant, identity, capability, purpose, region, and sensitivity claims as untrusted.

## Abuse cases and controls

| Abuse case | Control | Exact evidence | Disposition |
|---|---|---|---|
| Summary text is silently promoted | Candidate schema has no prose/value column; evidence is hashed before persistence; promotion defaults off | `TestCompactionCandidateIdempotentAcrossRestoreAndRebuild`, migration 0038 column review | Mitigated |
| Secret or transcript over-collection | Credential-pattern rejection and 256-byte ephemeral evidence ceiling; source records contain kind, ID, and digest only | `TestCompactionCandidateRejectsSecretAndEvidenceOvercollection` | Mitigated |
| Cross-tenant or cross-identity retrieval | Exact tenant and owner predicates precede ranking; denied results reveal no candidate state | `TestRetrievalGateDeniesCrossBoundaryBeforeRelevance/identity` | Mitigated |
| Purpose, regional, capability, or sensitivity bypass | Exact hard SQL predicates and closed sensitivity vocabulary run before confidence/recency ordering | `TestRetrievalGateDeniesCrossBoundaryBeforeRelevance` | Mitigated |
| Automatic promotion without governance | Independent-review bit and explicit class allowlist are both mandatory | `TestPromotionRequiresReviewAndExplicitClassPolicy` | Mitigated |
| Restore/rebuild duplicates memory | Candidate uniqueness spans owner, class, purpose, canonical source digest, and evidence digest; promotion is unique by candidate | `TestCompactionCandidateIdempotentAcrossRestoreAndRebuild` | Mitigated |
| Deleted source or withdrawn consent remains retrievable | One transaction revokes candidate and all promoted projections | `TestConsentWithdrawalDeletionForgetAndExpiryPropagate` | Mitigated |
| Superseded or expired record remains active | Candidate and memory lifecycle links are updated transactionally and both are filtered before ranking | `TestSupersessionRemovesOldMemoryFromRetrieval` | Mitigated |
| Provenance is rewritten to hide origin | Source/reachability rows reject update and delete | `TestMigration0038TablesAndImmutableSources` | Mitigated |
| Privacy methods exist only in tests and never run in production | Daemon injects the concrete store into a bounded closed-action admin route; every action has a production call edge | `TestCompactionMemoryComposition`, `TestCompactionMemoryAdminActionsReachStore`, `deadcode -test ./...` | Mitigated |
| An ordinary identity or model invokes a destructive lifecycle action | Parent mux requires `governance.write`; the route is absent from the tool registry and errors are sanitized | `TestServeWebuiAuthulaSubtreePublic`, `TestServeWebuiApprovalsCapabilityGate` | Mitigated |

## Privacy lifecycle

- Collection is purpose-bound and consent-basis-bound at creation.
- Evidence is minimized to a digest; plaintext exists only during validation and is not inserted.
- Immutable source links preserve deletion reachability without copying source prose.
- Promotion and retrieval are separate authorization decisions.
- Source deletion, consent withdrawal, expiry, supersession, and forget-me revoke both candidates and promoted memories transactionally.
- Canonical conversation deletion remains owned by the conversation domain; this store consumes a typed source-deletion event and does not mutate canonical evidence.
- The admin control plane dispatches the same store methods; `TestCompactionMemoryAdminLifecyclePostgresE2E` proves consent withdrawal, source deletion, forget-me, expiry, and supersession across the HTTP/PostgreSQL boundary.

## Residual risk

- The credential detector is defense in depth, not a complete data-loss-prevention engine. Upstream candidate extractors must avoid supplying secrets, and future candidate classes require adversarial fixtures before enablement.
- Digests may still be linkable when an attacker already knows candidate evidence. Database access controls and tenant isolation remain mandatory.
- Region is a policy label in this slice; infrastructure residency and replication enforcement must be verified separately before a regional class is enabled.
- Lifecycle propagation is transactional inside PostgreSQL. External replicas, backups, and downstream exports require their own deletion attestations before durable-memory rollout.
- `governance.write` holders are trusted operators and can name tenants, owners, and sources. Any future self-service route must derive those identifiers from the authenticated principal instead of accepting them from request JSON.

## Reviewer gate

The gate passes only while all named integration tests execute against PostgreSQL with race detection, the governance-write auth tests remain green, `deadcode` keeps the production memory surface reachable, migration 0038 remains rollback-compatible, no summary/value prose column is added, and the default policy remains disabled. Any new candidate class, evidence field, self-service route, export, replica, or region changes this disposition to **review required**.
