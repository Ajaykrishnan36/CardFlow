import { useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { CalendarPlus, Check, Phone, SkipForward } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { UpcomingRow } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { NewRecordDialog } from '@crm/features/records/new-record-dialog';
import { recordHref, useRecordScope } from '@crm/features/records/record-scope';
import { useObjectMeta } from '@crm/features/records/use-object-meta';
import { errorText, toolKeys, useTools } from '@crm/features/tools/tool-utils';

const OUTCOMES = [
  { value: 'connected', label: 'Connected' },
  { value: 'no_answer', label: 'No answer' },
  { value: 'busy', label: 'Busy' },
  { value: 'switched_off', label: 'Switched off' },
  { value: 'callback', label: 'Asked to call back' },
  { value: 'interested', label: 'Interested — will decide' },
  { value: 'booked', label: 'Booked' },
  { value: 'declined', label: 'Declined' },
  { value: 'done_elsewhere', label: 'Done elsewhere' },
  { value: 'wrong_number', label: 'Wrong number' }
];
const NEEDS_DATE = new Set(['connected', 'callback', 'interested', 'booked']);
const KIND: Record<string, string> = {
  service: 'Service', renewal: 'Renewal', follow_up: 'Follow-up', visit: 'Visit', payment: 'Payment due', review: 'Review', delivery: 'Delivery', recall: 'Recall', other: 'Other'
};
const STATUS: Record<string, { label: string; tone: 'primary' | 'warning' | 'success' | 'neutral' }> = {
  open: { label: 'To contact', tone: 'primary' }, contacted: { label: 'Follow up', tone: 'warning' }, booked: { label: 'Booked', tone: 'primary' },
  done: { label: 'Done', tone: 'success' }, skipped: { label: 'Skipped', tone: 'neutral' }
};
const outcomeLabel = (v?: string) => OUTCOMES.find((o) => o.value === v)?.label ?? '';
const plusDays = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return d.toISOString().slice(0, 10);
};
const prettyDate = (iso: string) => {
  const d = new Date(`${iso}T00:00:00`);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
};
const daysText = (n: number) => (n === 0 ? 'Today' : n === 1 ? 'Tomorrow' : n > 0 ? `In ${n} days` : `${-n} ${n === -1 ? 'day' : 'days'} overdue`);

