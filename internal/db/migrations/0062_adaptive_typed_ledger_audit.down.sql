-- Validation-only migration: a successful up changed no schema or ledger data,
-- so there is nothing to reverse when moving the migration marker down.
DO $$
BEGIN
    NULL;
END;
$$;
