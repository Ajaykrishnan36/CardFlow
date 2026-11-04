import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Check, ListChecks, Network, Package, Trophy } from 'lucide-react';
import { commerceApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { RecordRow } from '@crm/api/types';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { AnimatedNumber, Gauge, ProgressBar, shortMoney, useEntered } from '@crm/components/charts';
import { cn } from '@crm/lib/utils';
import { useRecordScope } from './record-scope';
import { recordKeys, useObjectMeta } from './use-object-meta';

// A deal's workspace (D-176): its value and chance, the stages it has passed as a tracker you
// can click to move it, the people involved around the customer, the next steps with what
// is done, and what is being sold. Everything is the deal's own records; nothing is invented.

function num(v: unknown) {
  const n = typeof v === 'number' ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

function codeOf(prefix: string) {
  return prefix.startsWith('/w/') ? decodeURIComponent(prefix.slice(3)) : '';
}

export function DealWorkspace({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const qc = useQueryClient();
  const code = codeOf(scope.prefix);
  const meta = useObjectMeta('opportunities');
  const stages = (meta.data?.statuses ?? []).filter((s) => s.value !== 'closed_lost');
  const status = String(record.values.status ?? '');
  const at = stages.findIndex((s) => s.value === status);
  const amount = num(record.values.amount);
  const prob = status === 'closed_won' ? 100 : num(record.values.probability);
  const canEdit = scope.can('opportunities', 'update');
  const on = useEntered(100);

  const move = async (to: string) => {
    if (to === status || !canEdit) return;
    try {
      await scope.api.update('opportunities', record.id, { status: to }, record.version);
      toast.success(`Moved to ${stages.find((s) => s.value === to)?.label ?? to}`);
    } catch (e) {
      toast.error(isApiError(e) ? Object.values(e.fieldErrors ?? {})[0] ?? e.message : 'Could not move the deal.');
    } finally {
      void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, 'opportunities') });
    }
  };

  const daysIn = Math.max(0, Math.round((Date.now() - new Date(record.updatedAt ?? record.createdAt).getTime()) / 86400000));

  return (
    <div className="mb-4 space-y-4">
      <Card className="crm-rise overflow-hidden p-0">
        <div className="flex flex-wrap items-center gap-5 px-5 py-4">
          <div className="min-w-0 flex-1">
            <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Deal value</p>
            <p className="text-[34px] font-bold leading-tight tracking-tight text-foreground"><AnimatedNumber value={amount} format={(n) => shortMoney(n)} /></p>
            <p className="text-xs text-muted-foreground">
              {record.lookups.accountId?.label || record.lookups.contactId?.label || ''}
              {record.values.closeDate ? ` · closes ${new Date(String(record.values.closeDate)).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })}` : ''}
            </p>
          </div>
          <Gauge value={prob} size={84} thickness={7} track="hsl(var(--muted))">
            <span className="text-[15px] font-bold text-foreground">{Math.round(prob)}%</span>
          </Gauge>
          {canEdit && status !== 'closed_won' && status !== 'closed_lost' ? (
            <Button onClick={() => void move('closed_won')} className="rounded-xl"><Trophy /> Mark won</Button>
          ) : null}
        </div>
        {/* Stage tracker */}
        <div className="border-t bg-muted/30 px-3 py-3">
          <ol className="flex items-center gap-1.5 overflow-x-auto">
            {stages.map((s, i) => {
              const done = at >= 0 && i < at;
              const current = i === at;
              return (
                <li key={s.value} className="shrink-0">
                  <button
                    type="button"
                    disabled={!canEdit}
                    onClick={() => void move(s.value)}
                    className={cn(
                      'flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[13px] font-medium transition-all duration-500',
                      current ? 'bg-sidebar-active text-sidebar-active-foreground shadow-lift' : done ? 'text-primary hover:bg-primary-soft' : 'text-muted-foreground hover:bg-muted',
                      !canEdit && 'cursor-default'
                    )}
                    style={{ opacity: on ? 1 : 0, transform: on ? 'none' : 'translateY(4px)', transitionDelay: `${i * 70}ms` }}
                  >
                    <span className={cn('grid size-5 place-items-center rounded-full text-[10px] font-bold', current ? 'bg-mint text-navy' : done ? 'bg-primary text-primary-foreground' : 'border border-muted-foreground/40')}>
                      {done ? <Check className="size-3" /> : i + 1}
                    </span>
                    {s.label}
                    {current && status !== 'closed_won' ? <span className="rounded-full bg-white/15 px-1.5 text-[10px] font-semibold">day {daysIn}</span> : null}
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[1.35fr_1fr]">
        <Stakeholders code={code} record={record} />
        <div className="grid gap-4">
          <NextSteps record={record} />
          <Breakdown code={code} record={record} amount={amount} />
        </div>
      </div>
    </div>
  );
}

function Stakeholders({ code, record }: { code: string; record: RecordRow }) {
  const scope = useRecordScope();
  const q = useQuery({ queryKey: ['contact-roles', code, 'opportunities', record.id], queryFn: () => commerceApi(code).contactRoles('opportunities', record.id), enabled: Boolean(code) });
  const people = useMemo(() => {
    const list = (q.data?.data ?? []).filter((r) => r.isActive).map((r) => ({ id: r.contactId, name: r.contact, role: r.role, primary: r.isPrimary }));
    const main = record.lookups.contactId;
    if (main && !list.some((p) => p.id === main.id)) list.unshift({ id: main.id, name: main.label, role: 'Primary contact', primary: true });
    return list.slice(0, 8);
  }, [q.data, record.lookups.contactId]);
  const on = useEntered(150);
  const center = record.lookups.accountId?.label || record.lookups.contactId?.label || record.title;
  const W = 560;
  const H = 300;
  const cx = W / 2;
  const cy = H / 2 + 6;
  const pos = (i: number, n: number) => {
    const a = (-90 + (360 / Math.max(1, n)) * i) * (Math.PI / 180);
    return [cx + Math.cos(a) * 205, cy + Math.sin(a) * 112] as const;
  };
  return (
    <div className="crm-hero crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift">
      <div className="crm-hero-stars pointer-events-none absolute inset-0 opacity-50" aria-hidden />
      <div className="relative flex items-start justify-between gap-2">
        <div>
          <p className="flex items-center gap-2 text-[16px] font-semibold"><Network className="size-4 text-mint" /> Stakeholders</p>
          <p className="text-xs text-white/60">{people.length} {people.length === 1 ? 'person' : 'people'} · {people.filter((p) => p.primary).length} primary</p>
        </div>
        <span className="flex items-center gap-3 text-[11px] text-white/70">
          <span className="flex items-center gap-1"><span className="size-2 rounded-full bg-mint" /> Primary</span>
          <span className="flex items-center gap-1"><span className="size-2 rounded-full bg-chart-5" /> Involved</span>
        </span>
      </div>
      {people.length === 0 ? (
        <p className="relative py-16 text-center text-[13px] text-white/60">No one is linked to this deal yet. Add contact roles below to map who decides.</p>
      ) : (
        <div className="relative">
          <svg viewBox={`0 0 ${W} ${H}`} className="w-full" aria-hidden>
            {people.map((p, i) => {
              const [x, y] = pos(i, people.length);
              return (
                <path key={p.id} d={`M ${cx} ${cy} Q ${(cx + x) / 2} ${(cy + y) / 2 - 24} ${x} ${y}`} fill="none"
                  stroke={p.primary ? 'hsl(var(--mint))' : 'hsl(var(--chart-5))'} strokeWidth={1.6} strokeDasharray={p.primary ? undefined : '5 5'}
                  pathLength={1} style={{ strokeDasharray: p.primary ? 1 : undefined, strokeDashoffset: on || !p.primary ? 0 : 1, transition: `stroke-dashoffset 900ms ease ${i * 120}ms`, opacity: 0.85 }} />
              );
            })}
          </svg>
          <div className="absolute grid place-items-center" style={{ left: `${(cx / W) * 100}%`, top: `${(cy / H) * 100}%`, transform: 'translate(-50%, -50%)' }}>
            <span className="grid size-14 place-items-center rounded-2xl bg-primary text-[15px] font-bold text-primary-foreground shadow-[0_0_32px_hsl(var(--chart-1)/0.6)]">
              {center.split(/\s+/).map((w) => w[0]).slice(0, 2).join('').toUpperCase()}
            </span>
            <span className="mt-1 max-w-[140px] truncate text-center text-[12px] font-semibold text-white">{center}</span>
          </div>
          {people.map((p, i) => {
            const [x, y] = pos(i, people.length);
            return (
              <Link key={p.id} to={`${scope.routeBase}/contacts/${p.id}`}
                className="absolute flex items-center gap-2 transition-[opacity,transform] duration-500 hover:scale-105"
                style={{ left: `${(x / W) * 100}%`, top: `${(y / H) * 100}%`, transform: `translate(-50%, -50%) scale(${on ? 1 : 0.7})`, opacity: on ? 1 : 0, transitionDelay: `${200 + i * 110}ms` }}>
                <span className={cn('grid size-10 shrink-0 place-items-center rounded-full border-2 bg-white text-[12px] font-bold text-navy', p.primary ? 'border-mint' : 'border-chart-5')}>
                  {p.name.split(/\s+/).map((w) => w[0]).slice(0, 2).join('').toUpperCase()}
                </span>
                <span className="max-w-[120px]">
                  <span className="block truncate text-[12px] font-semibold text-white">{p.name}</span>
                  <span className={cn('block w-fit rounded-full px-1.5 text-[10px]', p.primary ? 'bg-mint/20 text-mint' : 'bg-white/10 text-white/75')}>{p.role}</span>
                </span>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

function NextSteps({ record }: { record: RecordRow }) {
  const scope = useRecordScope();
  const qc = useQueryClient();
  const params = { limit: 20, filter: JSON.stringify({ field: 'opportunityId', op: 'eq', value: record.id }) };
  const key = recordKeys.list(scope.prefix, 'tasks', params);
  const q = useQuery({ queryKey: key, queryFn: () => scope.api.list('tasks', params), enabled: scope.can('tasks', 'read') });
  const rows = q.data?.data ?? [];
  const done = rows.filter((r) => r.values.status === 'completed').length;
  const toggle = async (r: RecordRow) => {
    try {
      await scope.api.update('tasks', r.id, { status: r.values.status === 'completed' ? 'not_started' : 'completed' }, r.version);
    } catch (e) {
      toast.error(isApiError(e) ? e.message : 'Could not update the task.');
    } finally {
      void qc.invalidateQueries({ queryKey: recordKeys.lists(scope.prefix, 'tasks') });
    }
  };
  if (!scope.can('tasks', 'read')) return null;
  return (
    <Card className="crm-rise p-5">
      <div className="mb-2 flex items-center justify-between">
        <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><ListChecks className="size-4 text-primary" /> Next steps</p>
        <span className="text-xs font-semibold text-primary">{done}/{rows.length}</span>
      </div>
      <ProgressBar value={done} max={rows.length || 1} className="mb-3 h-1.5" />
      {rows.length === 0 ? (
        <p className="py-3 text-center text-[13px] text-muted-foreground">No tasks on this deal yet.</p>
      ) : (
        <ul className="space-y-1.5">
          {rows.slice(0, 6).map((r) => {
            const complete = r.values.status === 'completed';
            return (
              <li key={r.id} className="flex items-center gap-2.5">
                <button type="button" onClick={() => void toggle(r)} disabled={!scope.can('tasks', 'update')} aria-label={complete ? 'Mark not done' : 'Mark done'}
                  className={cn('grid size-5 shrink-0 place-items-center rounded-md border transition-colors', complete ? 'border-primary bg-primary text-primary-foreground' : 'border-input hover:border-primary')}>
                  {complete ? <Check className="size-3.5" /> : null}
                </button>
                <Link to={`${scope.routeBase}/tasks/${r.id}`} className={cn('min-w-0 flex-1 truncate text-[13px] hover:text-primary', complete ? 'text-muted-foreground line-through' : 'text-foreground')}>{r.title}</Link>
                <span className={cn('shrink-0 text-[11px]', complete ? 'text-success' : 'text-muted-foreground')}>{complete ? 'Done' : r.values.dueDate ? new Date(String(r.values.dueDate)).toLocaleDateString('en-IN', { day: 'numeric', month: 'short' }) : ''}</span>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

function Breakdown({ code, record, amount }: { code: string; record: RecordRow; amount: number }) {
  const q = useQuery({ queryKey: ['deal-lines', code, record.id, record.version], queryFn: () => commerceApi(code).lines('opportunities', record.id), enabled: Boolean(code) });
  const lines = q.data?.pricing.lines ?? [];
  // Shares are of the lines themselves, so they add up to 100%.
  const total = lines.reduce((n, l) => n + l.total, 0) || amount || 1;
  const colors = ['bg-chart-1', 'bg-chart-2', 'bg-chart-3', 'bg-chart-5', 'bg-chart-4'];
  const on = useEntered(120);
  return (
    <Card className="crm-rise p-5">
      <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><Package className="size-4 text-primary" /> Deal breakdown</p>
      <p className="mb-3 text-xs text-muted-foreground">{lines.length ? `${lines.length} items · ${shortMoney(q.data?.pricing.total ?? 0)} incl. tax` : 'No products on this deal yet.'}</p>
      {lines.length ? (
        <>
          <div className="mb-3 flex h-2.5 overflow-hidden rounded-full bg-muted">
            {lines.map((l, i) => (
              <span key={i} className={cn('h-full transition-[width] duration-1000', colors[i % colors.length])} style={{ width: on ? `${(l.total / total) * 100}%` : '0%' }} />
            ))}
          </div>
          <ul className="space-y-2">
            {lines.map((l, i) => (
              <li key={i} className="flex items-center gap-2.5 text-[13px]">
                <span className={cn('size-2.5 shrink-0 rounded-sm', colors[i % colors.length])} />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium text-foreground">{l.name}</p>
                  <p className="text-[11px] text-muted-foreground">{l.quantity} × {shortMoney(l.unitPrice)}{l.discountAmount ? ` · −${shortMoney(l.discountAmount)}` : ''}{l.taxRate ? ` · tax ${l.taxRate}%` : ''}</p>
                </div>
                <span className="shrink-0 font-semibold tabular-nums">{shortMoney(l.total)}</span>
                <span className="w-9 shrink-0 text-right text-[11px] text-muted-foreground">{Math.round((l.total / total) * 100)}%</span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </Card>
  );
}
