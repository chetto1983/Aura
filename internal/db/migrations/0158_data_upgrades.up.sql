-- A Go data upgrade that must run once records its name here, in the same transaction as its
-- rewrite. Slot measured with `ls internal/db/migrations/ | tail -1`: 0157 was the head.
--
-- WHY. The PIM tool split (docs/superpowers/specs/2026-11-23-pim-tool-split-design.md §5.5)
-- rewrites the stored grants and policies with Go (`internal/pimsplit`, run by `aura db
-- migrate`), and it deletes the rows an operator had typed by hand under a new tool name before
-- those tools existed. After the first run, a rewritten row has exactly that shape, and so does
-- every grant or policy set on the new tools later. Nothing in those rows says whether they were
-- written before the upgrade or after it, so a rewrite keyed on shape alone would delete its own
-- output, and the operators' later grants and denies, on every migrate. This row is what says
-- "already done".
--
-- Only aura_migrate reads or writes it: 0001's default privileges would otherwise hand aura_app
-- DML on it, and the running daemon has no business here.
CREATE TABLE aura.data_upgrades (
    name    text        PRIMARY KEY,
    done_at timestamptz NOT NULL DEFAULT now()
);

REVOKE ALL ON aura.data_upgrades FROM aura_app;
GRANT ALL ON aura.data_upgrades TO aura_migrate;

COMMENT ON TABLE aura.data_upgrades IS
    'One row per Go data upgrade that has run (migration 0158), written in the upgrade''s own transaction so it runs once. Migrate role only.';
