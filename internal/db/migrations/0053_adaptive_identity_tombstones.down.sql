DROP TRIGGER IF EXISTS identities_adaptive_tombstone_trg ON aura.identities;
DROP FUNCTION IF EXISTS aura.tombstone_adaptive_identity();
DROP TABLE IF EXISTS aura.adaptive_identity_tombstones;
