-- Restore 0043's control-character rule.
--
-- Reversible only while no stored preview contains a control character; once a real tool
-- result is in the table (a shell command's trailing newline is enough) this ADD fails,
-- which is correct — the down migration must not silently discard rows it cannot satisfy.
ALTER TABLE aura.idempotency_operations
    DROP CONSTRAINT IF EXISTS idempotency_operations_replay_preview_check;

ALTER TABLE aura.idempotency_operations
    ADD CONSTRAINT idempotency_operations_replay_preview_check CHECK (
        replay_preview IS NULL OR (
            octet_length(replay_preview) <= 4096
            AND replay_preview !~ '[[:cntrl:]]'
        )
    );
