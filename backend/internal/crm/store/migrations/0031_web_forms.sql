-- Web-to-lead forms (D-146): a public page a business links to or embeds on its site.
-- Whatever is sent in becomes a lead for the form's owner.
CREATE TABLE IF NOT EXISTS crm.web_forms (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  token         text NOT NULL UNIQUE,
  name          text NOT NULL,
  heading       text NOT NULL DEFAULT '',
  intro         text NOT NULL DEFAULT '',
  fields        text[] NOT NULL DEFAULT '{}',
  owner_id      uuid NOT NULL,
  lead_source   text NOT NULL DEFAULT 'web',
  consent_text  text NOT NULL DEFAULT '',
  thank_you     text NOT NULL DEFAULT '',
  is_active     boolean NOT NULL DEFAULT true,
  submissions   int NOT NULL DEFAULT 0,
  last_submission_at timestamptz,
  created_by    uuid,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS web_forms_ws ON crm.web_forms (workspace_id);
