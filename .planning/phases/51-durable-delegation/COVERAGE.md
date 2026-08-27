# API Coverage — Phase 51: Durable delegation

No external API integration: every capability in this phase is Aura's own tree — the shipped
Postgres lease queue (`aura.ingestion_jobs`), the shipped steer rail, the shipped pause
machinery, the shipped ArcadeDB memory client and our own `cmd/arcadedb-mcp` fork — generalized
or extended, with **no new `go.mod` entry** (`51-RESEARCH.md` §Standard Stack, §Package
Legitimacy Audit).

If any plan for this phase proposes a new dependency (for example a job-queue library), that is
a deviation from the research and triggers the full Package Legitimacy Gate protocol at plan
time — it is not pre-cleared by this declaration.
