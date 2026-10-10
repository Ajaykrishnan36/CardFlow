-- An Admin can add and manage staff (D-138). New businesses get this from the role template;
-- this gives it once to the "Admin access" set of the businesses that already exist. An
-- Admin can still never give more access than they have themselves.
UPDATE crm.permission_sets
   SET rules = jsonb_set(rules, '{capabilities}', COALESCE(rules->'capabilities', '[]'::jsonb) || '["members.manage"]'::jsonb), updated_at = now()
 WHERE system_key = 'ADMIN' AND NOT COALESCE(rules->'capabilities', '[]'::jsonb) ? 'members.manage';
