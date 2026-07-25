-- The up migration only held a transaction-scoped table lock while validating
-- immutable ledger history. It changed no schema or rows, and 0063 enforcement
-- remains necessary for every fact written after the successful validation.
DO $$
BEGIN
    NULL;
END;
$$;
