-- A customer's credit limit (D-152): orders and invoices that would take the account past
-- it are stopped unless someone writes down why they are allowed.
ALTER TABLE crm.accounts ADD COLUMN IF NOT EXISTS credit_limit numeric(18,2);
