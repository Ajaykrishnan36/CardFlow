-- Customer documents (D-170): a file on a record can say what it is (PAN card, Aadhaar,
-- address proof, loan papers…) and whether someone has checked it. Only additions.
ALTER TABLE crm.files ADD COLUMN IF NOT EXISTS doc_type text;
ALTER TABLE crm.files ADD COLUMN IF NOT EXISTS verified_at timestamptz;
ALTER TABLE crm.files ADD COLUMN IF NOT EXISTS verified_by uuid REFERENCES crm.identities(id) ON DELETE SET NULL;
