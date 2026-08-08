-- Honest inverse of the up: the table did not exist before it, so dropping it restores
-- exactly the prior state. The policies, the index and the grant are all owned by the
-- table and go with it, so naming them again here would only be a second place to forget
-- one.
DROP TABLE IF EXISTS aura.ingested_documents;
