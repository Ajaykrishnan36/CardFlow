import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CalendarClock, FileOutput, GitBranch, MapPin, Megaphone, Package, Pencil, Plus, Receipt, ShieldCheck, Tag, UserCheck, Users, Wrench, X } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { commerceApi, type BundleComponent, type PriceLineInput, type PriceResult } from '@crm/api/endpoints';
import type { LookupTarget, LookupValue, ObjectKey, RecordRow } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Checkbox, Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { formatMoney } from '@crm/lib/money';
import { cn } from '@crm/lib/utils';
import { humanize, recordKeys } from './use-object-meta';
import { recordHref, useRecordScope } from './record-scope';

// Record-page panels of the commercial, sales-execution and service model (D-118…D-127):
// priced lines of a document, price book entries and bundles, contact roles, the record's
// team, territories, campaigns, credit, entitlement usage and field-service actions.
// Everything reads and writes through the API; the server decides what is allowed.

export function codeOf(prefix: string): string {
  return prefix.startsWith('/w/') ? decodeURIComponent(prefix.slice(3)) : '';
}

export function firstError(e: unknown): string {
  return isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : 'Something went wrong. Try again.';
}

function Panel({ icon, title, count, actions, children }: { icon: ReactNode; title: string; count?: number; actions?: ReactNode; children: ReactNode }) {
  return (
    <Card className="overflow-hidden">
      <CardHeader
        className="px-4 py-2.5"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <span className="text-muted-foreground [&>svg]:size-3.5">{icon}</span> {title}
            {count != null ? <span className="rounded-full bg-muted px-1.5 text-[11px] font-medium tabular-nums text-muted-foreground">{count}</span> : null}
          </span>
        }
        actions={actions}
      />
      {children}
    </Card>
  );
}

const Empty = ({ children }: { children: ReactNode }) => <p className="px-4 py-3 text-[13px] text-muted-foreground">{children}</p>;
const Loading = () => (
  <div className="space-y-2 px-4 py-3">
    <Skeleton className="h-4 w-2/3" />
    <Skeleton className="h-4 w-1/2" />
  </div>
);

function RemoveButton({ label, onClick, disabled }: { label: string; onClick: () => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      className="grid size-6 shrink-0 place-items-center rounded text-muted-foreground opacity-60 hover:bg-muted hover:text-danger group-hover:opacity-100"
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      <X className="size-3.5" />
    </button>
  );
}

/** Search-and-pick for one record of an object (or a person). */
export function RecordPicker({ target, value, onPick, placeholder, exclude }: { target: LookupTarget; value: LookupValue | null; onPick: (v: LookupValue | null) => void; placeholder?: string; exclude?: string[] }) {
  const scope = useRecordScope();
  const [q, setQ] = useState('');
  const searchQ = useQuery({ queryKey: ['picker', scope.prefix, target, q], queryFn: () => scope.api.lookup(target, q), enabled: !value });
  if (value) {
    return (
      <div className="flex items-center justify-between rounded-md border bg-muted/40 px-3 py-2 text-[13px]">
        <span className="truncate font-medium">{value.label}</span>
        <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => onPick(null)}>
          Change
        </button>
      </div>
    );
  }
  const rows = (searchQ.data ?? []).filter((r) => !exclude?.includes(r.id));
  return (
    <>
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={placeholder ?? `Search ${humanize(target).toLowerCase()}…`} />
      <ul className="mt-1.5 max-h-40 overflow-y-auto rounded-md border">
        {rows.length === 0 ? (
          <li className="px-3 py-2 text-[13px] text-muted-foreground">{searchQ.isLoading ? 'Searching…' : 'Nothing found.'}</li>
        ) : (
          rows.map((r) => (
            <li key={r.id}>
              <button type="button" className="block w-full truncate px-3 py-2 text-left text-[13px] hover:bg-muted" onClick={() => onPick(r)}>
                {r.label}
              </button>
            </li>
          ))
        )}
      </ul>
    </>
  );
}

function FormDialog({ title, description, error, onClose, onSubmit, submit, busy, disabled, children, wide }: {
  title: string; description?: ReactNode; error?: string | null; onClose: () => void; onSubmit: () => void; submit: string; busy?: boolean; disabled?: boolean; children: ReactNode; wide?: boolean;
}) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className={cn('p-0', wide ? 'max-w-3xl' : 'max-w-md')}>
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
          {description ? <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{description}</DialogDescription> : null}
        </div>
        <form
          className="max-h-[70vh] space-y-3 overflow-y-auto px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          {children}
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={busy} disabled={disabled}>
              {submit}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- document lines (CPQ)

const LINE_OBJECTS = ['quotes', 'sales_orders', 'invoices', 'contracts', 'work_orders', 'opportunities', 'credit_notes'];
export const hasLines = (object: string) => LINE_OBJECTS.includes(object);

const approvalTone = { pending: 'warning', approved: 'success', rejected: 'danger', cancelled: 'neutral' } as const;

