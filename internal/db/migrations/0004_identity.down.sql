-- capability_grants FK ON DELETE CASCADE means dropping it explicitly first is
-- belt-and-suspenders; dropping identities alone would also cascade.
DROP TABLE IF EXISTS aura.capability_grants;
DROP TABLE IF EXISTS aura.identities;
