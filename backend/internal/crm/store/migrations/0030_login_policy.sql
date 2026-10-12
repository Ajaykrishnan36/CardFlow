-- Sign-in rules a business sets for its own people (D-142): the networks they may work
-- from, and how long a sign-in lasts before they must sign in again.
CREATE TABLE IF NOT EXISTS crm.login_policies (
  workspace_id  uuid PRIMARY KEY REFERENCES crm.workspaces ON DELETE CASCADE,
  ip_ranges     text[] NOT NULL DEFAULT '{}',
  session_hours int CHECK (session_hours IS NULL OR session_hours BETWEEN 1 AND 720),
  updated_by    uuid,
  updated_at    timestamptz NOT NULL DEFAULT now()
);
