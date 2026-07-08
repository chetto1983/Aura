-- Source: Phase 37A (Web Artifact Delivery Lane) / WEBART-01 / D-06.
-- send_file under an authenticated identity ingests the produced file into Garage as an owned
-- aura.assets row (assets.Service.IngestAgentFile). That row's source_kind must be 'agent' so an
-- agent-produced deliverable is first-class and distinguishable from human 'web'/'telegram'/'cli'
-- uploads (audit + future retention/filtering), but the 0020 source_kind CHECK admits only the
-- original three, so the ingest INSERT fails with 23514 until this widen lands.
--
-- Mirror 0034's widen verbatim: the 0020 constraint is an inline column CHECK, so Postgres
-- auto-named it `assets_source_kind_check`; drop + re-add it with the extra 'agent' member. 0020
-- already GRANTed aura_app DML on aura.assets (aura_migrate owns DDL), so no grant change is
-- needed here.

ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli', 'agent'));
