-- 0078: a replay preview holds TOOL OUTPUT, so it may contain newlines.
--
-- 0043 created the column with `replay_preview !~ '[[:cntrl:]]'`, mirroring the Go
-- validator that the preview shared with HTTP header values and the sidecar path.
-- Rejecting every control character is right for those two — one is a CRLF-injection
-- vector, the other a path — and wrong here: a shell result almost always ends in a
-- newline, so practically every shell_exec completion was refused.
--
-- The failure mode is the bad one. The check runs when the operation is COMPLETED, i.e.
-- after the command has already executed: the effect happened, its record was rejected
-- with `idempotency_operations_replay_preview_check`, and the agent was handed an error
-- for work that had succeeded. Observed live on the appliance: the command's output was
-- in the transcript and the turn still reported failure.
--
-- The length bound stays. The control-character rule goes, and nothing replaces it:
-- Postgres already cannot store U+0000 in a text column, so the one character that
-- genuinely cannot live here is rejected by the type, not by a CHECK. Tabs, carriage
-- returns and ANSI colour are text this column holds fine.
ALTER TABLE aura.idempotency_operations
    DROP CONSTRAINT IF EXISTS idempotency_operations_replay_preview_check;

ALTER TABLE aura.idempotency_operations
    ADD CONSTRAINT idempotency_operations_replay_preview_check CHECK (
        replay_preview IS NULL OR octet_length(replay_preview) <= 4096
    );
