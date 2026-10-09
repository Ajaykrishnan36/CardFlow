-- Sales-overview gaps (D-134): a contact can report to another contact, and partners get
-- named roles on accounts and on deals. Everything here only adds.

ALTER TABLE crm.contacts ADD COLUMN IF NOT EXISTS reports_to_id uuid REFERENCES crm.contacts;
CREATE INDEX IF NOT EXISTS contacts_reports_to_idx ON crm.contacts (workspace_id, reports_to_id) WHERE reports_to_id IS NOT NULL;

INSERT INTO crm.relationship_types (key, label, inverse_label, source_object, target_object, cardinality, is_system) VALUES
  ('partner_on',      'Partner on',                'Partner',                'accounts', 'opportunities', 'many_to_many', true),
  ('reseller_on',     'Reseller on',               'Reseller',               'accounts', 'opportunities', 'many_to_many', true),
  ('distributor_on',  'Distributor on',            'Distributor',            'accounts', 'opportunities', 'many_to_many', true),
  ('implementer_on',  'Implementation partner on', 'Implementation partner', 'accounts', 'opportunities', 'many_to_many', true),
  ('reseller_of',     'Reseller of',               'Reseller',               'accounts', 'accounts',      'many_to_many', true),
  ('distributor_of',  'Distributor of',            'Distributor',            'accounts', 'accounts',      'many_to_many', true)
ON CONFLICT DO NOTHING;
