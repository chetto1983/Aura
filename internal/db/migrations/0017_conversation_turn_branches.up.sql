-- Source: phase 25-CONTEXT.md D-09 + 25-RESEARCH.md Pitfall 3 / A3 / OQ3 (full tree)
-- + 25-PATTERNS.md §"0017_conversation_turn_branches" + CHAT-05 (REQUIREMENTS.md).
--
-- D-09 conversation branch trees: additive parent/branch pointers on
-- aura.conversation_turns so the operator can edit/regenerate a turn and produce a
-- navigable branch tree (N siblings under one parent). The columns are ADDITIVE and
-- tx-safe (ALTER … ADD COLUMN … DEFAULT <constant> is metadata-only on PG 11+, and
-- the row-dependent parent_seq backfill is a plain UPDATE inside the implicit tx).
-- NO CREATE INDEX CONCURRENTLY here (Pitfall 5 / T-25-23): a single-operator pending
-- branch set is tiny (RESEARCH A4), so no index is warranted; a concurrent index, if
-- ever needed, lands in its OWN single-statement migration (the 0006 precedent).
--
-- A3 / Runtime State note: the migration touches a table that ALREADY has rows in any
-- live deployment, so it DEFAULTS every existing turn into ONE canonical linear branch
-- (branch_id = the canonical-branch sentinel, parent_seq = the previous seq). That keeps
-- LoadHistory / LoadManagedHistory byte-identical for non-branched conversations
-- (store.go:250 byte-identity contract; the CAP-04 cache-invariant gate guards the head).
-- ListTurnsBySeq SELECTs explicit columns, so adding columns does not change its output.

-- branch_id: which branch a turn belongs to. The all-zero UUID is the per-conversation
-- canonical (first/default) branch every pre-0017 turn is backfilled onto; new sibling
-- branches mint a fresh uuid. Constant default => metadata-only ADD COLUMN (tx-safe).
ALTER TABLE aura.conversation_turns
    ADD COLUMN branch_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

-- parent_seq: the seq of this turn's parent turn within the same conversation. NULL at
-- the root (seq=1). Nullable, no constant default (the canonical backfill is the plain
-- UPDATE below); a NULL parent_seq on a non-root turn would only arise from a future
-- branch write, never from this backfill.
ALTER TABLE aura.conversation_turns
    ADD COLUMN parent_seq integer;

-- Default backfill into one canonical linear branch: each existing turn's parent is the
-- previous seq; the root turn (seq=1) keeps parent_seq NULL. Deterministic and
-- row-local — no nondeterministic source, so the loaded history stays byte-identical.
UPDATE aura.conversation_turns
SET parent_seq = seq - 1
WHERE seq > 1;

COMMENT ON COLUMN aura.conversation_turns.branch_id IS
    'D-09 branch tree (CHAT-05): the branch a turn belongs to. All-zero UUID = the per-conversation canonical branch (pre-0017 backfill).';
COMMENT ON COLUMN aura.conversation_turns.parent_seq IS
    'D-09 branch tree (CHAT-05): seq of the parent turn within the conversation; NULL at the root (seq=1). Canonical backfill: seq-1.';
