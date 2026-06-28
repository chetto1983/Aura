DROP TRIGGER IF EXISTS identity_recovery_audit_no_truncate ON aura.identity_recovery_audit;
DROP TRIGGER IF EXISTS identity_recovery_audit_no_update_delete ON aura.identity_recovery_audit;
DROP TABLE IF EXISTS aura.identity_recovery_audit;
DROP TABLE IF EXISTS aura.password_reset_tokens;
DROP TABLE IF EXISTS aura.password_reset_challenges;
DROP TABLE IF EXISTS aura.identity_recovery;
DROP FUNCTION IF EXISTS aura.reject_identity_recovery_audit_mutation();
