import { useMemo, useState, type DragEvent } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CalendarDays, CheckCircle2, Columns3, Hourglass, LayoutGrid, List, Plus, Search, Trophy } from 'lucide-react';
import { businessApi, type OverviewRange } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { RecordRow } from '@crm/api/types';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Skeleton } from '@crm/components/ui/spinner';
import { AnimatedNumber, Funnel, Gauge, MiniBars, ProgressBar, shortMoney } from '@crm/components/charts';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useRecordScope } from '@crm/features/records/record-scope';
import { recordKeys, useObjectMeta } from '@crm/features/records/use-object-meta';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { cn } from '@crm/lib/utils';

// The pipeline (D-176): the funnel and stage totals on top, then every deal — as a list
// (the default), a board to drag deals from stage to stage, or compact cards. Moving a deal
// saves it through the same rules as editing it; if the CRM refuses (say, a won deal needs
// its amount), the card goes back and the reason is shown.

type View = 'list' | 'board' | 'cards';

const STAGE_DOT: Record<string, string> = {
  prospecting: 'bg-chart-4', qualification: 'bg-chart-5', proposal: 'bg-chart-2', negotiation: 'bg-warning', closed_won: 'bg-success', closed_lost: 'bg-danger'
};

function num(v: unknown) {
  const n = typeof v === 'number' ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

export function PipelinePage() {
  const { code, context } = useWorkspace();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const base = workspaceBase(code);
  useDocumentTitle(`Pipeline · ${context.workspace.name}`);
  const [view, setView] = useState<View>(() => {
    try {
      return (localStorage.getItem('crm.pipeline.view') as View) || 'list';
    } catch {
      return 'list';
    }
  });
  const [q, setQ] = useState('');
  // ?stage= (a click on the funnel) shows that stage's deals only.
  const [sp, setSp] = useSearchParams();
  const stage = sp.get('stage') ?? '';
  const pickStage = (s: string) => {
    const next = new URLSearchParams(sp);
    if (s) next.set('stage', s);
    else next.delete('stage');
    setSp(next, { replace: true });
  };
  const [range] = useState<OverviewRange>('quarter');
  const pickView = (v: View) => {
    setView(v);
    try {
      localStorage.setItem('crm.pipeline.view', v);
    } catch {
      /* private window */
    }
  };
  const meta = useObjectMeta('opportunities');
  const params = { limit: 200 };
  const listKey = recordKeys.list(scope.prefix, 'opportunities', params);
  const list = useQuery({ queryKey: listKey, queryFn: () => scope.api.list('opportunities', params), staleTime: 15_000 });
  const ov = useQuery({ queryKey: ['workspace', code, 'overview', range], queryFn: () => businessApi.overview(code, range), staleTime: 30_000 });
  const stages = (meta.data?.statuses ?? []).filter((s) => s.value !== 'closed_lost');
  const rows = useMemo(() => {
    const all = list.data?.data ?? [];
    const term = q.trim().toLowerCase();
    return all
      .filter((r) => !stage || r.values.status === stage)
      .filter((r) => !term || `${r.title} ${r.lookups.accountId?.label ?? ''} ${r.lookups.contactId?.label ?? ''}`.toLowerCase().includes(term));
  }, [list.data, q, stage]);
  const open = rows.filter((r) => !['closed_won', 'closed_lost'].includes(String(r.values.status)));
  const openValue = open.reduce((n, r) => n + num(r.values.amount), 0);
  const canEdit = scope.can('opportunities', 'update');
  const canCreate = scope.can('opportunities', 'create');

  const move = async (row: RecordRow, status: string) => {
    if (row.values.status === status) return;
    const prev = qc.getQueryData(listKey);
    qc.setQueryData(listKey, (old: typeof list.data) =>
      old ? { ...old, data: old.data.map((r) => (r.id === row.id ? { ...r, values: { ...r.values, status } } : r)) } : old
    );
    try {
      await scope.api.update('opportunities', row.id, { status }, row.version);
      toast.success(`${row.title} moved to ${stages.find((s) => s.value === status)?.label ?? status}`);
    } catch (e) {
      qc.setQueryData(listKey, prev);
      const msg = isApiError(e) ? Object.values(e.fieldErrors ?? {})[0] ?? e.message : 'Could not move the deal.';
      toast.error(msg);
    } finally {
      void qc.invalidateQueries({ queryKey: recordKeys.lists(scope.prefix, 'opportunities') });
      void qc.invalidateQueries({ queryKey: ['workspace', code, 'overview'] });
    }
  };

  const d = ov.data;
  return (
    <div className="mx-auto w-full max-w-[1440px] space-y-4 px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[26px] font-bold tracking-tight text-foreground">Pipeline</h1>
          <p className="text-[13px] text-muted-foreground">
            <span className="font-medium text-foreground">{open.length} open deals</span> · {shortMoney(openValue)} in motion
            {d ? <> · {d.period}</> : null}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search deals, accounts…" leading={<Search />} className="h-9 w-56 rounded-full" />
          <div className="inline-flex rounded-full border bg-card p-0.5 shadow-card" role="tablist" aria-label="View">
            {([['list', List, 'List'], ['board', Columns3, 'Board'], ['cards', LayoutGrid, 'Cards']] as const).map(([k, Icon, label]) => (
              <button key={k} type="button" role="tab" aria-selected={view === k} onClick={() => pickView(k)}
                className={cn('inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-colors', view === k ? 'bg-sidebar-active text-sidebar-active-foreground' : 'text-muted-foreground hover:text-foreground')}>
                <Icon className="size-3.5" /> {label}
              </button>
            ))}
          </div>
          {canCreate ? (
            <Button asChild size="sm" className="rounded-full">
              <Link to={`${base}/opportunities?new=1`}><Plus /> New deal</Link>
            </Button>
          ) : null}
        </div>
      </div>

      {/* Analytics */}
      {!d ? (
        <Skeleton className="h-[300px] rounded-2xl" />
      ) : (
        <div className="grid gap-4 xl:grid-cols-[1.9fr_1fr]">
          <div className="crm-hero crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift">
            <div className="crm-hero-stars pointer-events-none absolute inset-0 opacity-60" aria-hidden />
            <p className="relative flex items-center gap-2 text-[16px] font-semibold">Stage by stage <span className="text-xs font-normal text-white/60">· {d.period}</span></p>
            <div className="relative mt-3 grid items-center gap-4 md:grid-cols-[1fr_1.3fr_auto]">
              <ul className="space-y-2">
                {d.funnel.map((s, i) => {
                  const prev = i > 0 ? d.funnel[i - 1]!.count : 0;
                  return (
                    <li key={s.key} className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <p className="text-[13px] font-medium text-white">{s.label}</p>
                        <p className="text-[11px] text-white/55">{s.value ? shortMoney(s.value) : '—'}{i > 0 && prev ? ` · ${Math.round((s.count / prev) * 100)}% of the stage before` : ''}</p>
                      </div>
                      <span className="text-[18px] font-semibold tabular-nums text-white"><AnimatedNumber value={s.count} /></span>
                    </li>
                  );
                })}
              </ul>
              <Funnel stages={d.funnel} height={230} format={(n) => shortMoney(n)} onSelect={(s) => (s.key === 'leads' ? undefined : pickStage(s.key === stage ? '' : s.key))} />
              <Gauge value={d.winRate ?? 0} size={170} thickness={10}>
                <div>
                  <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-white/60">Win rate</p>
                  <p className="text-[30px] font-bold leading-none text-white"><AnimatedNumber value={d.winRate ?? 0} format={(n) => `${Math.round(n)}%`} /></p>
                </div>
              </Gauge>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-1">
            <div className="crm-mint-card crm-rise rounded-2xl p-5 shadow-lift">
              <p className="text-[13px] text-white/85">Open pipeline</p>
              <p className="text-[32px] font-bold leading-tight"><AnimatedNumber value={d.openPipeline?.value ?? 0} format={(n) => shortMoney(n)} /></p>
              <p className="text-[13px] text-white/85">{d.openPipeline?.count ?? 0} open · expected {shortMoney(d.expectedRevenue)}</p>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Card className="crm-rise p-4">
                <Trophy className="size-4 text-primary" />
                <p className="mt-2 text-xs text-muted-foreground">Win rate</p>
                <p className="text-[22px] font-bold">{d.winRate !== undefined ? `${Math.round(d.winRate)}%` : '—'}</p>
                <MiniBars values={d.winRateTrend?.length ? d.winRateTrend : [0]} className="mt-1 h-6" />
              </Card>
              <Card className="crm-rise p-4">
                <Hourglass className="size-4 text-primary" />
                <p className="mt-2 text-xs text-muted-foreground">Avg. sales cycle</p>
                <p className="text-[22px] font-bold">{d.avgCycleDays !== undefined ? `${d.avgCycleDays.toFixed(d.avgCycleDays < 10 ? 1 : 0)}d` : '—'}</p>
                <p className="text-[11px] text-muted-foreground">open → won</p>
              </Card>
            </div>
          </div>
        </div>
      )}

      {stage ? (
        <div className="flex items-center gap-2 text-[13px]">
          <span className="text-muted-foreground">Showing</span>
          <button type="button" onClick={() => pickStage('')} className="inline-flex items-center gap-1.5 rounded-full bg-sidebar-active px-3 py-1 text-xs font-semibold text-sidebar-active-foreground">
            {stages.find((s) => s.value === stage)?.label ?? stage} ×
          </button>
          <span className="text-muted-foreground">deals only — click to show all</span>
        </div>
      ) : null}

      {/* Deals */}
      {list.isPending || meta.isPending ? (
        <Skeleton className="h-[420px] rounded-2xl" />
      ) : view === 'board' ? (
        <Board stages={stages} rows={rows} base={base} canEdit={canEdit} onMove={move} />
      ) : view === 'cards' ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {rows.map((r, i) => (
            <DealCard key={r.id} r={r} base={base} i={i} stage={stages.find((s) => s.value === r.values.status)?.label} />
          ))}
        </div>
      ) : (
        <DealList rows={rows} base={base} stages={stages} />
      )}
    </div>
  );
}

