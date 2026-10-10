-- Integrity hardening after the schema review (D-136). Everything here only adds:
-- constraints are created NOT VALID (existing rows are not re-checked, new writes are) and
-- then validated where the data allows, so an old stray row can never stop a deploy.

-- 1. Tenant keys: every tenant-owned table points at its workspace.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['teams', 'team_members', 'role_assignments', 'membership_permission_sets', 'api_keys', 'invitations',
    'notifications', 'outbox_events', 'workflow_runs', 'webhook_deliveries', 'campaign_recipients', 'unsubscribes', 'message_links',
    'code_counters', 'assignment_state', 'idempotency_keys', 'lead_conversions', 'mail_blocklist'] LOOP
    IF to_regclass('crm.' || t) IS NULL OR EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_workspace_fk') THEN CONTINUE; END IF;
    EXECUTE format('ALTER TABLE crm.%I ADD CONSTRAINT %I FOREIGN KEY (workspace_id) REFERENCES crm.workspaces (id) ON DELETE CASCADE NOT VALID', t, t || '_workspace_fk');
    BEGIN
      EXECUTE format('ALTER TABLE crm.%I VALIDATE CONSTRAINT %I', t, t || '_workspace_fk');
    EXCEPTION WHEN others THEN
      RAISE NOTICE 'crm.%: old rows point at a workspace that no longer exists; the key guards new rows only', t;
    END;
  END LOOP;
END $$;

-- 2. Same tenant: a row can only reference a row of its own workspace. For every foreign key
--    between two tenant tables, a second key on (workspace_id, column) → (workspace_id, id).
DO $$
DECLARE r record; cname text;
BEGIN
  FOR r IN
    SELECT c.conrelid::regclass::text AS child, cc.relname AS child_name, a.attname AS col, c.confrelid::regclass::text AS parent, pc.relname AS parent_name
    FROM pg_constraint c
    JOIN pg_class cc ON cc.oid = c.conrelid JOIN pg_namespace cn ON cn.oid = cc.relnamespace AND cn.nspname = 'crm'
    JOIN pg_class pc ON pc.oid = c.confrelid JOIN pg_namespace pn ON pn.oid = pc.relnamespace AND pn.nspname = 'crm'
    JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
    JOIN pg_attribute pa ON pa.attrelid = c.confrelid AND pa.attnum = c.confkey[1] AND pa.attname = 'id'
    JOIN pg_attribute cw ON cw.attrelid = c.conrelid AND cw.attname = 'workspace_id' AND NOT cw.attisdropped
    JOIN pg_attribute pw ON pw.attrelid = c.confrelid AND pw.attname = 'workspace_id' AND NOT pw.attisdropped AND pw.attnotnull
    WHERE c.contype = 'f' AND array_length(c.conkey, 1) = 1 AND a.attname <> 'workspace_id' AND pc.relname <> 'workspaces'
  LOOP
    cname := left(r.child_name || '_' || r.col, 52) || '_same_ws';
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = cname) THEN CONTINUE; END IF;
    EXECUTE format('CREATE UNIQUE INDEX IF NOT EXISTS %I ON %s (workspace_id, id)', left(r.parent_name, 50) || '_ws_id_key', r.parent);
    EXECUTE format('ALTER TABLE %s ADD CONSTRAINT %I FOREIGN KEY (workspace_id, %I) REFERENCES %s (workspace_id, id) NOT VALID', r.child, cname, r.col, r.parent);
    BEGIN
      EXECUTE format('ALTER TABLE %s VALIDATE CONSTRAINT %I', r.child, cname);
    EXCEPTION WHEN others THEN
      RAISE NOTICE '%.%: old rows cross workspaces; the key guards new rows only', r.child, r.col;
    END;
  END LOOP;
END $$;

-- 3. Typed keys: a foreign key into crm.object_records must land on the right kind of record
--    (an allocation's invoice_id on an invoice, not on a task).
CREATE OR REPLACE FUNCTION crm.check_record_type() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE i int := 0; rid uuid; want text; got text;
BEGIN
  WHILE i < TG_NARGS LOOP
    EXECUTE format('SELECT ($1).%I', TG_ARGV[i]) INTO rid USING NEW;
    want := TG_ARGV[i + 1];
    got := NULL;
    IF rid IS NOT NULL THEN
      SELECT r.object_key INTO got FROM crm.object_records r WHERE r.id = rid;
      IF got IS DISTINCT FROM want THEN
        RAISE EXCEPTION '%.% must reference a % record, not %', TG_TABLE_NAME, TG_ARGV[i], want, COALESCE(got, 'nothing')
          USING ERRCODE = 'foreign_key_violation';
      END IF;
    END IF;
    i := i + 2;
  END LOOP;
  RETURN NEW;
END $$;

DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    ('payment_allocations', ARRAY['payment_id', 'payments', 'invoice_id', 'invoices']),
    ('refund_allocations', ARRAY['refund_id', 'refunds', 'payment_id', 'payments', 'invoice_id', 'invoices']),
    ('credit_allocations', ARRAY['credit_note_id', 'credit_notes', 'invoice_id', 'invoices']),
    ('sla_timers', ARRAY['case_id', 'cases', 'policy_id', 'sla_policies']),
    ('price_book_entries', ARRAY['price_book_id', 'price_books', 'catalog_item_id', 'catalog_items']),
    ('product_bundle_items', ARRAY['bundle_item_id', 'catalog_items', 'component_item_id', 'catalog_items']),
    ('territory_assignments', ARRAY['territory_id', 'territories']),
    ('entitlement_usage', ARRAY['entitlement_id', 'entitlements'])) AS v(tbl, args)
  LOOP
    IF to_regclass('crm.' || r.tbl) IS NULL THEN CONTINUE; END IF;
    EXECUTE format('DROP TRIGGER IF EXISTS record_type_check ON crm.%I', r.tbl);
    EXECUTE format('CREATE TRIGGER record_type_check BEFORE INSERT OR UPDATE ON crm.%I FOR EACH ROW EXECUTE FUNCTION crm.check_record_type(%s)',
      r.tbl, (SELECT string_agg(quote_literal(x), ', ') FROM unnest(r.args) x));
  END LOOP;
