import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Banknote, Clock, Link2, Pause, Plus, RotateCcw, Timer, Undo2, X } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { enterpriseApi, type RecordRelationship, type RelationshipType, type SlaTimer } from '@crm/api/endpoints';
import type { LookupValue, ObjectKey, RecordRow } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { formatMoney } from '@crm/lib/money';
import { cn } from '@crm/lib/utils';
import { humanize, recordKeys } from './use-object-meta';
import { recordHref, useRecordScope } from './record-scope';

// The parts of a record page that come from the enterprise model (D-111…D-116):
// links to any other record, an invoice's payments, a payment's invoices, a case's SLA
// clocks and a contract's renewal. Everything here reads and writes through the API;
// the server decides what the viewer may see.

function codeOf(prefix: string): string {
  return prefix.startsWith('/w/') ? decodeURIComponent(prefix.slice(3)) : '';
}

function firstError(e: unknown): string {
  return isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : 'Something went wrong. Try again.';
}

const today = () => new Date().toISOString().slice(0, 10);

/** Objects a "related to" link may point at, when the relationship doesn't fix one. */
const LINKABLE: Array<[string, string]> = [
  ['accounts', 'Account'],
  ['contacts', 'Contact'],
  ['leads', 'Lead'],
  ['opportunities', 'Opportunity'],
  ['quotes', 'Quote'],
  ['sales_orders', 'Sales order'],
  ['invoices', 'Invoice'],
  ['contracts', 'Contract'],
  ['assets', 'Asset'],
  ['cases', 'Case'],
  ['catalog_items', 'Product or service'],
  ['purchase_orders', 'Purchase order'],
  ['appointments', 'Appointment'],
  ['events', 'Event'],
  ['tasks', 'Task']
];

// ---------------------------------------------------------------- relationships

