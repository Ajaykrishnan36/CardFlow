-- Values a business adds to a built-in dropdown (D-154): a "Bike service" case category,
-- a "Walk-in" lead source. The built-in values stay; these are added after them.
CREATE TABLE IF NOT EXISTS crm.field_option_additions (
    workspace_id uuid NOT NULL REFERENCES crm.workspaces(id) ON DELETE CASCADE,
    object_key   text NOT NULL,
    field_key    text NOT NULL,
    value        text NOT NULL,
    label        text NOT NULL,
    position     int  NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, object_key, field_key, value)
);
