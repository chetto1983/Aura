-- Reverse 0035: narrow aura.assets.source_kind back to the 0020 set ('web','telegram','cli').
--
-- A down that narrows the kind CHECK must first remove the rows the widening admitted: any asset
-- with source_kind='agent' (an ingested send_file deliverable) violates the restored 0020 CHECK
-- and aborts the whole down mid-chain (dirty database). aura.asset_events FKs aura.assets(id)
-- ON DELETE CASCADE, so this delete also reaps each agent asset's event rows.
DELETE FROM aura.assets WHERE source_kind = 'agent';

ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli'));
