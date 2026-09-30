-- D-80: email conversations. The RFC Message-ID / In-Reply-To headers let replies thread
-- in the recipient's inbox and let the CRM group a record's emails into conversations.
ALTER TABLE crm.messages ADD COLUMN IF NOT EXISTS rfc_message_id text;
ALTER TABLE crm.messages ADD COLUMN IF NOT EXISTS in_reply_to text;
CREATE INDEX IF NOT EXISTS messages_rfc_idx ON crm.messages (workspace_id, rfc_message_id) WHERE rfc_message_id IS NOT NULL;
