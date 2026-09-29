-- Contact first, account when there's a business (D-52).
-- An app sign-up is a lead converted into a contact only; an account is created
-- when that person has a business (registered in the app or claimed from a
-- scanned card). A conversion can therefore have a contact and no account.
ALTER TABLE crm.lead_conversions ALTER COLUMN account_id DROP NOT NULL;
