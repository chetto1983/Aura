-- The address an email draft leaves from, read by the host from the PIM's list_accounts when the draft
-- is created, so the review shows who the message is sent as and not only the account id (measured on
-- the lab VM on 2026-11-12). Empty for WhatsApp, and when the PIM names no address for the account.
ALTER TABLE aura.message_drafts ADD COLUMN sender_address text NOT NULL DEFAULT '';
