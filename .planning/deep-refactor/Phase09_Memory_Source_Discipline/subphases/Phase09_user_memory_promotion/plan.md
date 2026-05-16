# Phase-O: User Memory Promotion — Plan

**Parent phase:** Phase 9 — Memory and Source Discipline  
**Status:** ✅ Closed 2026-05-16  
**Scope reference:** prd.md §5.7 user_profile_memory (lines 680–725)

## Goal

Wire `KindUserMemory` (scaffolded in Phase 7B US-L01) as a first-class writer and
retrieval layer. Conversation candidates from the summariser that express personal
facts/preferences/identity are routed to a dedicated user_memory tier — distinct from
the wiki knowledge-base (knowledge_wiki layer) — following the Mem0 V3 ADD-only
pipeline pattern (extraction → dedup hash → approval gate → compact store).

## Stories

| Story | Description |
|-------|-------------|
| US-O01 | `triage.go` — pure `TargetLayer` logic + `UserFactHandle` (SHA-256 based) |
| US-O02 | `applier.go` branch — route user-memory candidates to `proposed_updates` kind='user_memory' |
| US-O03 | `learning/user_memory_writer.go` — `WriteApprovedUserFact` writes to `compact_memory_documents` |
| US-O04 | `recall_user_memory` LLM tool + closure docs |

## Design Decisions

- **No new tables or migrations.** `proposed_updates` accepts kind='user_memory' via
  existing schema; `compact_memory_documents` accepts kind='user_memory' via existing schema.
- **Tags carry category.** The `person|preference|fact|todo` category from the triage helper
  is stored in `Document.Tags`, enabling the `recall_user_memory` category filter without
  schema extension.
- **Approval-gated write.** User facts never write autonomously; the operator reviews
  pending proposals in the dashboard before they become active. This satisfies prd.md §5.7
  "user profile memory requires explicit intent, validation, or a question when ambiguous".
- **Phase-Q deferred.** The `memory.user.write` capability check and the question gate for
  ambiguous preferences remain deferred to Phase-Q.