function Board({ stages, rows, base, canEdit, onMove }: { stages: Array<{ value: string; label: string }>; rows: RecordRow[]; base: string; canEdit: boolean; onMove: (r: RecordRow, s: string) => void }) {
  const [over, setOver] = useState<string | null>(null);
  const byId = useMemo(() => new Map(rows.map((r) => [r.id, r])), [rows]);
  const total = rows.reduce((n, r) => n + (String(r.values.status) !== 'closed_lost' ? num(r.values.amount) : 0), 0) || 1;
  return (
    <div className="crm-scroll -mx-1 flex gap-3 overflow-x-auto px-1 pb-2">
      {stages.map((s) => {
        const items = rows.filter((r) => r.values.status === s.value);
        const value = items.reduce((n, r) => n + num(r.values.amount), 0);
        return (
          <div
            key={s.value}
            onDragOver={(e: DragEvent) => {
              if (!canEdit) return;
              e.preventDefault();
              setOver(s.value);
            }}
            onDragLeave={() => setOver((o) => (o === s.value ? null : o))}
            onDrop={(e: DragEvent) => {
              e.preventDefault();
              setOver(null);
              const r = byId.get(e.dataTransfer.getData('text/plain'));
              if (r) onMove(r, s.value);
            }}
            className={cn('flex w-[280px] shrink-0 flex-col rounded-2xl border bg-muted/40 p-2.5 transition-colors', over === s.value && 'border-primary bg-primary-soft/60')}
          >
            <div className="mb-1 px-1">
              <p className="flex items-center justify-between text-[13px] font-semibold text-foreground">
                <span className="flex items-center gap-2"><span className={cn('size-2 rounded-full', STAGE_DOT[s.value] ?? 'bg-primary')} />{s.label} <span className="font-normal text-muted-foreground">{items.length}</span></span>
                <span className="tabular-nums">{shortMoney(value)}</span>
              </p>
              <ProgressBar value={value} max={total} className="mt-2 h-1" />
              <p className="mt-1 text-[11px] text-muted-foreground">{Math.round((value / total) * 100)}% of the pipeline</p>
            </div>
            <div className="mt-1 flex min-h-[80px] flex-col gap-2">
              {items.map((r, i) => (
                <div key={r.id} draggable={canEdit} onDragStart={(e) => e.dataTransfer.setData('text/plain', r.id)} className={cn(canEdit && 'cursor-grab active:cursor-grabbing')}>
                  <DealCard r={r} base={base} i={i} compact />
                </div>
              ))}
              {items.length === 0 ? <p className="rounded-xl border border-dashed px-3 py-6 text-center text-xs text-muted-foreground">{canEdit ? 'Drop a deal here' : 'No deals'}</p> : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function DealCard({ r, base, i, compact, stage }: { r: RecordRow; base: string; i: number; compact?: boolean; stage?: string }) {
  const amount = num(r.values.amount);
  const prob = num(r.values.probability);
  const close = String(r.values.closeDate ?? '');
  const late = close && close < new Date().toISOString().slice(0, 10) && !['closed_won', 'closed_lost'].includes(String(r.values.status));
  const won = r.values.status === 'closed_won';
  const owner = r.lookups.ownerId?.label ?? '';
  return (
    <Link to={`${base}/opportunities/${r.id}`} draggable={false}
      className={cn('crm-rise block rounded-xl border bg-card p-3 shadow-card transition-[box-shadow,transform] hover:-translate-y-0.5 hover:shadow-lift', late && 'border-l-[3px] border-l-warning')}
      style={{ animationDelay: `${Math.min(i, 12) * 35}ms` }}>
      <div className="flex items-start gap-2.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-primary-soft text-[11px] font-bold text-primary">{initials(r.lookups.accountId?.label || r.lookups.contactId?.label || r.title)}</span>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[13px] font-semibold text-foreground">{r.lookups.accountId?.label || r.lookups.contactId?.label || r.title}</p>
          <p className="truncate text-[11px] text-muted-foreground">{r.title}</p>
        </div>
        {won ? <CheckCircle2 className="size-4 shrink-0 text-success" /> : null}
      </div>
      <div className="mt-2.5 flex items-center justify-between gap-2">
        <span className="text-[17px] font-bold tabular-nums text-foreground">{shortMoney(amount)}</span>
        {!won ? (
          <span className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="flex gap-[2px]">{[25, 50, 75, 100].map((t) => <span key={t} className={cn('h-2.5 w-1 rounded-sm', prob >= t ? 'bg-primary' : 'bg-muted-foreground/25')} />)}</span>
            {prob ? `${Math.round(prob)}%` : ''}
          </span>
        ) : <span className="text-[11px] font-semibold text-success">Won</span>}
      </div>
      {!compact && stage ? <p className="mt-1 text-[11px] text-muted-foreground">{stage}</p> : null}
      <div className="mt-2 flex items-center justify-between gap-2 border-t pt-2 text-[11px] text-muted-foreground">
        <span className="truncate">{owner}{close ? ` · ${new Date(close).toLocaleDateString('en-IN', { day: 'numeric', month: 'short' })}` : ''}</span>
        {late ? <span className="flex items-center gap-1 font-semibold text-warning"><CalendarDays className="size-3" /> late</span> : r.values.nextStep ? <span className="truncate text-primary">{String(r.values.nextStep)}</span> : null}
      </div>
    </Link>
  );
}

function DealList({ rows, base, stages }: { rows: RecordRow[]; base: string; stages: Array<{ value: string; label: string; tone?: string }> }) {
  const [sort, setSort] = useState<{ key: 'amount' | 'closeDate' | 'title'; dir: 1 | -1 }>({ key: 'amount', dir: -1 });
  const [page, setPage] = useState(0);
  const per = 25;
  const sorted = useMemo(() => {
    const out = [...rows];
    out.sort((a, b) => {
      const av = sort.key === 'amount' ? num(a.values.amount) : sort.key === 'title' ? a.title : String(a.values.closeDate ?? '');
      const bv = sort.key === 'amount' ? num(b.values.amount) : sort.key === 'title' ? b.title : String(b.values.closeDate ?? '');
      return (av < bv ? -1 : av > bv ? 1 : 0) * sort.dir;
    });
    return out;
  }, [rows, sort]);
  const pages = Math.max(1, Math.ceil(sorted.length / per));
  const shown = sorted.slice(page * per, page * per + per);
  const th = (key: typeof sort.key, label: string, right?: boolean) => (
    <th className={cn('px-4 py-2.5 font-medium', right && 'text-right')}>
      <button type="button" className="inline-flex items-center gap-1 hover:text-foreground" onClick={() => setSort((s) => ({ key, dir: s.key === key ? (s.dir === 1 ? -1 : 1) : -1 }))}>
        {label}{sort.key === key ? (sort.dir === 1 ? ' ↑' : ' ↓') : ''}
      </button>
    </th>
  );
  return (
    <Card className="overflow-hidden">
      <div className="crm-scroll overflow-x-auto">
        <table className="w-full min-w-[820px] text-[13px]">
          <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
            <tr>
              {th('title', 'Deal')}
              <th className="px-4 py-2.5 font-medium">Customer</th>
              <th className="px-4 py-2.5 font-medium">Stage</th>
              {th('amount', 'Amount', true)}
              <th className="px-4 py-2.5 font-medium">Probability</th>
              {th('closeDate', 'Close date')}
              <th className="px-4 py-2.5 font-medium">Owner</th>
              <th className="px-4 py-2.5 font-medium">Next step</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {shown.map((r, i) => {
              const st = stages.find((s) => s.value === r.values.status);
              return (
                <tr key={r.id} className="crm-rise transition-colors hover:bg-muted/40" style={{ animationDelay: `${Math.min(i, 15) * 20}ms` }}>
                  <td className="max-w-[260px] px-4 py-2.5"><Link to={`${base}/opportunities/${r.id}`} className="block truncate font-medium text-foreground hover:text-primary">{r.title}</Link></td>
                  <td className="max-w-[200px] truncate px-4 py-2.5 text-muted-foreground">{r.lookups.accountId?.label || r.lookups.contactId?.label || '—'}</td>
                  <td className="px-4 py-2.5">
                    <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-foreground">
                      <span className={cn('size-1.5 rounded-full', STAGE_DOT[String(r.values.status)] ?? 'bg-primary')} />{st?.label ?? (r.values.status === 'closed_lost' ? 'Closed lost' : String(r.values.status ?? ''))}
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-right font-semibold tabular-nums">{shortMoney(num(r.values.amount))}</td>
                  <td className="px-4 py-2.5"><div className="flex items-center gap-2"><ProgressBar value={num(r.values.probability)} className="h-1.5 w-16" /><span className="text-xs text-muted-foreground">{num(r.values.probability) ? `${Math.round(num(r.values.probability))}%` : ''}</span></div></td>
                  <td className="px-4 py-2.5 text-muted-foreground">{r.values.closeDate ? new Date(String(r.values.closeDate)).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }) : '—'}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">{r.lookups.ownerId?.label ?? ''}</td>
                  <td className="max-w-[200px] truncate px-4 py-2.5 text-muted-foreground">{String(r.values.nextStep ?? '')}</td>
                </tr>
              );
            })}
            {shown.length === 0 ? <tr><td colSpan={8} className="px-4 py-10 text-center text-muted-foreground">No deals match.</td></tr> : null}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
        <span>{sorted.length} deals</span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage((p) => p - 1)}>Previous</Button>
          <span>Page {page + 1} of {pages}</span>
          <Button variant="outline" size="sm" disabled={page + 1 >= pages} onClick={() => setPage((p) => p + 1)}>Next</Button>
        </div>
      </div>
    </Card>
  );
}

function initials(s: string) {
  return s.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join('') || '·';
}
