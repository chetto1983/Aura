-- Drop the work board. Rolling back loses every card and saved view: unlike the policy and
-- grant tables, these rows are the operator's own content, so a rollback past 0141 needs the
-- pre-upgrade backup to keep them (docs/BACKUP-RESTORE.md).

DROP TABLE IF EXISTS aura.board_views;
DROP TABLE IF EXISTS aura.board_cards;
DROP TABLE IF EXISTS aura.boards;
