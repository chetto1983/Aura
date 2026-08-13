-- Backfill the canonical parent_seq chain that migration 0017 left broken.
--
-- 0017 added parent_seq, backfilled `seq - 1` onto the rows that existed AT THAT MOMENT,
-- and gave the column no default. The runtime append path never set it, so every turn
-- written since carried NULL. The branch walk recurses `JOIN path p ON t.seq = p.parent_seq`,
-- so a NULL ends it: a continued branch reconstructed to the forked turn ALONE, and an
-- operator editing or regenerating a message handed the model a conversation containing
-- only the edited question.
--
-- InsertConversationTurn now derives the value, which repairs every FUTURE row. This
-- repairs the rows already on disk — measured on the live deployment before writing it:
-- 690 turns, 690 with a NULL parent, 662 of them at seq > 1 and therefore broken.
--
-- Two guards, because a blind `seq - 1` would invent topology rather than restore it:
--
--   branch_id = canonical — a FORKED turn's parent is its divergence point, NOT seq - 1.
--     Those rows also carry NULL, and guessing would silently attach them to whichever
--     turn happens to precede them. Their parent is genuinely unrecoverable from the
--     table, so they are left NULL: an honestly missing pointer beats a fabricated one.
--     (0017's own backfill was canonical-only for the same reason.)
--
--   the parent row must exist — a gap in seq (a deleted turn) would otherwise produce a
--     dangling pointer, which breaks the walk exactly like the NULL it replaced.
UPDATE aura.conversation_turns AS t
SET parent_seq = t.seq - 1
WHERE t.seq > 1
  AND t.parent_seq IS NULL
  AND t.branch_id = '00000000-0000-0000-0000-000000000000'
  AND EXISTS (
      SELECT 1
      FROM aura.conversation_turns AS p
      WHERE p.conversation_id = t.conversation_id
        AND p.seq = t.seq - 1
  );