export function RelationshipsCard({ object, record, canEdit }: { object: ObjectKey; record: RecordRow; canEdit: boolean }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const key = ['relationships', code, object, record.id];
  const listQ = useQuery({ queryKey: key, queryFn: () => enterpriseApi(code).relationships(object, record.id), enabled: Boolean(code) });
  const [adding, setAdding] = useState(false);
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: key });
    void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, object, record.id) });
  };
  const remove = useMutation({
    mutationFn: (id: string) => enterpriseApi(code).removeRelationship(id),
    onSuccess: () => {
      toast.success('Link removed');
      refresh();
    },
    onError: (e) => toast.error(firstError(e))
  });
  if (!code) return null;
  const rows = listQ.data?.data ?? [];
  const groups = new Map<string, RecordRelationship[]>();
  for (const r of rows) groups.set(r.label, [...(groups.get(r.label) ?? []), r]);
  const editable = canEdit && (listQ.data?.canEdit ?? false);

  return (
    <Card className="overflow-hidden">
      <CardHeader
        className="px-4 py-2.5"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <Link2 className="size-3.5 text-muted-foreground" aria-hidden /> Relationships
            <span className="rounded-full bg-muted px-1.5 text-[11px] font-medium tabular-nums text-muted-foreground">{rows.length}</span>
          </span>
        }
        actions={
          editable ? (
            <Button size="sm" variant="ghost" onClick={() => setAdding(true)}>
              <Plus /> Link
            </Button>
          ) : null
        }
      />
      {listQ.isLoading ? (
        <div className="space-y-2 px-4 py-3">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : rows.length === 0 ? (
        <p className="px-4 py-4 text-[13px] text-muted-foreground">
          Not linked to anything else yet.{editable ? ' Link a contact to a second account, a decision maker to a deal, a contract to the assets it covers.' : ''}
        </p>
      ) : (
        <div className="divide-y">
          {[...groups.entries()].map(([label, items]) => (
            <div key={label} className="px-4 py-2.5">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{label}</p>
              <ul className="mt-1 space-y-1">
                {items.map((r) => (
                  <li key={r.id} className="group flex items-start gap-2 text-[13px]">
                    <div className="min-w-0 flex-1">
                      <Link to={recordHref(scope, r.object, r.recordId)} className="font-medium text-primary hover:underline">
                        {r.title || r.code}
                      </Link>
                      <span className="ml-1.5 text-xs text-muted-foreground">{humanize(r.object)}</span>
                      {r.note ? <p className="truncate text-xs text-muted-foreground">{r.note}</p> : null}
                    </div>
                    {editable ? (
                      <button
                        type="button"
                        className="grid size-6 shrink-0 place-items-center rounded text-muted-foreground opacity-60 hover:bg-muted hover:text-danger group-hover:opacity-100"
                        aria-label={`Remove link to ${r.title}`}
                        disabled={remove.isPending}
                        onClick={() => remove.mutate(r.id)}
                      >
                        <X className="size-3.5" />
                      </button>
                    ) : null}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
      {adding ? <AddRelationshipDialog object={object} record={record} onClose={() => setAdding(false)} onDone={refresh} /> : null}
    </Card>
  );
}

function AddRelationshipDialog({ object, record, onClose, onDone }: { object: ObjectKey; record: RecordRow; onClose: () => void; onDone: () => void }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const typesQ = useQuery({ queryKey: ['relationship-types', code], queryFn: () => enterpriseApi(code).relationshipTypes(), staleTime: 5 * 60_000 });
  // A link is made from its starting record: only the types that can start here.
  const types = useMemo(() => (typesQ.data ?? []).filter((t: RelationshipType) => !t.sourceObject || t.sourceObject === object), [typesQ.data, object]);
  const [typeKey, setTypeKey] = useState('');
  const type = types.find((t) => t.key === typeKey) ?? types[0];
  const targets = LINKABLE.filter(([o]) => scope.can(o, 'read'));
  const [targetObject, setTargetObject] = useState('');
  const target = type?.targetObject || targetObject || targets[0]?.[0] || '';
  const [q, setQ] = useState('');
  const [picked, setPicked] = useState<LookupValue | null>(null);
  const [note, setNote] = useState('');
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setPicked(null), [target]);
  const searchQ = useQuery({
    queryKey: ['relationship-lookup', code, target, q],
    queryFn: () => scope.api.lookup(target, q),
    enabled: Boolean(target) && !picked
  });
  const save = useMutation({
    mutationFn: () => enterpriseApi(code).addRelationship(object, record.id, { type: type!.key, targetObject: target, targetId: picked!.id, note: note.trim() || undefined }),
    onSuccess: () => {
      toast.success('Linked');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  const results = (searchQ.data ?? []).filter((r) => !(target === object && r.id === record.id));

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">Link {record.title || 'this record'}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">Connect it to another record of this business.</DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            if (type && picked) save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label="Relationship">
            <Select value={type?.key ?? ''} onChange={(e) => setTypeKey(e.target.value)}>
              {types.map((t) => (
                <option key={t.key} value={t.key}>
                  {t.label}
                  {t.targetObject ? ` → ${humanize(t.targetObject)}` : ''}
                </option>
              ))}
            </Select>
          </Field>
          {type && !type.targetObject ? (
            <Field label="Kind of record">
              <Select value={target} onChange={(e) => setTargetObject(e.target.value)}>
                {targets.map(([o, label]) => (
                  <option key={o} value={o}>
                    {label}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          <Field label="Record">
            {picked ? (
              <div className="flex items-center justify-between rounded-md border bg-muted/40 px-3 py-2 text-[13px]">
                <span className="truncate font-medium">{picked.label}</span>
                <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => setPicked(null)}>
                  Change
                </button>
              </div>
            ) : (
              <>
                <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Search ${humanize(target).toLowerCase()}…`} autoFocus />
                <ul className="mt-1.5 max-h-44 overflow-y-auto rounded-md border">
                  {searchQ.isLoading ? (
                    <li className="px-3 py-2 text-[13px] text-muted-foreground">Searching…</li>
                  ) : results.length === 0 ? (
                    <li className="px-3 py-2 text-[13px] text-muted-foreground">Nothing found.</li>
                  ) : (
                    results.map((r) => (
                      <li key={r.id}>
                        <button type="button" className="block w-full truncate px-3 py-2 text-left text-[13px] hover:bg-muted" onClick={() => setPicked(r)}>
                          {r.label}
                        </button>
                      </li>
                    ))
                  )}
                </ul>
              </>
            )}
          </Field>
          <Field label="Note (optional)">
            <Input value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} placeholder="e.g. Signs off the budget" />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!type || !picked}>
              Link
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- invoice payments

const paymentTone: Record<string, 'success' | 'warning' | 'danger' | 'neutral' | 'primary'> = {
  paid: 'success',
  partially_refunded: 'warning',
  refunded: 'neutral',
  pending: 'warning',
  authorized: 'primary',
  failed: 'danger',
  cancelled: 'neutral'
};

export function InvoicePaymentsCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const key = ['invoice-ledger', code, record.id];
  // Refetch when the invoice itself changes (its total decides the balance).
  const ledgerQ = useQuery({ queryKey: [...key, record.version], queryFn: () => enterpriseApi(code).invoiceLedger(record.id), enabled: Boolean(code) });
  const [open, setOpen] = useState(false);
  if (!code) return null;
  const l = ledgerQ.data;
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: key });
    void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, 'invoices', record.id) });
    void qc.invalidateQueries({ queryKey: recordKeys.lists(scope.prefix, 'payments') });
  };
  return (
    <Card className="overflow-hidden">
      <CardHeader
        className="px-4 py-2.5"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <Banknote className="size-3.5 text-muted-foreground" aria-hidden /> Payments
          </span>
        }
        actions={
          l?.canRecordPayment && l.balance > 0 ? (
            <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
              <Plus /> Record payment
            </Button>
          ) : null
        }
      />
      {ledgerQ.isLoading || !l ? (
        <div className="space-y-2 px-4 py-3">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ) : (
        <>
          <dl className="grid grid-cols-3 gap-2 border-b bg-muted/30 px-4 py-3 text-[13px]">
            <div>
              <dt className="text-xs text-muted-foreground">Invoice total</dt>
              <dd className="font-semibold tabular-nums">{formatMoney(l.total, l.currency)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Paid</dt>
              <dd className="font-semibold tabular-nums text-success">{formatMoney(l.paid, l.currency)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Balance due</dt>
              <dd className={cn('font-semibold tabular-nums', l.balance > 0 ? 'text-danger' : 'text-foreground')}>{formatMoney(l.balance, l.currency)}</dd>
            </div>
          </dl>
          {!l.canSeePayments ? (
            <p className="px-4 py-3 text-[13px] text-muted-foreground">You don't have access to the payment records. Ask an administrator for Finance access.</p>
          ) : l.payments.length === 0 ? (
            <p className="px-4 py-3 text-[13px] text-muted-foreground">No payments recorded against this invoice yet.</p>
          ) : (
            <ul className="divide-y">
              {l.payments.map((p) => (
                <li key={p.id} className="flex items-center gap-3 px-4 py-2 text-[13px]">
                  <div className="min-w-0 flex-1">
                    <Link to={recordHref(scope, 'payments', p.id)} className="font-medium text-primary hover:underline">
                      {p.code}
                    </Link>
                    <span className="ml-1.5 text-xs text-muted-foreground">
                      {p.date}
                      {p.method ? ` · ${humanize(p.method)}` : ''}
                    </span>
                    {p.refunded > 0 ? <p className="text-xs text-muted-foreground">Refunded {formatMoney(p.refunded, l.currency)}</p> : null}
                  </div>
                  <Badge tone={paymentTone[p.status] ?? 'neutral'}>{humanize(p.status)}</Badge>
                  <span className={cn('w-24 text-right font-medium tabular-nums', !p.counts && 'text-muted-foreground line-through')}>{formatMoney(p.applied, l.currency)}</span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {open && l ? <RecordPaymentDialog invoice={record} balance={l.balance} currency={l.currency} onClose={() => setOpen(false)} onDone={refresh} /> : null}
    </Card>
  );
}

function RecordPaymentDialog({ invoice, balance, currency, onClose, onDone }: { invoice: RecordRow; balance: number; currency: string; onClose: () => void; onDone: () => void }) {
  const scope = useRecordScope();
  const [amount, setAmount] = useState(String(balance));
  const [date, setDate] = useState(today());
  const [method, setMethod] = useState('bank_transfer');
  const [reference, setReference] = useState('');
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () =>
      scope.api.create('payments', {
        name: `Payment for ${invoice.code || invoice.title}`,
        amount: Number(amount),
        paymentDate: date,
        paymentMethod: method,
        reference: reference.trim() || undefined,
        invoiceId: invoice.id,
        status: 'paid'
      }),
    onSuccess: () => {
      toast.success('Payment recorded');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  const n = Number(amount);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">Record a payment</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            {invoice.title} · {formatMoney(balance, currency)} still due
          </DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            if (!(n > 0)) return setError('Enter the amount received.');
            save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Amount received" hint={n > balance ? 'More than is due: the rest stays on the payment, to apply to another invoice.' : undefined}>
              <Input type="number" inputMode="decimal" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />
            </Field>
            <Field label="Received on">
              <Input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
            </Field>
          </div>
          <Field label="Paid by">
            <Select value={method} onChange={(e) => setMethod(e.target.value)}>
              <option value="bank_transfer">Bank transfer</option>
              <option value="upi">UPI</option>
              <option value="cash">Cash</option>
              <option value="card">Card</option>
              <option value="cheque">Cheque</option>
              <option value="other">Other</option>
            </Select>
          </Field>
          <Field label="Reference (optional)">
            <Input value={reference} onChange={(e) => setReference(e.target.value)} placeholder="UTR, cheque or transaction number" />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              Record payment
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- payment → invoices

export function PaymentAllocationsCard({ record, canEdit, currency }: { record: RecordRow; canEdit: boolean; currency?: string }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const qc = useQueryClient();
  const key = ['payment-allocations', code, record.id];
  const listQ = useQuery({ queryKey: [...key, record.version], queryFn: () => enterpriseApi(code).allocations(record.id), enabled: Boolean(code) });
  const [applying, setApplying] = useState(false);
  const [refunding, setRefunding] = useState(false);
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: key });
    void qc.invalidateQueries({ queryKey: recordKeys.detail(scope.prefix, 'payments', record.id) });
    void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, 'invoices') });
  };
  const remove = useMutation({
    mutationFn: (id: string) => enterpriseApi(code).unallocate(record.id, id),
    onSuccess: () => {
      toast.success('Taken off the invoice');
      refresh();
    },
    onError: (e) => toast.error(firstError(e))
  });
  if (!code) return null;
  const cur = (typeof record.values.currency === 'string' && record.values.currency) || currency;
  const amount = Number(record.values.amount) || 0;
  const refunded = Number(record.values.refundedAmount) || 0;
  const unapplied = Number(record.values.unappliedAmount) || 0;
  const status = String(record.values.status ?? '');
  const received = status === 'paid' || status === 'partially_refunded';
  const rows = listQ.data ?? [];
  return (
    <Card className="overflow-hidden">
      <CardHeader
        className="px-4 py-2.5"
        title={<span className="text-[13px]">Applied to invoices</span>}
        actions={
          canEdit ? (
            <span className="flex gap-1">
              {unapplied > 0 ? (
                <Button size="sm" variant="ghost" onClick={() => setApplying(true)}>
                  <Plus /> Apply
                </Button>
              ) : null}
              {received && amount - refunded > 0 ? (
                <Button size="sm" variant="ghost" onClick={() => setRefunding(true)}>
                  <Undo2 /> Refund
                </Button>
              ) : null}
            </span>
          ) : null
        }
      />
      <dl className="grid grid-cols-3 gap-2 border-b bg-muted/30 px-4 py-3 text-[13px]">
        <div>
          <dt className="text-xs text-muted-foreground">Received</dt>
          <dd className="font-semibold tabular-nums">{formatMoney(amount, cur)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Refunded</dt>
          <dd className="font-semibold tabular-nums">{formatMoney(refunded, cur)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Not applied</dt>
          <dd className={cn('font-semibold tabular-nums', unapplied > 0 && 'text-warning')}>{formatMoney(unapplied, cur)}</dd>
        </div>
      </dl>
      {listQ.isLoading ? (
        <div className="px-4 py-3">
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : rows.length === 0 ? (
        <p className="px-4 py-3 text-[13px] text-muted-foreground">This payment isn't applied to an invoice yet.</p>
      ) : (
        <ul className="divide-y">
          {rows.map((a) => (
            <li key={a.id} className="group flex items-center gap-3 px-4 py-2 text-[13px]">
              <Link to={recordHref(scope, 'invoices', a.invoiceId)} className="min-w-0 flex-1 truncate font-medium text-primary hover:underline">
                {a.invoiceCode} · {a.invoice}
              </Link>
              <span className="font-medium tabular-nums">{formatMoney(a.amount, cur)}</span>
              {canEdit ? (
                <button
                  type="button"
                  className="grid size-6 place-items-center rounded text-muted-foreground opacity-60 hover:bg-muted hover:text-danger group-hover:opacity-100"
                  aria-label={`Take this payment off ${a.invoiceCode}`}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(a.id)}
                >
                  <X className="size-3.5" />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {applying ? <ApplyPaymentDialog payment={record} unapplied={unapplied} currency={cur} onClose={() => setApplying(false)} onDone={refresh} /> : null}
      {refunding ? <RefundDialog payment={record} max={amount - refunded} currency={cur} onClose={() => setRefunding(false)} onDone={refresh} /> : null}
    </Card>
  );
}

function ApplyPaymentDialog({ payment, unapplied, currency, onClose, onDone }: { payment: RecordRow; unapplied: number; currency?: string; onClose: () => void; onDone: () => void }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const [q, setQ] = useState('');
  const [picked, setPicked] = useState<LookupValue | null>(null);
  const [amount, setAmount] = useState(String(unapplied));
  const [error, setError] = useState<string | null>(null);
  const searchQ = useQuery({ queryKey: ['invoice-lookup', code, q], queryFn: () => scope.api.lookup('invoices', q), enabled: !picked });
  const save = useMutation({
    mutationFn: () => enterpriseApi(code).allocate(payment.id, { invoiceId: picked!.id, amount: Number(amount) }),
    onSuccess: () => {
      toast.success('Applied to the invoice');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">Apply to an invoice</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{formatMoney(unapplied, currency)} of this payment is not applied yet.</DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            if (picked) save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label="Invoice">
            {picked ? (
              <div className="flex items-center justify-between rounded-md border bg-muted/40 px-3 py-2 text-[13px]">
                <span className="truncate font-medium">{picked.label}</span>
                <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => setPicked(null)}>
                  Change
                </button>
              </div>
            ) : (
              <>
                <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search invoices…" autoFocus />
                <ul className="mt-1.5 max-h-44 overflow-y-auto rounded-md border">
                  {(searchQ.data ?? []).length === 0 ? (
                    <li className="px-3 py-2 text-[13px] text-muted-foreground">{searchQ.isLoading ? 'Searching…' : 'Nothing found.'}</li>
                  ) : (
                    (searchQ.data ?? []).map((r) => (
                      <li key={r.id}>
                        <button type="button" className="block w-full truncate px-3 py-2 text-left text-[13px] hover:bg-muted" onClick={() => setPicked(r)}>
                          {r.label}
                        </button>
                      </li>
                    ))
                  )}
                </ul>
              </>
            )}
          </Field>
          <Field label="Amount to apply">
            <Input type="number" inputMode="decimal" min="0" step="0.01" max={unapplied} value={amount} onChange={(e) => setAmount(e.target.value)} />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!picked || !(Number(amount) > 0)}>
              Apply
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RefundDialog({ payment, max, currency, onClose, onDone }: { payment: RecordRow; max: number; currency?: string; onClose: () => void; onDone: () => void }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const [amount, setAmount] = useState(String(max));
  const [reason, setReason] = useState('');
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: () => enterpriseApi(code).refund(payment.id, { amount: Number(amount), reason: reason.trim() || undefined }),
    onSuccess: () => {
      toast.success('Refund recorded');
      onDone();
      onClose();
    },
    onError: (e) => setError(firstError(e))
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="text-base font-semibold">Refund this payment</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            Up to {formatMoney(max, currency)} can be refunded. The invoices it paid get their balance back. This records the refund; it does not move money.
          </DialogDescription>
        </div>
        <form
          className="space-y-3 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            save.mutate();
          }}
        >
          {error ? <Alert tone="danger">{error}</Alert> : null}
          <Field label="Amount to refund">
            <Input type="number" inputMode="decimal" min="0" step="0.01" max={max} value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus />
          </Field>
          <Field label="Reason (optional)">
            <Textarea value={reason} onChange={(e) => setReason(e.target.value)} rows={2} maxLength={500} />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" loading={save.isPending} disabled={!(Number(amount) > 0)}>
              Refund
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- case SLA

function minutesText(m: number): string {
  const n = Math.abs(m);
  if (n < 60) return `${n} min`;
  if (n < 48 * 60) return `${Math.floor(n / 60)} h ${n % 60 ? `${n % 60} min` : ''}`.trim();
  return `${Math.round(n / 60 / 24)} days`;
}

const slaTone: Record<SlaTimer['state'], 'success' | 'warning' | 'danger' | 'neutral' | 'primary'> = {
  running: 'primary',
  paused: 'neutral',
  met: 'success',
  breached: 'danger',
  missed: 'danger'
};
const slaLabel: Record<SlaTimer['state'], string> = { running: 'Running', paused: 'Paused', met: 'Met', breached: 'Breached', missed: 'Met late' };

export function CaseSlaCard({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const slaQ = useQuery({
    queryKey: ['case-sla', code, record.id, record.version],
    queryFn: () => enterpriseApi(code).caseSla(record.id),
    enabled: Boolean(code),
    refetchInterval: 60_000
  });
  if (!code) return null;
  const sla = slaQ.data;
  return (
    <Card className="overflow-hidden">
      <CardHeader
        className="px-4 py-2.5"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <Timer className="size-3.5 text-muted-foreground" aria-hidden /> Service level
          </span>
        }
      />
      {slaQ.isLoading || !sla ? (
        <div className="px-4 py-3">
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : sla.timers.length === 0 ? (
        <p className="px-4 py-3 text-[13px] text-muted-foreground">
          No SLA applies to this case. Add an SLA policy (Service → SLA policies) for its priority, or mark one as the default.
        </p>
      ) : (
        <div className="space-y-3 px-4 py-3">
          {sla.policy ? (
            <p className="text-xs text-muted-foreground">
              Policy:{' '}
              <Link to={recordHref(scope, 'sla_policies', sla.policy.id)} className="font-medium text-primary hover:underline">
                {sla.policy.label}
              </Link>
            </p>
          ) : null}
          {sla.timers.map((t) => {
            const pct = Math.min(100, Math.round((t.elapsedMinutes / Math.max(1, t.targetMinutes)) * 100));
            const late = t.state === 'breached' || t.state === 'missed';
            return (
              <div key={t.milestone}>
                <div className="flex items-center justify-between gap-2 text-[13px]">
                  <span className="font-medium">{t.milestone === 'first_response' ? 'First response' : 'Resolution'}</span>
                  <Badge tone={slaTone[t.state]}>
                    {t.state === 'paused' ? <Pause className="size-3" /> : null}
                    {slaLabel[t.state]}
                  </Badge>
                </div>
                <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                  <div className={cn('h-full rounded-full', late ? 'bg-danger' : pct >= 80 ? 'bg-warning' : 'bg-primary')} style={{ width: `${pct}%` }} />
                </div>
                <p className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                  <Clock className="size-3" aria-hidden />
                  {t.state === 'running' || t.state === 'paused'
                    ? `${minutesText(t.remainingMinutes)} left of ${minutesText(t.targetMinutes)}`
                    : t.state === 'breached'
                      ? `Overdue by ${minutesText(t.remainingMinutes)}`
                      : `Target ${minutesText(t.targetMinutes)}, took ${minutesText(t.elapsedMinutes)}`}
                  {t.businessHours ? ' · working hours' : ''}
                  {t.state === 'running' || t.state === 'breached' ? ` · due ${new Date(t.dueAt).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}` : ''}
                </p>
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- contract renewal

export function RenewContractButton({ record, disabled }: { record: RecordRow; disabled?: boolean }) {
  const scope = useRecordScope();
  const code = codeOf(scope.prefix);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(String(record.values.contractValue ?? ''));
  const [start, setStart] = useState('');
  const [end, setEnd] = useState('');
  const [error, setError] = useState<string | null>(null);
  const renew = useMutation({
    mutationFn: () => enterpriseApi(code).renewContract(record.id, { startDate: start || undefined, endDate: end || undefined, contractValue: value === '' ? undefined : Number(value) }),
    onSuccess: (r) => {
      toast.success('Contract renewed');
      void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, 'contracts') });
      setOpen(false);
      navigate(recordHref(scope, 'contracts', r.id));
    },
    onError: (e) => setError(firstError(e))
  });
  const status = String(record.values.status ?? '');
  if (!code || !scope.can('contracts', 'create') || ['renewed', 'cancelled', 'draft'].includes(status)) return null;
  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)} disabled={disabled}>
        <RotateCcw /> Renew
      </Button>
      {open ? (
        <Dialog open onOpenChange={(o) => !o && setOpen(false)}>
          <DialogContent className="max-w-md p-0">
            <div className="border-b px-5 py-4">
              <DialogTitle className="text-base font-semibold">Renew {record.title}</DialogTitle>
              <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
                Creates the next term as a new contract with the same customer and cover, and marks this one as renewed.
              </DialogDescription>
            </div>
            <form
              className="space-y-3 px-5 py-4"
              onSubmit={(e) => {
                e.preventDefault();
                setError(null);
                renew.mutate();
              }}
            >
              {error ? <Alert tone="danger">{error}</Alert> : null}
              <div className="grid grid-cols-2 gap-3">
                <Field label="Starts" hint="Blank = the day after this one ends">
                  <Input type="date" value={start} onChange={(e) => setStart(e.target.value)} />
                </Field>
                <Field label="Ends" hint="Blank = same length">
                  <Input type="date" value={end} onChange={(e) => setEnd(e.target.value)} />
                </Field>
              </div>
              <Field label="Contract value">
                <Input type="number" inputMode="decimal" min="0" step="0.01" value={value} onChange={(e) => setValue(e.target.value)} />
              </Field>
              <div className="flex justify-end gap-2 pt-1">
                <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit" loading={renew.isPending}>
                  Renew contract
                </Button>
              </div>
            </form>
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}
