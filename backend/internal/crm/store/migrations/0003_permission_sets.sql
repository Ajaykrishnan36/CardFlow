-- Permission sets become owner-manageable (M3 access slice; DECISIONS D-29..D-32).
ALTER TABLE crm.permission_sets
  ADD COLUMN description text,
  ADD COLUMN created_by  uuid REFERENCES crm.identities,
  ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN updated_at  timestamptz NOT NULL DEFAULT now();

CREATE INDEX membership_permission_sets_set_idx ON crm.membership_permission_sets (permission_set_id);
