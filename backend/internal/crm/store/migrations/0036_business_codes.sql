-- Business URLs (D-158): every business gets its own generated code and address,
-- base URL / DDMMYY WW last-4-of-ID NNNNNN. The six-digit running number comes from this
-- sequence, so two sign-ups at the same moment never share a code.
CREATE SEQUENCE IF NOT EXISTS crm.business_code_seq START 1;

-- A tenant (address) that no longer has a business of its own points at the one that
-- took its place, so old links keep working. Nothing is removed.
ALTER TABLE crm.tenants ADD COLUMN IF NOT EXISTS redirect_to uuid REFERENCES crm.tenants(id);