/** /crm/w/:ws/upcoming — what is due, by day: count → list → call → outcome → book → done → next cycle. */
export function UpcomingPage() {
  useDocumentTitle('Upcoming');
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const [win, setWin] = useState('next10');
  const [who, setWho] = useState<'all' | 'mine' | 'team'>('all');
  const [page, setPage] = useState(1);
  const [calling, setCalling] = useState<UpcomingRow | null>(null);
  const [finishing, setFinishing] = useState<{ row: UpcomingRow; mode: 'done' | 'skip' } | null>(null);
  const [booking, setBooking] = useState<UpcomingRow | null>(null);
  const sumKey = toolKeys.one(code, 'upcoming', 'summary', who);
  const listKey = toolKeys.one(code, 'upcoming', 'list', win, who, String(page));
  const summary = useQuery({ queryKey: sumKey, queryFn: () => api.upcomingSummary({ scope: who }) });
  const list = useQuery({ queryKey: listKey, queryFn: () => api.upcomingList({ window: win, scope: who, page, pageSize: 25 }) });
  const apptMeta = useObjectMeta('appointments');
  const refresh = () => void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'upcoming') });
  const claim = useMutation({
    mutationFn: (r: UpcomingRow) => api.claimDue(r.id).then(() => r),
    onSuccess: (r) => {
      setCalling(r);
      if (r.phone) window.location.href = `tel:${r.phone.replace(/[^\d+]/g, '')}`;
    },
    onError: (e) => toast.error(errorText(e, "Couldn't start the call."))
  });
  const tiles = summary.data?.tiles ?? [];
  const main = tiles.filter((t) => ['overdue', 'today', 'next3', 'next7', 'next10', 'next30'].includes(t.key));
  const work = tiles.filter((t) => ['uncontacted', 'callback', 'booked', 'done'].includes(t.key));
  const current = tiles.find((t) => t.key === win);
  const total = list.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / 25));

  return (
    <PageContainer wide>
      <PageHeader
        title="Upcoming"
        description="Everything due for your customers — services, renewals, follow-ups, payments. Call, note the outcome, book it, mark it done; the next one is planned by itself."
        actions={
          <Select
            className="w-40"
            aria-label="Whose items"
            value={who}
            onChange={(e) => {
              setWho(e.target.value as 'all' | 'mine' | 'team');
              setPage(1);
            }}
            options={[
              { value: 'all', label: 'Everything I can see' },
              { value: 'mine', label: 'Mine' },
              { value: 'team', label: 'My team' }
            ]}
          />
        }
      />
      {summary.isError ? (
        <Card>
          <ErrorState title="Couldn't load the counts" onRetry={() => void summary.refetch()} />
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {(summary.isPending ? Array.from({ length: 6 }).map(() => null) : main).map((t, i) =>
              t ? (
                <button
                  key={t.key}
                  type="button"
                  onClick={() => {
                    setWin(t.key);
                    setPage(1);
                  }}
                  className={cn(
                    'rounded-lg border bg-card p-3 text-left shadow-card transition-colors hover:border-primary',
                    win === t.key && 'border-primary ring-2 ring-primary/20',
                    t.key === 'overdue' && t.items > 0 && 'border-danger/40'
                  )}
                >
                  <p className="text-xs font-medium text-muted-foreground">{t.label}</p>
                  <p className={cn('mt-1 text-2xl font-semibold tabular-nums', t.key === 'overdue' && t.items > 0 && 'text-danger')}>{t.items}</p>
                  <p className="text-xs text-muted-foreground">
                    {t.customers} {t.customers === 1 ? 'customer' : 'customers'}
                    {t.range ? ` · ${t.range}` : ''}
                  </p>
                </button>
              ) : (
                <Skeleton key={i} className="h-24" />
              )
            )}
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            {work.map((t) => (
              <Button
                key={t.key}
                size="sm"
                variant={win === t.key ? 'primary' : 'outline'}
                onClick={() => {
                  setWin(t.key);
                  setPage(1);
                }}
              >
                {t.label} <span className="ml-1 tabular-nums opacity-80">{t.items}</span>
              </Button>
            ))}
          </div>
          {summary.data ? (
            <p className="mt-2 text-xs text-muted-foreground">
              {summary.data.note} Times in {summary.data.timezone}.
              {summary.data.missingDueDate > 0 ? ` ${summary.data.missingDueDate} active schedule(s) have no next date — open Recurring schedules to fix them.` : ''}
            </p>
          ) : null}
        </>
      )}

      <Card className="mt-4 overflow-hidden">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <p className="text-sm font-semibold">
            {current?.label ?? 'Due'} <span className="font-normal text-muted-foreground">· {total} {total === 1 ? 'item' : 'items'}</span>
          </p>
          <Button size="sm" variant="outline" asChild>
            <Link to={`${scope.routeBase}/care_schedules`}>Recurring schedules</Link>
          </Button>
        </div>
        {list.isPending ? (
          <div className="space-y-2 p-4">
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </div>
        ) : list.isError ? (
          <ErrorState title="Couldn't load the list" onRetry={() => void list.refetch()} />
        ) : list.data.data.length === 0 ? (
          <p className="px-4 py-10 text-center text-sm text-muted-foreground">Nothing here. Pick another tile, or add a recurring schedule for a customer.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[860px] text-[13px]">
              <thead>
                <tr className="border-b bg-muted/40 text-left text-xs font-medium text-muted-foreground">
                  <th className="px-4 py-2">Customer</th>
                  <th className="px-3 py-2">What is due</th>
                  <th className="px-3 py-2">Due</th>
                  <th className="px-3 py-2">Last call</th>
                  <th className="px-3 py-2">Owner</th>
                  <th className="px-3 py-2">Status</th>
                  <th className="px-4 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {list.data.data.map((r) => {
                  const st = STATUS[r.status] ?? { label: r.status, tone: 'neutral' as const };
                  const open = r.status !== 'done' && r.status !== 'skipped';
                  return (
                    <tr key={r.id} className="align-top">
                      <td className="px-4 py-2.5">
                        {r.contactId ? (
                          <Link to={recordHref(scope, 'contacts', r.contactId)} className="font-medium text-primary hover:underline">
                            {r.customer || '—'}
                          </Link>
                        ) : (
                          <span className="font-medium">{r.customer || '—'}</span>
                        )}
                        {r.phone ? (
                          <a href={`tel:${r.phone.replace(/[^\d+]/g, '')}`} className="block text-xs text-muted-foreground hover:text-primary">
                            {r.phone}
                          </a>
                        ) : (
                          <span className="block text-xs text-danger">No phone number</span>
                        )}
                      </td>
                      <td className="px-3 py-2.5">
                        <Link to={recordHref(scope, 'due_items', r.id)} className="hover:underline">
                          {KIND[r.kind] ?? r.kind}
                        </Link>
                        {r.asset ? <span className="block text-xs text-muted-foreground">{r.asset}</span> : null}
                      </td>
                      <td className="px-3 py-2.5">
                        <span className="tabular-nums">{prettyDate(r.dueDate)}</span>
                        <span className={cn('block text-xs', r.daysLeft < 0 ? 'text-danger' : 'text-muted-foreground')}>{daysText(r.daysLeft)}</span>
                      </td>
                      <td className="px-3 py-2.5">
                        {r.lastOutcome ? outcomeLabel(r.lastOutcome) : <span className="text-muted-foreground">Not called</span>}
                        {r.nextActionAt ? <span className="block text-xs text-muted-foreground">Next: {prettyDate(r.nextActionAt)}</span> : null}
                        {r.claimedBy ? <span className="block text-xs text-warning">{r.claimedBy} is calling</span> : null}
                      </td>
                      <td className="px-3 py-2.5 text-muted-foreground">{r.owner || '—'}</td>
                      <td className="px-3 py-2.5">
                        <Badge tone={st.tone}>{st.label}</Badge>
                      </td>
                      <td className="px-4 py-2.5">
                        {open ? (
                          <span className="flex justify-end gap-1.5">
                            <Button size="sm" onClick={() => claim.mutate(r)} loading={claim.isPending && claim.variables?.id === r.id} aria-label={`Call ${r.customer}`}>
                              <Phone /> Call
                            </Button>
                            <Button size="sm" variant="outline" onClick={() => setBooking(r)} aria-label={`Book for ${r.customer}`}>
                              <CalendarPlus /> Book
                            </Button>
                            <Button size="sm" variant="outline" onClick={() => setFinishing({ row: r, mode: 'done' })} aria-label={`Mark done for ${r.customer}`}>
                              <Check /> Done
                            </Button>
                            <Button size="icon-sm" variant="subtle" onClick={() => setFinishing({ row: r, mode: 'skip' })} aria-label={`Skip for ${r.customer}`} title="Skip">
                              <SkipForward />
                            </Button>
                          </span>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {pages > 1 ? (
          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
            <span>
              Page {page} of {pages}
            </span>
            <span className="flex gap-1.5">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((p) => p - 1)}>
                Previous
              </Button>
              <Button size="sm" variant="outline" disabled={page >= pages} onClick={() => setPage((p) => p + 1)}>
                Next
              </Button>
            </span>
          </div>
        ) : null}
      </Card>

      {calling ? <CallDialog row={calling} onClose={() => setCalling(null)} onSaved={refresh} /> : null}
      {finishing ? <FinishDialog row={finishing.row} mode={finishing.mode} onClose={() => setFinishing(null)} onSaved={refresh} /> : null}
      {booking && apptMeta.data ? (
        <NewRecordDialog
          object="appointments"
          meta={apptMeta.data}
          open
          onOpenChange={(o) => {
            if (!o) {
              setBooking(null);
              refresh();
            }
          }}
          note={`For ${booking.customer} — ${KIND[booking.kind] ?? booking.kind}. After booking, log the call as Booked.`}
          defaults={{ name: `${KIND[booking.kind] ?? 'Visit'} — ${booking.customer}`, ...(booking.contactId ? { contactId: booking.contactId } : {}), ...(booking.accountId ? { accountId: booking.accountId } : {}) }}
        />
      ) : null}
    </PageContainer>
  );
}

function CallDialog({ row, onClose, onSaved }: { row: UpcomingRow; onClose: () => void; onSaved: () => void }) {
  const scope = useRecordScope();
  const [outcome, setOutcome] = useState('');
  const [notes, setNotes] = useState('');
  const [next, setNext] = useState('');
  const save = useMutation({
    mutationFn: () =>
      scope.api.create('call_logs', { dueItemId: row.id, contactId: row.contactId, channel: 'call', outcome, notes: notes.trim() || undefined, nextActionAt: next || undefined }),
    onSuccess: () => {
      toast.success('Call saved');
      onSaved();
      onClose();
    }
  });
  const fe = isApiError(save.error) ? save.error.fieldErrors : {};
  const needsDate = NEEDS_DATE.has(outcome);
  const presets = useMemo(() => [1, 3, 7, 30].map((n) => ({ n, iso: plusDays(n) })), []);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (outcome) save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">How did the call go?</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">
          {row.customer} · {row.phone || 'no phone number'} · {KIND[row.kind] ?? row.kind} due {prettyDate(row.dueDate)}
        </DialogDescription>
        <form onSubmit={submit} noValidate className="mt-4 space-y-4">
          <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Outcome">
            {OUTCOMES.map((o) => (
              <button
                key={o.value}
                type="button"
                role="radio"
                aria-checked={outcome === o.value}
                onClick={() => setOutcome(o.value)}
                className={cn('rounded-md border px-3 py-2 text-left text-[13px] hover:bg-muted', outcome === o.value && 'border-primary bg-primary-soft text-primary')}
              >
                {o.label}
              </button>
            ))}
          </div>
          {outcome === 'wrong_number' ? <Alert tone="warning">Fix the customer's number on their record — this item stays on the list until then.</Alert> : null}
          {outcome === 'declined' || outcome === 'done_elsewhere' ? <Alert tone="info">This occurrence will be skipped and the next one planned.</Alert> : null}
          <Field label={needsDate ? 'Follow up on (required)' : 'Follow up on'} error={fe.nextActionAt}>
            <div className="flex flex-wrap gap-1.5">
              <Input type="date" value={next} onChange={(e) => setNext(e.target.value)} className="w-40" />
              {presets.map((p) => (
                <Button key={p.n} type="button" size="sm" variant="outline" onClick={() => setNext(p.iso)}>
                  +{p.n}d
                </Button>
              ))}
            </div>
          </Field>
          <Field label="What was said">
            <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} maxLength={1000} />
          </Field>
          {save.isError && Object.keys(fe).length === 0 ? <Alert tone="danger">{errorText(save.error, 'Something went wrong.')}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!outcome || (needsDate && !next)}>
              Save call
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function FinishDialog({ row, mode, onClose, onSaved }: { row: UpcomingRow; mode: 'done' | 'skip'; onClose: () => void; onSaved: () => void }) {
  const scope = useRecordScope();
  const [date, setDate] = useState(plusDays(0));
  const [reason, setReason] = useState('');
  const [notes, setNotes] = useState('');
  const save = useMutation({
    mutationFn: () =>
      scope.api.update(
        'due_items',
        row.id,
        mode === 'done' ? { status: 'done', doneOn: date, notes: notes || undefined } : { status: 'skipped', skipReason: reason, notes: notes || undefined },
        row.version
      ),
    onSuccess: () => {
      toast.success(mode === 'done' ? 'Done — the next one is planned' : 'Skipped — the next one is planned');
      onSaved();
      onClose();
    }
  });
  const fe = isApiError(save.error) ? save.error.fieldErrors : {};
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{mode === 'done' ? 'Mark as done' : 'Skip this time'}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">
          {row.customer} · {KIND[row.kind] ?? row.kind} due {prettyDate(row.dueDate)}. The next occurrence is planned from the schedule.
        </DialogDescription>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
          noValidate
          className="mt-4 space-y-4"
        >
          {mode === 'done' ? (
            <Field label="Actually done on" error={fe.doneOn}>
              <Input type="date" value={date} onChange={(e) => setDate(e.target.value)} max={plusDays(0)} />
            </Field>
          ) : (
            <Field label="Why" error={fe.skipReason}>
              <Select
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="Pick a reason"
                options={[
                  { value: 'declined', label: 'Customer declined' },
                  { value: 'done_elsewhere', label: 'Done elsewhere' },
                  { value: 'not_needed', label: 'Not needed this time' },
                  { value: 'unreachable', label: 'Could not reach' },
                  { value: 'moved', label: 'Customer moved' },
                  { value: 'other', label: 'Other' }
                ]}
              />
            </Field>
          )}
          <Field label="Notes">
            <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} />
          </Field>
          {save.isError && Object.keys(fe).length === 0 ? <Alert tone="danger">{errorText(save.error, 'Something went wrong.')}</Alert> : null}
          {save.isError && fe.status ? <Alert tone="danger">{fe.status}</Alert> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={mode === 'skip' && !reason}>
              {mode === 'done' ? 'Mark done' : 'Skip'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
