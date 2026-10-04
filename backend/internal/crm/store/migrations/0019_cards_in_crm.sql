-- D-98: business cards belong to the CRM. A scanned card is saved by a person, inside a
-- business, and linked to the lead / contact / account it is about. Nothing is shared
-- between businesses: matching and links always carry the workspace.

CREATE TABLE IF NOT EXISTS crm.card_links (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  card_id      uuid NOT NULL,
  object_key   text NOT NULL CHECK (object_key IN ('leads', 'contacts', 'accounts')),
  record_id    uuid NOT NULL,
  created_by   uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, card_id, object_key, record_id)
);
CREATE INDEX IF NOT EXISTS card_links_record_idx ON crm.card_links (workspace_id, object_key, record_id);
CREATE INDEX IF NOT EXISTS card_links_card_idx ON crm.card_links (card_id);

-- Matching a card against the people a business already has: last 10 digits of a phone
-- number, lower-cased email. Always combined with workspace_id.
CREATE INDEX IF NOT EXISTS leads_phone_key_idx ON crm.leads (workspace_id, (right(regexp_replace(COALESCE(phone, ''), '\D', '', 'g'), 10))) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS leads_mobile_key_idx ON crm.leads (workspace_id, (right(regexp_replace(COALESCE(mobile, ''), '\D', '', 'g'), 10))) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS leads_email_key_idx ON crm.leads (workspace_id, (lower(email))) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS contacts_phone_key_idx ON crm.contacts (workspace_id, (right(regexp_replace(COALESCE(phone, ''), '\D', '', 'g'), 10))) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS contacts_mobile_key_idx ON crm.contacts (workspace_id, (right(regexp_replace(COALESCE(mobile, ''), '\D', '', 'g'), 10))) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS contacts_email_key_idx ON crm.contacts (workspace_id, (lower(email))) WHERE deleted_at IS NULL;

-- The card vault itself stays where it is (public.saved_cards). It gains the person who
-- saved it as a CRM identity and the business it was saved in. user_id becomes optional:
-- someone who signs in to the CRM by email has no app profile.
DO $$
BEGIN
  IF to_regclass('public.saved_cards') IS NOT NULL THEN
    ALTER TABLE public.saved_cards ADD COLUMN IF NOT EXISTS identity_id uuid REFERENCES crm.identities;
    ALTER TABLE public.saved_cards ADD COLUMN IF NOT EXISTS workspace_id uuid REFERENCES crm.workspaces;
    ALTER TABLE public.saved_cards ALTER COLUMN user_id DROP NOT NULL;
    -- Present in the full schema, missing from the minimal bootstrap.
    ALTER TABLE public.saved_cards ADD COLUMN IF NOT EXISTS event_tag varchar(100);
    CREATE INDEX IF NOT EXISTS idx_saved_cards_identity ON public.saved_cards (identity_id) WHERE deleted_at IS NULL;
    CREATE INDEX IF NOT EXISTS idx_saved_cards_workspace ON public.saved_cards (workspace_id) WHERE deleted_at IS NULL;
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'identity_id') THEN
      UPDATE public.saved_cards c SET identity_id = u.identity_id
      FROM public.users u WHERE u.id = c.user_id AND u.identity_id IS NOT NULL AND c.identity_id IS NULL;
    END IF;
  END IF;
END $$;