END $$;

-- 4. Money documents live in crm.object_records with their links inside the record. The
--    database now checks those links itself: a line item, payment, refund or note can only
--    point at a record that exists, in the same workspace, of the right kind.
CREATE OR REPLACE FUNCTION crm.check_money_links() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE links text[]; i int := 1; v text; ok boolean;
BEGIN
  links := CASE NEW.object_key
    WHEN 'line_items'   THEN ARRAY['quoteId', 'quotes', 'orderId', 'sales_orders', 'invoiceId', 'invoices', 'opportunityId', 'opportunities',
                                   'contractId', 'contracts', 'workOrderId', 'work_orders', 'creditNoteId', 'credit_notes', 'itemId', 'catalog_items']
    WHEN 'payments'     THEN ARRAY['invoiceId', 'invoices', 'orderId', 'sales_orders', 'accountId', '@accounts', 'contactId', '@contacts']
    WHEN 'refunds'      THEN ARRAY['paymentId', 'payments', 'accountId', '@accounts']
    WHEN 'credit_notes' THEN ARRAY['invoiceId', 'invoices', 'accountId', '@accounts']
    WHEN 'debit_notes'  THEN ARRAY['invoiceId', 'invoices', 'accountId', '@accounts']
    WHEN 'adjustments'  THEN ARRAY['invoiceId', 'invoices', 'accountId', '@accounts']
    WHEN 'invoices'     THEN ARRAY['orderId', 'sales_orders', 'quoteId', 'quotes', 'accountId', '@accounts', 'contactId', '@contacts']
    WHEN 'sales_orders' THEN ARRAY['quoteId', 'quotes', 'accountId', '@accounts', 'contactId', '@contacts']
    WHEN 'quotes'       THEN ARRAY['opportunityId', 'opportunities', 'accountId', '@accounts', 'contactId', '@contacts']
    ELSE NULL END;
  IF links IS NULL THEN RETURN NEW; END IF;
  WHILE i < array_length(links, 1) LOOP
    v := NEW.custom->>links[i];
    IF v IS NOT NULL AND v <> '' AND (TG_OP = 'INSERT' OR v IS DISTINCT FROM (OLD.custom->>links[i])) THEN
      IF v !~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        ok := false;
      ELSIF links[i + 1] = '@accounts' THEN
        SELECT EXISTS (SELECT 1 FROM crm.accounts a WHERE a.id = v::uuid AND a.workspace_id = NEW.workspace_id) INTO ok;
      ELSIF links[i + 1] = '@contacts' THEN
        SELECT EXISTS (SELECT 1 FROM crm.contacts c WHERE c.id = v::uuid AND c.workspace_id = NEW.workspace_id) INTO ok;
      ELSE
        SELECT EXISTS (SELECT 1 FROM crm.object_records r WHERE r.id = v::uuid AND r.workspace_id = NEW.workspace_id AND r.object_key = links[i + 1]) INTO ok;
      END IF;
      IF NOT ok THEN
        RAISE EXCEPTION '%.% must reference a % of the same workspace', NEW.object_key, links[i], ltrim(links[i + 1], '@')
          USING ERRCODE = 'foreign_key_violation';
      END IF;
    END IF;
    i := i + 2;
  END LOOP;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS money_links_check ON crm.object_records;
CREATE TRIGGER money_links_check BEFORE INSERT OR UPDATE OF custom ON crm.object_records FOR EACH ROW EXECUTE FUNCTION crm.check_money_links();

-- 5. A scanned card's link to a lead or contact follows the card.
DO $$
BEGIN
  IF to_regclass('public.saved_cards') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'card_links_card_fk') THEN
    ALTER TABLE crm.card_links ADD CONSTRAINT card_links_card_fk FOREIGN KEY (card_id) REFERENCES public.saved_cards (id) ON DELETE CASCADE NOT VALID;
    BEGIN
      ALTER TABLE crm.card_links VALIDATE CONSTRAINT card_links_card_fk;
    EXCEPTION WHEN others THEN
      RAISE NOTICE 'crm.card_links: old rows point at cards that no longer exist; the key guards new rows only';
    END;
  END IF;
END $$;

-- 6. The lookups every object is filtered and reported by.
CREATE INDEX IF NOT EXISTS object_records_account_idx ON crm.object_records (workspace_id, object_key, (custom->>'accountId')) WHERE deleted_at IS NULL AND custom ? 'accountId';
CREATE INDEX IF NOT EXISTS object_records_contact_idx ON crm.object_records (workspace_id, object_key, (custom->>'contactId')) WHERE deleted_at IS NULL AND custom ? 'contactId';
CREATE INDEX IF NOT EXISTS object_records_opportunity_idx ON crm.object_records (workspace_id, object_key, (custom->>'opportunityId')) WHERE deleted_at IS NULL AND custom ? 'opportunityId';
