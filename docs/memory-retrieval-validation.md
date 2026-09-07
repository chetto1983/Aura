# Memory retrieval correction — 2026-09-07

## Scope and official references

Technical identifiers must occur as complete, case-insensitive tokens in each
admitted fact (statement or endpoints) or conversation anchor. A technical token
contains a letter and either a digit or an underscore. Ordinary prose and purely
numeric dates retain semantic retrieval. This is conservative candidate admission,
not general entity resolution or answer entailment. Multiple identifiers are
conjunctive within a candidate; disjunctive and multi-record comparisons are not
understood by this gate. Filtering operates on the existing bounded candidate pool.

The database continues to own dense retrieval, full-text retrieval, fusion and
cosine reranking. No custom ranker, database adapter, schema migration, threshold
change or personal-memory fixture was introduced.

- [Vector search](https://docs.arcadedb.com/arcadedb/concepts/vector-search)
- [Full-text syntax and analyzers](https://docs.arcadedb.com/arcadedb/how-to/data-modeling/full-text-index)
- [SQL filters](https://docs.arcadedb.com/arcadedb/reference/sql/sql-where)

Expansion preserves entity names and kinds while returning each additional
fact_key only once across the expanded nodes and never repeating ranked facts.
For legacy facts without keys, identity includes endpoints, predicate, validity
window and statement. A reached entity can consequently carry an empty fact list.

## Mounted-MCP evidence

Before and after: 12 fixed questions × two tools × three sequential repetitions,
72 calls per version. Same operator memory, limit=5; no memory writes. Gold facts
and the conversation marker had been observed before evaluation. Six negative
queries used invented technical identifiers, including a bilingual docking-code
pair and substitutions of a project, database and tool name.

The running MCP image was rebuilt and recreated independently of the other
services. Image ID: sha256:ddea857d14d45aba8e4db26e63b45240643059fcaab6e0274e7fab68e719c903.
The first post-recreate mounted call failed with OAuth invalid_grant. The standard
Codex MCP login succeeded, and subsequent mounted calls actually succeeded.

| Observation | Before | After |
|---|---|---|
| Five positive fact cases, search and recall | 5/5 | 5/5 |
| Positive conversation marker, recall | found | found |
| Search abstention on six negative cases | 2/6 | 6/6 |
| Recall abstention on six negative cases | 1/6 | 6/6 |
| Duplicate fact keys across ranked evidence and expansion | observed | zero in every measured recall |
| ArcadeDB direct / MENTIONS-neighbourhood fact counts | 3 / 7 | 3 / 7 |
| Aura database fact before valid_from / now | 0 / 1 | 0 / 1 |

All three post-fix repetitions agree. Fact-only search now abstains on the
conversation-only marker instead of offering similarly named unrelated facts.
Recall still retrieves the exact conversation.

Post-fix sample call p95: search 432 ms, recall 421 ms (36 calls each, nearest-rank).
These include tool transport, are not production percentiles and are not final
agent answer latency. The sample is small and intentionally adversarial; it does
not demonstrate general abstention or correctness of final generated answers.

## Verification

- Global go vet and go build: pass in an isolated checkout containing the fix.
  The shared workspace initially failed vet in unrelated in-progress AG-UI tests
  because scriptedSkillsBoard lacked WritableHouseRoot. Those edits were preserved.
- Unit and race suites: internal/arcadedb and cmd/arcadedb-mcp pass.
- Full internal/arcadedb arcadedb_integration suite with race: pass; 86.4% statement
  coverage. Dedicated temporary database and test-owned disposable databases only.
- TestAgentMemoryMCPLive suite with race: pass, using test-owned identities.
- golangci-lint on both changed/dependent packages: zero issues.
- Mutation spot check of memoryIdentifiersMatch: 6/8 killed (75%), zero skipped.
  The two survivors remove/disable the empty-identifier early return; the final
  return still yields true, so these preserve results while losing a fast path.
- Mounted OAuth MCP: the 72 post-fix reads above plus direct, depth-2 and as_of
  regressions. Local raw receipts remain outside git in the operator's temporary
  audit directory; this document intentionally contains no personal memory dump.

MMR and PPR were not implemented or benchmarked. The remaining general question
is how to establish answer support for ordinary prose without technical identifiers.
