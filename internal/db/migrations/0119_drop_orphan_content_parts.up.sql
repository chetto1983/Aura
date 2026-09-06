-- 0119: drop aura.content_parts and aura.content_part_links, and the trigger function that
-- only they used.
--
-- They were created by 0037 for a typed-multimodal content plane that was never built. 0042
-- removed the compaction feature around them and left them deliberately ("0037_content_parts
-- is independent (typed-multimodal) and untouched"), and 0087 then gave both of them a
-- permissive owner policy AND the restrictive identity-required floor. So they have been
-- carried, indexed, policied and dumped into every backup ever since, for nothing.
--
-- Measured 2026-09-06 before dropping: 0 rows in each, and the ONLY references anywhere in
-- the tree are these migrations — no Go, no query file, no sqlc model. Nothing reads them,
-- nothing writes them, and no foreign key from outside the pair points at them.
--
-- Order matters twice over. content_part_links carries the FK onto content_parts and it is
-- ON DELETE RESTRICT, so the child goes first. And dropping a table drops its trigger but
-- NOT the function behind it, which would otherwise survive as a second orphan.
DROP TABLE IF EXISTS aura.content_part_links;
DROP TABLE IF EXISTS aura.content_parts;
DROP FUNCTION IF EXISTS aura.content_part_link_immutable();
