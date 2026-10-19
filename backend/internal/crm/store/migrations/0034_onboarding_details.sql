-- Onboarding (D-155): a person's own details (asked once, reused for every business they
-- create), the business card images a business uploads, and (in crm.workspaces.custom_domain) each business's own login domain.
CREATE TABLE IF NOT EXISTS crm.person_details (
    identity_id    uuid PRIMARY KEY REFERENCES crm.identities(id) ON DELETE CASCADE,
    date_of_birth  date,
    personal_email text,
    address_line   text,
    landmark       text,
    area           text,
    city           text,
    district       text,
    state          text,
    postal_code    text,
    country        text,
    latitude       numeric(9,6),
    longitude      numeric(9,6),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm.business_card_images (
    workspace_id uuid NOT NULL REFERENCES crm.workspaces(id) ON DELETE CASCADE,
    side         text NOT NULL CHECK (side IN ('front', 'back')),
    content_type text NOT NULL,
    data         bytea NOT NULL,
    uploaded_by  uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, side)
);

-- A business's own login domain uses crm.workspaces.custom_domain (unique since 0001).
