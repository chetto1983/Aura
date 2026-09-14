-- Reverses 0128_media_job.up.sql: dropping the table takes its recovery index and both
-- policies with it, and touches nothing else.
--
-- Never run this as an automatic rollback on a database holding jobs: each row is the only
-- local record of a paid provider job, and dropping it abandons clips that are still being
-- generated or were never delivered.
DROP TABLE IF EXISTS aura.media_job;