export function DocumentLinesCard({ object, record }: { object: ObjectKey; record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const api = commerceApi(code);
  const key = ['doc-lines', code, object, record.id];
  const linesQ = useQuery({ queryKey: [...key, record.version], queryFn: () => api.lines(object, record.id), enabled: Boolean(code) });
  const [editing, setEditing] = useState(false);
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: key });
    void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, object, record.id) });
  };
  const next = useMutation({
    mutationFn: (kind: 'convert' | 'invoice' | 'revise' | 'wo-invoice') =>
      kind === 'convert' ? api.convertQuote(record.id) : kind === 'invoice' ? api.invoiceOrder(record.id) : kind === 'revise' ? api.reviseQuote(record.id) : api.invoiceWorkOrder(record.id),
    onSuccess: (r) => {
      toast.success(`Created ${r.code}`);
      refresh();
      navigate(recordHref(scope, r.object, r.id));
    },
    onError: (e) => toast.error(firstError(e))
  });
  if (!code) return null;
  const d = linesQ.data;
  const p = d?.pricing;
  const money = (n: number) => formatMoney(n, p?.currency);
  const status = String(record.values.status ?? '');
  const approval = String(record.values.approvalStatus ?? '');
  return (
    <Card className="mb-4 overflow-hidden">
      <CardHeader
        className="px-4 py-3"
        title={
          <span className="flex flex-wrap items-center gap-2 text-[13px]">
            <Receipt className="size-4 text-muted-foreground" aria-hidden /> Items and pricing
            {p?.priceBook ? <span className="text-xs font-normal text-muted-foreground">· {p.priceBook}</span> : null}
            {object === 'quotes' && d?.approvalRequest && d.approvalRequest.status !== 'cancelled' ? (
              <Badge tone={approvalTone[d.approvalRequest.status]}>Discount {d.approvalRequest.status === 'pending' ? 'waiting for approval' : d.approvalRequest.status}</Badge>
            ) : null}
          </span>
        }
        actions={
          <span className="flex flex-wrap gap-1.5">
            {d?.editable ? (
              <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                <Pencil /> {p && p.lines.length ? 'Edit items' : 'Add items'}
              </Button>
            ) : null}
            {object === 'quotes' && scope.can('quotes', 'create') && p && p.lines.length > 0 ? (
              <Button size="sm" variant="ghost" loading={next.isPending && next.variables === 'revise'} onClick={() => next.mutate('revise')}>
                <GitBranch /> New version
              </Button>
            ) : null}
            {object === 'quotes' && scope.can('sales_orders', 'create') && p && p.lines.length > 0 && !['declined', 'expired'].includes(status) && approval !== 'pending' && approval !== 'rejected' ? (
              <Button size="sm" loading={next.isPending && next.variables === 'convert'} onClick={() => next.mutate('convert')}>
                <FileOutput /> Create order
              </Button>
            ) : null}
            {object === 'sales_orders' && scope.can('invoices', 'create') && status !== 'cancelled' ? (
              <Button size="sm" loading={next.isPending} onClick={() => next.mutate('invoice')}>
                <FileOutput /> Create invoice
              </Button>
            ) : null}
            {object === 'work_orders' && scope.can('invoices', 'create') && status === 'completed' && !record.values.invoiceId ? (
              <Button size="sm" loading={next.isPending} onClick={() => next.mutate('wo-invoice')}>
                <FileOutput /> Create invoice
              </Button>
            ) : null}
          </span>
        }
      />
      {linesQ.isLoading || !p ? (
        <Loading />
      ) : p.lines.length === 0 ? (
        <Empty>No items yet.{d?.editable ? ' Add products and services: prices come from the price book, with the rules and discounts that apply.' : ''}</Empty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-[13px]">
            <thead>
              <tr className="border-y bg-muted/40 text-left text-xs font-medium text-muted-foreground">
                <th className="px-4 py-2">Item</th>
                <th className="px-3 py-2 text-right">Qty</th>
                <th className="px-3 py-2 text-right">List price</th>
                <th className="px-3 py-2 text-right">Price</th>
                <th className="px-3 py-2 text-right">Discount</th>
                <th className="px-3 py-2 text-right">Tax</th>
                <th className="px-4 py-2 text-right">Amount</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {p.lines.map((l, i) => (
                <tr key={i} className={cn(l.bundleOf != null && 'bg-muted/20')}>
                  <td className={cn('px-4 py-2', l.bundleOf != null && 'pl-8')}>
                    {l.itemId ? (
                      <Link to={recordHref(scope, 'catalog_items', l.itemId)} className="font-medium text-primary hover:underline">
                        {l.name}
                      </Link>
                    ) : (
                      <span className="font-medium">{l.name}</span>
                    )}
                    {l.pricingNote ? <p className="text-xs text-muted-foreground">{l.pricingNote}</p> : null}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">{l.quantity}</td>
                  <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">{money(l.listPrice)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{money(l.unitPrice)}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{l.discountAmount ? `−${money(l.discountAmount)}` : '—'}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{l.taxAmount ? money(l.taxAmount) : '—'}</td>
                  <td className="px-4 py-2 text-right font-medium tabular-nums">{money(l.total)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot className="text-[13px]">
              <tr className="border-t">
                <td colSpan={6} className="px-3 py-1.5 text-right text-muted-foreground">Subtotal at list price</td>
                <td className="px-4 py-1.5 text-right tabular-nums">{money(p.subtotal)}</td>
              </tr>
              {p.discount ? (
                <tr>
                  <td colSpan={6} className="px-3 py-1.5 text-right text-muted-foreground">Discounts</td>
                  <td className="px-4 py-1.5 text-right tabular-nums">−{money(p.discount)}</td>
                </tr>
              ) : null}
              <tr>
                <td colSpan={6} className="px-3 py-1.5 text-right text-muted-foreground">Tax</td>
                <td className="px-4 py-1.5 text-right tabular-nums">{money(p.tax)}</td>
              </tr>
              <tr className="border-t bg-muted/40 font-semibold">
                <td colSpan={6} className="px-3 py-2 text-right">Total</td>
                <td className="px-4 py-2 text-right tabular-nums">{money(p.total)}</td>
              </tr>
            </tfoot>
          </table>
        </div>
      )}
      {d && !d.editable && p && p.lines.length > 0 && ['quotes', 'sales_orders', 'invoices', 'credit_notes'].includes(object) ? (
        <p className="border-t px-4 py-2 text-xs text-muted-foreground">These prices are fixed: this {humanize(object).toLowerCase().replace(/s$/, '')} is no longer a draft, so later price changes don't affect it.</p>
      ) : null}
      {editing && p ? <LinesEditor object={object} record={record} current={p} onClose={() => setEditing(false)} onSaved={refresh} /> : null}
    </Card>
  );
}

interface EditLine {
  key: number;
  itemId?: string;
  name: string;
  quantity: string;
  discountPercent: string;
  unitPrice: string;
}

function LinesEditor({ object, record, current, onClose, onSaved }: { object: ObjectKey; record: RecordRow; current: PriceResult; onClose: () => void; onSaved: () => void }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const api = commerceApi(code);
  const canOverride = scope.hasCapability('pricing.manage');
  // Bundle components are added by the server; only the top-level lines are edited.
  const [rows, setRows] = useState<EditLine[]>(() =>
    current.lines
      .filter((l) => l.bundleOf == null)
      .map((l, i) => ({ key: i, itemId: l.itemId, name: l.name, quantity: String(l.quantity), discountPercent: l.discountPercent ? String(l.discountPercent) : '', unitPrice: l.itemId ? '' : String(l.unitPrice) }))
  );
  const [book, setBook] = useState<LookupValue | null>(current.priceBookId ? { id: current.priceBookId, label: current.priceBook || 'Price book' } : null);
  const [adding, setAdding] = useState<LookupValue | null>(null);
  const [freeText, setFreeText] = useState('');
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (adding) {
      setRows((r) => [...r, { key: Date.now(), itemId: adding.id, name: adding.label.replace(/ · [A-Z]+-\d+$/, ''), quantity: '1', discountPercent: '', unitPrice: '' }]);
      setAdding(null);
    }
  }, [adding]);
  const body = useMemo(() => {
    const lines: PriceLineInput[] = rows.map((r) => ({
      itemId: r.itemId,
      name: r.itemId ? undefined : r.name,
      quantity: Number(r.quantity) || 0,
      discountPercent: r.discountPercent === '' ? undefined : Number(r.discountPercent),
      unitPrice: r.unitPrice === '' ? undefined : Number(r.unitPrice)
    }));
    return { priceBookId: book?.id, accountId: typeof record.values.accountId === 'string' ? record.values.accountId : undefined, currency: typeof record.values.currency === 'string' ? record.values.currency : undefined, lines };
  }, [rows, book, record.values.accountId, record.values.currency]);
  // Live price: what saving would produce, including rule discounts, tax and whether approval is needed.
  const previewQ = useQuery({ queryKey: ['price-preview', code, body], queryFn: () => api.preview(body), enabled: rows.length > 0, retry: false, placeholderData: (prev) => prev });
  const save = useMutation({
    mutationFn: () => api.putLines(object, record.id, body),
    onSuccess: () => {
      toast.success('Items saved');
      onSaved();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  const pv = previewQ.data;
  const problem = previewQ.isError ? firstError(previewQ.error) : null;
  const money = (n: number) => formatMoney(n, pv?.currency ?? current.currency);
  const set = (key: number, patch: Partial<EditLine>) => setRows((r) => r.map((x) => (x.key === key ? { ...x, ...patch } : x)));
  // The priced top-level line for each row, in order.
  const top = (pv?.lines ?? []).filter((l) => l.bundleOf == null);

  return (
    <FormDialog wide title="Items and pricing" description="Prices come from the price book. Discount rules, bundle contents and tax are worked out as you go." error={error ?? problem} onClose={onClose}
      onSubmit={() => { setError(null); save.mutate(); }} submit="Save items" busy={save.isPending} disabled={Boolean(problem)}>
      <Field label="Price book" hint="Leave empty to use this customer's own price book, or the default one.">
        <RecordPicker target="price_books" value={book} onPick={setBook} />
      </Field>
      <div className="overflow-x-auto rounded-md border">
        <table className="w-full min-w-[620px] text-[13px]">
          <thead>
            <tr className="border-b bg-muted/40 text-left text-xs font-medium text-muted-foreground">
              <th className="px-3 py-2">Item</th>
              <th className="w-20 px-2 py-2">Qty</th>
              <th className="w-24 px-2 py-2">Discount %</th>
              <th className="w-28 px-2 py-2">{canOverride ? 'Price override' : 'Price'}</th>
              <th className="w-28 px-3 py-2 text-right">Amount</th>
              <th className="w-8" />
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-3 py-4 text-center text-muted-foreground">Add a product or service below.</td>
              </tr>
            ) : null}
            {rows.map((r, i) => (
              <tr key={r.key} className="group">
                <td className="px-3 py-1.5">
                  <span className="font-medium">{r.name}</span>
                  {top[i]?.pricingNote && !problem ? <p className="text-xs text-muted-foreground">{top[i]!.pricingNote}</p> : null}
                </td>
                <td className="px-2 py-1.5">
                  <Input type="number" min="0" step="any" value={r.quantity} onChange={(e) => set(r.key, { quantity: e.target.value })} aria-label={`Quantity of ${r.name}`} />
                </td>
                <td className="px-2 py-1.5">
                  <Input type="number" min="0" max="100" step="any" value={r.discountPercent} onChange={(e) => set(r.key, { discountPercent: e.target.value })} aria-label={`Discount on ${r.name}`} />
                </td>
                <td className="px-2 py-1.5">
                  {canOverride || !r.itemId ? (
                    <Input type="number" min="0" step="0.01" value={r.unitPrice} placeholder={top[i] && !problem ? String(top[i]!.unitPrice) : ''} onChange={(e) => set(r.key, { unitPrice: e.target.value })} aria-label={`Price of ${r.name}`} />
                  ) : (
                    <span className="tabular-nums">{top[i] && !problem ? money(top[i]!.unitPrice) : '—'}</span>
                  )}
                </td>
                <td className="px-3 py-1.5 text-right font-medium tabular-nums">{top[i] && !problem ? money(top[i]!.total) : '—'}</td>
                <td className="pr-2">
                  <RemoveButton label={`Remove ${r.name}`} onClick={() => setRows((x) => x.filter((y) => y.key !== r.key))} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Add a product or service">
          <RecordPicker target="catalog_items" value={adding} onPick={setAdding} placeholder="Search the catalog…" />
        </Field>
        <Field label="Or a line that isn't in the catalog" hint="Type a description, press Add, then give it a price.">
          <div className="flex gap-2">
            <Input value={freeText} onChange={(e) => setFreeText(e.target.value)} placeholder="e.g. On-site installation" />
            <Button type="button" variant="outline" disabled={!freeText.trim()} onClick={() => { setRows((r) => [...r, { key: Date.now(), name: freeText.trim(), quantity: '1', discountPercent: '', unitPrice: '0' }]); setFreeText(''); }}>
              Add
            </Button>
          </div>
        </Field>
      </div>
      {pv && !problem && rows.length > 0 ? (
        <dl className="ml-auto grid w-full max-w-xs grid-cols-2 gap-y-1 text-[13px]">
          <dt className="text-muted-foreground">Subtotal at list price</dt>
          <dd className="text-right tabular-nums">{money(pv.subtotal)}</dd>
          <dt className="text-muted-foreground">Discounts</dt>
          <dd className="text-right tabular-nums">−{money(pv.discount)}</dd>
          <dt className="text-muted-foreground">Tax</dt>
          <dd className="text-right tabular-nums">{money(pv.tax)}</dd>
          <dt className="font-semibold">Total</dt>
          <dd className="text-right font-semibold tabular-nums">{money(pv.total)}</dd>
        </dl>
      ) : null}
      {pv?.approval.required && object === 'quotes' && !problem ? (
        <Alert tone="warning" title="This discount needs approval">
          The largest discount is {pv.maxDiscountPercent}% off the list price. Saving sends it for approval{pv.approval.label ? ` (${pv.approval.label})` : ''}; the quote can be sent once it is approved.
        </Alert>
      ) : null}
    </FormDialog>
  );
}

// ---------------------------------------------------------------- price book entries and bundles

export function PriceEntriesCard({ mode, record }: { mode: 'book' | 'item'; record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['price-entries', code, mode, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.entries(mode === 'book' ? { priceBookId: record.id } : { itemId: record.id }), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const refresh = () => void qc.invalidateQueries({ queryKey: key });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteEntry(id), onSuccess: () => { toast.success('Price removed'); refresh(); }, onError: (e) => toast.error(firstError(e)) });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const canManage = listQ.data?.canManage ?? false;
  return (
    <Panel icon={<Tag />} title={mode === 'book' ? 'Prices in this book' : 'Prices'} count={rows.length}
      actions={canManage ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus /> Price</Button> : null}>
      {listQ.isLoading ? <Loading /> : rows.length === 0 ? (
        <Empty>{mode === 'book' ? 'This price book has no prices yet. Add the products and services it covers.' : 'Not in any price book yet: quotes use the catalog price.'}</Empty>
      ) : (
        <ul className="divide-y">
          {rows.map((e) => (
            <li key={e.id} className="group flex items-center gap-2 px-4 py-2 text-[13px]">
              <div className="min-w-0 flex-1">
                <Link to={recordHref(scope, mode === 'book' ? 'catalog_items' : 'price_books', mode === 'book' ? e.itemId : e.priceBookId)} className="font-medium text-primary hover:underline">
                  {mode === 'book' ? e.item : e.priceBook}
                </Link>
                <p className="text-xs text-muted-foreground">
                  {Number(e.minQuantity) > 1 ? `From ${e.minQuantity} · ` : ''}from {e.validFrom}{e.validTo ? ` to ${e.validTo}` : ''}
                  {e.maxDiscountPercent ? ` · max ${Number(e.maxDiscountPercent)}% off` : ''}{e.isActive ? '' : ' · inactive'}
                </p>
              </div>
              <span className="text-right tabular-nums">
                <span className="font-medium">{formatMoney(e.unitPrice, e.currency)}</span>
                {e.listPrice > e.unitPrice ? <span className="block text-xs text-muted-foreground line-through">{formatMoney(e.listPrice, e.currency)}</span> : null}
              </span>
              {canManage ? <RemoveButton label="Remove this price" disabled={remove.isPending} onClick={() => remove.mutate(e.id)} /> : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? <EntryDialog mode={mode} record={record} baseCurrency={listQ.data?.base ?? 'INR'} onClose={() => setAdding(false)} onDone={refresh} /> : null}
    </Panel>
  );
}

function EntryDialog({ mode, record, baseCurrency, onClose, onDone }: { mode: 'book' | 'item'; record: RecordRow; baseCurrency: string; onClose: () => void; onDone: () => void }) {
  const scope = useRecordScope();
  const api = commerceApi(codeOf(scope.prefix));
  const [other, setOther] = useState<LookupValue | null>(null);
  const [f, setF] = useState({ unitPrice: '', listPrice: '', currency: baseCurrency, minQuantity: '1', maxDiscountPercent: '', validFrom: new Date().toISOString().slice(0, 10), validTo: '' });
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () =>
      api.saveEntry({
        priceBookId: mode === 'book' ? record.id : other?.id, itemId: mode === 'book' ? other?.id : record.id, currency: f.currency.toUpperCase(), unitPrice: Number(f.unitPrice),
        listPrice: f.listPrice === '' ? Number(f.unitPrice) : Number(f.listPrice), minQuantity: Number(f.minQuantity) || 1,
        maxDiscountPercent: f.maxDiscountPercent === '' ? undefined : Number(f.maxDiscountPercent), validFrom: f.validFrom, validTo: f.validTo || undefined
      }),
    onSuccess: () => { toast.success('Price saved'); onDone(); onClose(); },
    onError: (e) => setError(firstError(e))
  });
  const up = (k: keyof typeof f) => (e: { target: { value: string } }) => setF((x) => ({ ...x, [k]: e.target.value }));
  return (
    <FormDialog title="Add a price" description="A price for one item in one price book, from a date. A new price from a later date replaces the old one; the old one is kept as history."
      error={error} onClose={onClose} onSubmit={() => { setError(null); save.mutate(); }} submit="Save price" busy={save.isPending} disabled={!other || f.unitPrice === ''}>
      <Field label={mode === 'book' ? 'Product or service' : 'Price book'}>
        <RecordPicker target={mode === 'book' ? 'catalog_items' : 'price_books'} value={other} onPick={setOther} />
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Selling price"><Input type="number" min="0" step="0.01" value={f.unitPrice} onChange={up('unitPrice')} /></Field>
        <Field label="List price" hint="If higher, the difference shows as a discount."><Input type="number" min="0" step="0.01" value={f.listPrice} onChange={up('listPrice')} placeholder={f.unitPrice} /></Field>
        <Field label="Currency"><Input value={f.currency} onChange={up('currency')} maxLength={3} /></Field>
        <Field label="From quantity" hint="For volume prices."><Input type="number" min="0" step="any" value={f.minQuantity} onChange={up('minQuantity')} /></Field>
        <Field label="Valid from"><Input type="date" value={f.validFrom} onChange={up('validFrom')} /></Field>
        <Field label="Valid to"><Input type="date" value={f.validTo} onChange={up('validTo')} /></Field>
      </div>
      <Field label="Largest discount allowed (%)" hint="Empty = no limit of its own."><Input type="number" min="0" max="100" step="any" value={f.maxDiscountPercent} onChange={up('maxDiscountPercent')} /></Field>
    </FormDialog>
  );
}

export function BundleCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['bundle', code, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.bundle(record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const [pick, setPick] = useState<LookupValue | null>(null);
  const [form, setForm] = useState({ quantity: '1', required: true, included: false });
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (components: BundleComponent[]) => api.saveBundle(record.id, components),
    onSuccess: () => {
      toast.success('Bundle saved');
      setAdding(false);
      setPick(null);
      void qc.invalidateQueries({ queryKey: key });
      void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, 'catalog_items', record.id) });
    },
    onError: (e) => (adding ? setError(firstError(e)) : toast.error(firstError(e)))
  });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const canManage = listQ.data?.canManage ?? false;
  return (
    <Panel icon={<Package />} title="Bundle contents" count={rows.length} actions={canManage ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus /> Component</Button> : null}>
      {listQ.isLoading ? <Loading /> : rows.length === 0 ? (
        <Empty>Not a bundle. Add components to sell this item together with others in one line.</Empty>
      ) : (
        <ul className="divide-y">
          {rows.map((c) => (
            <li key={c.itemId} className="group flex items-center gap-2 px-4 py-2 text-[13px]">
              <div className="min-w-0 flex-1">
                <Link to={recordHref(scope, 'catalog_items', c.itemId)} className="font-medium text-primary hover:underline">{c.item}</Link>
                <p className="text-xs text-muted-foreground">{c.quantity} per bundle · {c.required ? 'always included' : 'optional'} · {c.priceMode === 'included' ? 'price included' : 'priced separately'}</p>
              </div>
              {canManage ? <RemoveButton label={`Remove ${c.item}`} disabled={save.isPending} onClick={() => save.mutate(rows.filter((x) => x.itemId !== c.itemId))} /> : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Add a component" error={error} onClose={() => setAdding(false)} submit="Add" busy={save.isPending} disabled={!pick}
          onSubmit={() => { setError(null); save.mutate([...rows, { itemId: pick!.id, quantity: Number(form.quantity) || 1, required: form.required, priceMode: form.included ? 'included' : 'additional' }]); }}>
          <Field label="Product or service"><RecordPicker target="catalog_items" value={pick} onPick={setPick} exclude={[record.id, ...rows.map((r) => r.itemId)]} /></Field>
          <Field label="Quantity per bundle"><Input type="number" min="0" step="any" value={form.quantity} onChange={(e) => setForm((f) => ({ ...f, quantity: e.target.value }))} /></Field>
          <Checkbox checked={form.required} onCheckedChange={(v) => setForm((f) => ({ ...f, required: Boolean(v) }))} label="Always part of the bundle" description="Off = an option the customer can add." />
          <Checkbox checked={form.included} onCheckedChange={(v) => setForm((f) => ({ ...f, included: Boolean(v) }))} label="Its price is included in the bundle's price" description="Off = charged at its own price on top." />
        </FormDialog>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------- contact roles

const ROLE_OBJECTS = ['opportunities', 'accounts', 'cases', 'contracts', 'work_orders', 'contacts'];
export const hasContactRoles = (object: string) => ROLE_OBJECTS.includes(object);

export function ContactRolesCard({ object, record }: { object: ObjectKey; record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['contact-roles', code, object, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.contactRoles(object, record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const [contact, setContact] = useState<LookupValue | null>(null);
  const [role, setRole] = useState('');
  const [primary, setPrimary] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: key });
  const add = useMutation({
    mutationFn: () => api.addContactRole(object, record.id, { contactId: contact!.id, role: role || (listQ.data?.roles[0] ?? 'Contact'), isPrimary: primary }),
    onSuccess: () => { toast.success('Role added'); setAdding(false); setContact(null); refresh(); },
    onError: (e) => setError(firstError(e))
  });
  const change = useMutation({
    mutationFn: (v: { id: string; body: { isPrimary?: boolean; isActive?: boolean; endDate?: string } }) => api.updateContactRole(v.id, v.body),
    onSuccess: refresh,
    onError: (e) => toast.error(firstError(e))
  });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteContactRole(id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const canEdit = listQ.data?.canEdit ?? false;
  const onContact = object === 'contacts';
  if (onContact && !listQ.isLoading && rows.length === 0) return null;
  return (
    <Panel icon={<UserCheck />} title={onContact ? 'Roles on records' : 'Contact roles'} count={rows.length}
      actions={canEdit ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus /> Role</Button> : null}>
      {listQ.isLoading ? <Loading /> : rows.length === 0 ? (
        <Empty>Nobody has a role here yet.{canEdit ? ' Say who decides, who evaluates, who pays.' : ''}</Empty>
      ) : (
        <ul className="divide-y">
          {rows.map((r) => (
            <li key={r.id} className={cn('group flex items-start gap-2 px-4 py-2 text-[13px]', !r.isActive && 'opacity-60')}>
              <div className="min-w-0 flex-1">
                <Link to={onContact ? recordHref(scope, r.object, r.recordId) : recordHref(scope, 'contacts', r.contactId)} className="font-medium text-primary hover:underline">
                  {onContact ? r.record : r.contact}
                </Link>
                {r.isPrimary ? <Badge tone="primary" className="ml-1.5">Primary</Badge> : null}
                <p className="text-xs text-muted-foreground">
                  {r.role}{onContact ? ` · ${humanize(r.object)}` : ''}{!r.isActive ? ` · ended${r.endDate ? ` ${r.endDate}` : ''}` : ''}
                </p>
              </div>
              {canEdit && r.isActive ? (
                <span className="flex items-center gap-1 text-xs opacity-0 group-hover:opacity-100">
                  {!r.isPrimary ? <button type="button" className="font-medium text-primary hover:underline" onClick={() => change.mutate({ id: r.id, body: { isPrimary: true } })}>Make primary</button> : null}
                  <button type="button" className="font-medium text-muted-foreground hover:underline" onClick={() => change.mutate({ id: r.id, body: { isActive: false, endDate: new Date().toISOString().slice(0, 10) } })}>End</button>
                </span>
              ) : null}
              {canEdit ? <RemoveButton label={`Remove ${r.contact}`} disabled={remove.isPending} onClick={() => remove.mutate(r.id)} /> : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Add a contact role" error={error} onClose={() => setAdding(false)} onSubmit={() => { setError(null); add.mutate(); }} submit="Add role" busy={add.isPending} disabled={!contact}>
          <Field label="Contact"><RecordPicker target="contacts" value={contact} onPick={setContact} exclude={rows.map((r) => r.contactId)} /></Field>
          <Field label="Role">
            <Select value={role || listQ.data?.roles[0] || ''} onChange={(e) => setRole(e.target.value)}>
              {(listQ.data?.roles ?? []).map((r) => <option key={r} value={r}>{r}</option>)}
            </Select>
          </Field>
          <Checkbox checked={primary} onCheckedChange={(v) => setPrimary(Boolean(v))} label="Primary contact" description="One per record; this replaces the current one." />
        </FormDialog>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------- record team

const TEAM_OBJECTS = ['accounts', 'opportunities', 'cases', 'contracts', 'work_orders'];
export const hasRecordTeam = (object: string) => TEAM_OBJECTS.includes(object);
const accessLabel = { read: 'Can view', write: 'Can edit', full: 'Full access' } as const;

export function RecordTeamCard({ object, record }: { object: ObjectKey; record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['record-team', code, object, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.team(object, record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const [person, setPerson] = useState<LookupValue | null>(null);
  const [role, setRole] = useState('');
  const [access, setAccess] = useState<'read' | 'write' | 'full'>('read');
  const [error, setError] = useState<string | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: key });
  const add = useMutation({
    mutationFn: () => api.saveTeamMember(object, record.id, { identityId: person!.id, teamRole: role || (listQ.data?.roles[0] ?? ''), accessLevel: access }),
    onSuccess: () => { toast.success('Added to the team'); setAdding(false); setPerson(null); refresh(); },
    onError: (e) => setError(firstError(e))
  });
  const remove = useMutation({ mutationFn: (id: string) => api.removeTeamMember(object, record.id, id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const canEdit = listQ.data?.canEdit ?? false;
  const owner = record.lookups.ownerId?.label;
  return (
    <Panel icon={<Users />} title={`${humanize(object).replace(/ies$/, 'y').replace(/s$/, '')} team`} count={rows.length}
      actions={canEdit ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus /> Member</Button> : null}>
      {listQ.isLoading ? <Loading /> : (
        <ul className="divide-y">
          {owner ? (
            <li className="flex items-center gap-2 px-4 py-2 text-[13px]">
              <div className="min-w-0 flex-1"><span className="font-medium">{owner}</span><p className="text-xs text-muted-foreground">Owner</p></div>
              <span className="text-xs text-muted-foreground">Full access</span>
            </li>
          ) : null}
          {rows.map((m) => (
            <li key={m.identityId} className="group flex items-center gap-2 px-4 py-2 text-[13px]">
              <div className="min-w-0 flex-1"><span className="font-medium">{m.name}</span><p className="text-xs text-muted-foreground">{m.teamRole || 'Member'}</p></div>
              <span className="text-xs text-muted-foreground">{accessLabel[m.accessLevel]}</span>
              {canEdit ? <RemoveButton label={`Remove ${m.name}`} disabled={remove.isPending} onClick={() => remove.mutate(m.identityId)} /> : null}
            </li>
          ))}
          {rows.length === 0 && !owner ? <Empty>No team yet.</Empty> : null}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Add to the team" description="Team members can open this record even if they normally see only their own." error={error} onClose={() => setAdding(false)}
          onSubmit={() => { setError(null); add.mutate(); }} submit="Add" busy={add.isPending} disabled={!person}>
          <Field label="Person"><RecordPicker target="users" value={person} onPick={setPerson} placeholder="Search people…" exclude={rows.map((r) => r.identityId)} /></Field>
          <Field label="Role on the team">
            <Select value={role || listQ.data?.roles[0] || ''} onChange={(e) => setRole(e.target.value)}>
              {(listQ.data?.roles ?? []).map((r) => <option key={r} value={r}>{r}</option>)}
            </Select>
          </Field>
          <Field label="Access">
            <Select value={access} onChange={(e) => setAccess(e.target.value as 'read' | 'write' | 'full')}>
              <option value="read">Can view</option>
              <option value="write">Can view and edit</option>
              <option value="full">Full access (also delete and manage the team)</option>
            </Select>
          </Field>
        </FormDialog>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------- territories

export function AccountTerritoryCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['account-territories', code, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.accountTerritories(record.id), enabled: Boolean(code) && scope.can('territories', 'read') });
  const [adding, setAdding] = useState(false);
  const [pick, setPick] = useState<LookupValue | null>(null);
  const [error, setError] = useState<string | null>(null);
  const assign = useMutation({
    mutationFn: () => api.assignTerritory(pick!.id, { kind: 'account', accountId: record.id, isPrimary: true }),
    onSuccess: () => { toast.success('Territory changed'); setAdding(false); setPick(null); void qc.invalidateQueries({ queryKey: key }); },
    onError: (e) => setError(firstError(e))
  });
  if (!code || !scope.can('territories', 'read')) return null;
  const rows = listQ.data?.data ?? [];
  const current = rows.filter((r) => !r.effectiveTo);
  const past = rows.filter((r) => r.effectiveTo);
  return (
    <Panel icon={<MapPin />} title="Territory" actions={listQ.data?.canManage ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}>{current.length ? 'Move' : 'Assign'}</Button> : null}>
      {listQ.isLoading ? <Loading /> : current.length === 0 && past.length === 0 ? <Empty>Not in a territory.</Empty> : (
        <ul className="divide-y">
          {[...current, ...past].map((a) => (
            <li key={a.id} className={cn('px-4 py-2 text-[13px]', a.effectiveTo && 'opacity-60')}>
              <Link to={recordHref(scope, 'territories', a.territoryId)} className="font-medium text-primary hover:underline">{a.territory}</Link>
              <p className="text-xs text-muted-foreground">from {a.effectiveFrom}{a.effectiveTo ? ` to ${a.effectiveTo}` : ''}</p>
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Assign a territory" description="The account moves from today. Its open deals move with it; closed deals keep the territory they closed in." error={error}
          onClose={() => setAdding(false)} onSubmit={() => { setError(null); assign.mutate(); }} submit="Assign" busy={assign.isPending} disabled={!pick}>
          <Field label="Territory"><RecordPicker target="territories" value={pick} onPick={setPick} /></Field>
        </FormDialog>
      ) : null}
    </Panel>
  );
}

export function TerritoryMembersCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['territory-assignments', code, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => api.territoryAssignments(record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const [kind, setKind] = useState<'account' | 'user'>('account');
  const [pick, setPick] = useState<LookupValue | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: key });
  const assign = useMutation({
    mutationFn: () => api.assignTerritory(record.id, kind === 'account' ? { kind, accountId: pick!.id } : { kind, identityId: pick!.id }),
    onSuccess: () => { toast.success('Assigned'); setAdding(false); setPick(null); refresh(); },
    onError: (e) => setError(firstError(e))
  });
  const end = useMutation({ mutationFn: (id: string) => api.endAssignment(id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const canManage = listQ.data?.canManage ?? false;
  return (
    <Panel icon={<MapPin />} title="Accounts and people" count={rows.length} actions={canManage ? <Button size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus /> Assign</Button> : null}>
      {listQ.isLoading ? <Loading /> : rows.length === 0 ? <Empty>Nothing is assigned to this territory yet.</Empty> : (
        <ul className="divide-y">
          {rows.map((a) => (
            <li key={a.id} className="group flex items-center gap-2 px-4 py-2 text-[13px]">
              <div className="min-w-0 flex-1">
                {a.kind === 'account' ? <Link to={recordHref(scope, 'accounts', a.targetId)} className="font-medium text-primary hover:underline">{a.target}</Link> : <span className="font-medium">{a.target}</span>}
                <p className="text-xs text-muted-foreground">{a.kind === 'account' ? 'Account' : a.kind === 'user' ? 'Person' : 'Team'} · from {a.effectiveFrom}</p>
              </div>
              {canManage ? <RemoveButton label={`End the assignment of ${a.target}`} disabled={end.isPending} onClick={() => end.mutate(a.id)} /> : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Assign to this territory" error={error} onClose={() => setAdding(false)} onSubmit={() => { setError(null); assign.mutate(); }} submit="Assign" busy={assign.isPending} disabled={!pick}>
          <Field label="What">
            <Select value={kind} onChange={(e) => { setKind(e.target.value as 'account' | 'user'); setPick(null); }}>
              <option value="account">An account</option>
              <option value="user">A person</option>
            </Select>
          </Field>
          <Field label={kind === 'account' ? 'Account' : 'Person'}><RecordPicker key={kind} target={kind === 'account' ? 'accounts' : 'users'} value={pick} onPick={setPick} /></Field>
        </FormDialog>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------- campaigns of a lead or contact

export function RecordCampaignsCard({ object, record }: { object: ObjectKey; record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const listQ = useQuery({ queryKey: ['record-campaigns', code, object, record.id], queryFn: () => commerceApi(code).recordCampaigns(object, record.id), enabled: Boolean(code) });
  const rows = listQ.data?.data ?? [];
  if (!code || rows.length === 0) return null;
  return (
    <Panel icon={<Megaphone />} title="Campaigns" count={rows.length}>
      <ul className="divide-y">
        {rows.map((m) => (
          <li key={m.id} className="flex items-center gap-2 px-4 py-2 text-[13px]">
            <div className="min-w-0 flex-1"><span className="font-medium">{m.campaign}</span><p className="text-xs text-muted-foreground">{new Date(m.firstTouchAt).toLocaleDateString()} · {m.source}</p></div>
            <Badge tone={m.responded ? 'success' : 'neutral'}>{humanize(m.status)}</Badge>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

// ---------------------------------------------------------------- credit note

export function CreditNoteCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const api = commerceApi(code);
  const key = ['credit-allocations', code, record.id];
  const listQ = useQuery({ queryKey: [...key, record.version], queryFn: () => api.creditAllocations(record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const [invoice, setInvoice] = useState<LookupValue | null>(null);
  const remaining = Number(record.values.remainingAmount) || 0;
  const [amount, setAmount] = useState(String(remaining));
  const [error, setError] = useState<string | null>(null);
  const cur = typeof record.values.currency === 'string' ? record.values.currency : undefined;
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: key });
    void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, 'credit_notes', record.id) });
    void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, 'invoices') });
  };
  const apply = useMutation({
    mutationFn: () => api.applyCredit(record.id, { invoiceId: invoice!.id, amount: Number(amount) }),
    onSuccess: () => { toast.success('Credit applied'); setAdding(false); setInvoice(null); refresh(); },
    onError: (e) => setError(firstError(e))
  });
  const remove = useMutation({ mutationFn: (id: string) => api.unapplyCredit(record.id, id), onSuccess: refresh, onError: (e) => toast.error(firstError(e)) });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const status = String(record.values.status ?? '');
  const canApply = (listQ.data?.canEdit ?? false) && remaining > 0 && (status === 'issued' || status === 'partially_applied');
  return (
    <Panel icon={<Receipt />} title="Applied to invoices" count={rows.length} actions={canApply ? <Button size="sm" variant="ghost" onClick={() => { setAmount(String(remaining)); setAdding(true); }}><Plus /> Apply</Button> : null}>
      <dl className="grid grid-cols-3 gap-2 border-b bg-muted/30 px-4 py-3 text-[13px]">
        <div><dt className="text-xs text-muted-foreground">Credit</dt><dd className="font-semibold tabular-nums">{formatMoney(Number(record.values.total) || 0, cur)}</dd></div>
        <div><dt className="text-xs text-muted-foreground">Applied</dt><dd className="font-semibold tabular-nums">{formatMoney(Number(record.values.appliedAmount) || 0, cur)}</dd></div>
        <div><dt className="text-xs text-muted-foreground">Remaining</dt><dd className="font-semibold tabular-nums">{formatMoney(remaining, cur)}</dd></div>
      </dl>
      {status === 'draft' || status === 'pending_approval' ? <Empty>{status === 'draft' ? 'Issue this credit note (set its status to Issued) to apply it.' : 'Waiting for approval before it can be applied.'}</Empty> : rows.length === 0 ? <Empty>Not applied to an invoice yet: the credit is kept on the customer's account.</Empty> : (
        <ul className="divide-y">
          {rows.map((a) => (
            <li key={a.id} className="group flex items-center gap-2 px-4 py-2 text-[13px]">
              <Link to={recordHref(scope, 'invoices', a.invoiceId)} className="min-w-0 flex-1 truncate font-medium text-primary hover:underline">{a.invoiceCode} · {a.invoice}</Link>
              <span className="font-medium tabular-nums">{formatMoney(a.amount, cur)}</span>
              {listQ.data?.canEdit ? <RemoveButton label={`Take the credit off ${a.invoiceCode}`} disabled={remove.isPending} onClick={() => remove.mutate(a.id)} /> : null}
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <FormDialog title="Apply credit to an invoice" description={`${formatMoney(remaining, cur)} of this credit note is left.`} error={error} onClose={() => setAdding(false)}
          onSubmit={() => { setError(null); apply.mutate(); }} submit="Apply" busy={apply.isPending} disabled={!invoice || !(Number(amount) > 0)}>
          <Field label="Invoice"><RecordPicker target="invoices" value={invoice} onPick={setInvoice} /></Field>
          <Field label="Amount"><Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} /></Field>
        </FormDialog>
      ) : null}
    </Panel>
  );
}

// ---------------------------------------------------------------- entitlement usage

export function EntitlementUsageCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const usageQ = useQuery({ queryKey: ['entitlement-usage', code, record.id, record.version], queryFn: () => commerceApi(code).entitlementUsage(record.id), enabled: Boolean(code) });
  if (!code) return null;
  const u = usageQ.data;
  const bar = (label: string, x: { included: number; used: number; remaining?: number }, unit: string) => (
    <div>
      <div className="flex justify-between text-[13px]"><span className="font-medium">{label}</span><span className="tabular-nums text-muted-foreground">{x.used} of {x.included || '∞'} {unit}</span></div>
      {x.included ? (
        <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted"><div className={cn('h-full rounded-full', x.used >= x.included ? 'bg-danger' : 'bg-primary')} style={{ width: `${Math.min(100, (x.used / x.included) * 100)}%` }} /></div>
      ) : null}
    </div>
  );
  return (
    <Panel icon={<ShieldCheck />} title="Usage">
      {usageQ.isLoading || !u ? <Loading /> : (
        <div className="space-y-3 px-4 py-3">
          {bar('Cases', u.cases, 'cases')}
          {bar('Service hours', u.hours, 'hours')}
          <p className="text-xs text-muted-foreground">When the allowance is used up: {u.overagePolicy === 'block' ? 'new cases are refused.' : 'cases are still taken and marked “over entitlement”.'}</p>
          {u.data.length ? (
            <ul className="divide-y border-t text-[13px]">
              {u.data.slice(0, 8).map((x, i) => (
                <li key={i} className="flex justify-between py-1.5">
                  <Link to={recordHref(scope, x.object, x.recordId)} className="text-primary hover:underline">{x.note || humanize(x.object)}</Link>
                  <span className="tabular-nums text-muted-foreground">{x.kind === 'case' ? '1 case' : `${x.quantity} h`}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      )}
    </Panel>
  );
}

// ---------------------------------------------------------------- field service actions

export function CaseWorkOrderButton({ record, disabled }: { record: RecordRow; disabled?: boolean }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const navigate = useNavigate();
  const make = useMutation({
    mutationFn: () => commerceApi(code).caseWorkOrder(record.id),
    onSuccess: (r) => { toast.success(`Work order ${r.code} created`); navigate(recordHref(scope, 'work_orders', r.id)); },
    onError: (e) => toast.error(firstError(e))
  });
  if (!code || !scope.can('work_orders', 'create')) return null;
  return (
    <Button size="sm" variant="outline" onClick={() => make.mutate()} loading={make.isPending} disabled={disabled}>
      <Wrench /> Work order
    </Button>
  );
}

/** Book (or re-book) an appointment from the free slots of the business's service resources. */
export function BookAppointmentButton({ record, object, disabled }: { record: RecordRow; object: 'work_orders' | 'appointments' | 'cases'; disabled?: boolean }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const navigate = useNavigate();
  const api = commerceApi(code);
  const [open, setOpen] = useState(false);
  const preset = object === 'appointments' && typeof record.values.itemId === 'string' ? { id: record.values.itemId, label: record.lookups.itemId?.label ?? 'Service' } : null;
  const [service, setService] = useState<LookupValue | null>(preset);
  const [date, setDate] = useState(() => new Date(Date.now() + 86400000).toISOString().slice(0, 10));
  const [slot, setSlot] = useState('');
  const [error, setError] = useState<string | null>(null);
  const slotsQ = useQuery({ queryKey: ['slots', code, service?.id ?? '', date], queryFn: () => api.slots({ serviceId: service?.id, date, days: 1 }), enabled: open });
  const book = useMutation({
    mutationFn: () => {
      const [resourceId, start] = slot.split('|');
      if (object === 'appointments') return api.reschedule(record.id, { start: start!, resourceId });
      const str = (k: string) => (typeof record.values[k] === 'string' ? (record.values[k] as string) : undefined);
      return api.book({ serviceId: service?.id, resourceId, start: start!, accountId: str('accountId'), contactId: str('contactId'), name: record.title,
        workOrderId: object === 'work_orders' ? record.id : undefined, caseId: object === 'cases' ? record.id : str('caseId') });
    },
    onSuccess: (r) => {
      toast.success(object === 'appointments' ? 'Rescheduled' : 'Appointment booked');
      setOpen(false);
      void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, object, record.id) });
      navigate(recordHref(scope, 'appointments', r.id));
    },
    onError: (e) => setError(firstError(e))
  });
  if (!code || !scope.can('appointments', 'create') || !scope.can('service_resources', 'read')) return null;
  const status = String(record.values.status ?? '');
  if (object === 'appointments' && !['scheduled', 'confirmed', 'no_show', ''].includes(status)) return null;
  if (object === 'work_orders' && ['completed', 'cancelled'].includes(status)) return null;
  const slots = slotsQ.data?.data ?? [];
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} disabled={disabled}>
        <CalendarClock /> {object === 'appointments' ? 'Reschedule' : 'Book appointment'}
      </Button>
      {open ? (
        <FormDialog title={object === 'appointments' ? 'Reschedule' : 'Book an appointment'} description="Only times when a resource with the right skills is working and free are offered." error={error}
          onClose={() => setOpen(false)} onSubmit={() => { setError(null); book.mutate(); }} submit={object === 'appointments' ? 'Reschedule' : 'Book'} busy={book.isPending} disabled={!slot}>
          {object !== 'appointments' ? <Field label="Service" hint="Sets how long it takes and which skills are needed."><RecordPicker target="catalog_items" value={service} onPick={(v) => { setService(v); setSlot(''); }} /></Field> : null}
          <Field label="Day"><Input type="date" value={date} onChange={(e) => { setDate(e.target.value); setSlot(''); }} /></Field>
          <Field label={`Free times${slotsQ.data ? ` (${slotsQ.data.durationMinutes} min)` : ''}`}>
            {slotsQ.isLoading ? <Skeleton className="h-9" /> : slots.length === 0 ? (
              <p className="text-[13px] text-muted-foreground">Nobody is free that day. Try another day, or add service resources (Service → Service resources).</p>
            ) : (
              <div className="grid max-h-48 grid-cols-3 gap-1.5 overflow-y-auto">
                {slots.map((s) => {
                  const v = `${s.resourceId}|${s.start}`;
                  return (
                    <button key={v} type="button" onClick={() => setSlot(v)}
                      className={cn('rounded-md border px-2 py-1.5 text-left text-xs', slot === v ? 'border-primary bg-primary-soft text-primary' : 'hover:bg-muted')}>
                      <span className="block font-medium tabular-nums">{new Date(s.start).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                      <span className="block truncate text-muted-foreground">{s.resource}</span>
                    </button>
                  );
                })}
              </div>
            )}
          </Field>
        </FormDialog>
      ) : null}
    </>
  );
}

