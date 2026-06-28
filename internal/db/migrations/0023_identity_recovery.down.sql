DROP TABLE IF EXISTS aura.identity_recovery_audit;
DROP TABLE IF EXISTS aura.password_reset_tokens;
DROP TABLE IF EXISTS aura.password_reset_challenges;
DROP TABLE IF EXISTS aura.identity_recovery;
DROP INDEX IF EXISTS aura.identities_user_name_lower_uniq;
DROP FUNCTION IF EXISTS aura.reject_identity_recovery_audit_mutation();
