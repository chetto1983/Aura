-- Reverse 0021: drop the triggers + function + the audit table. The two indexes
-- drop with their table. Triggers are dropped explicitly before the function they
-- call (they would also drop via the table drop — explicit here for clarity).
--
-- Unlike 0010 this table widened no prior CHECK, so there is no constraint to
-- restore and no rows in other tables to clean up first.

DROP TRIGGER IF EXISTS identity_audit_no_truncate      ON aura.identity_audit;
DROP TRIGGER IF EXISTS identity_audit_no_update_delete ON aura.identity_audit;
DROP TABLE   IF EXISTS aura.identity_audit;
DROP FUNCTION IF EXISTS aura.reject_identity_audit_mutation();
