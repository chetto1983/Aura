-- Reverse 0012: drop both tables (the partial index drops with its table). Neither
-- table is referenced by a later FK, so order is not constrained, but drop
-- telegram_setup_pending first for symmetry with the up creation order.
DROP TABLE IF EXISTS aura.telegram_setup_pending;
DROP TABLE IF EXISTS aura.telegram_accounts;
