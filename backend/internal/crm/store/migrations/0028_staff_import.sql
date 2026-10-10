-- Staff can import records from a CSV file (D-139): new businesses get it from the role
-- template; this adds it once to the "Staff access" set of the businesses that exist, on
-- every object they can already create.
UPDATE crm.permission_sets p
   SET rules = jsonb_set(p.rules, '{objects}', (
         SELECT jsonb_object_agg(e.key, CASE WHEN e.value ? 'create' AND NOT e.value ? 'import' THEN e.value || '["import"]'::jsonb ELSE e.value END)
         FROM jsonb_each(p.rules->'objects') e)), updated_at = now()
 WHERE p.system_key = 'STAFF' AND jsonb_typeof(p.rules->'objects') = 'object' AND p.rules->'objects' <> '{}'::jsonb;
