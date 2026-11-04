import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  Activity, AlertTriangle, ArrowRight, Banknote, CalendarClock, CheckCircle2, ClipboardList, FileText, Handshake, Hourglass, Percent, Receipt,
  ShoppingCart, Sparkles, Trophy, TrendingUp, UserPlus, Users, Wallet
} from 'lucide-react';
import { businessApi, type Overview, type OverviewKpi, type OverviewRange } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { AnimatedNumber, Delta, Funnel, Gauge, LineChart, MiniBars, ProgressBar, Sparkline, shortMoney } from '@crm/components/charts';
import { formatMoney } from '@crm/lib/money';
import { cn } from '@crm/lib/utils';
import { workspaceBase } from './workspace-context';

// The owner's dashboard (D-176): the pipeline as a live funnel, the money, eight numbers
// against the period before, deals at risk, revenue over time, what is selling, who is
// selling, what is coming up and what just happened — all from the business's records.

const KPI_ICONS: Record<string, typeof Users> = {
  leads: UserPlus, openOpportunities: Handshake, won: Trophy, revenue: Wallet, newCustomers: Users, orders: ShoppingCart,
  pendingPayments: Receipt, conversion: Percent
};

const RANGES: Array<{ key: OverviewRange; label: string }> = [
  { key: 'month', label: 'This month' },
  { key: 'quarter', label: 'This quarter' },
  { key: 'year', label: 'This year' }
];

const kpiText = (k: OverviewKpi) => (n: number) => (k.money ? shortMoney(n) : k.percent ? `${n.toFixed(n < 10 && n % 1 ? 1 : 0)}%` : Math.round(n).toLocaleString('en-IN'));

export function OverviewSection({ code }: { code: string }) {
  const [range, setRange] = useState<OverviewRange>(() => {
    try {
      return (localStorage.getItem('crm.overview.range') as OverviewRange) || 'month';
    } catch {
      return 'month';
    }
  });
  const q = useQuery({
    queryKey: ['workspace', code, 'overview', range],
    queryFn: () => businessApi.overview(code, range),
    staleTime: 30_000,
    refetchInterval: 60_000,
    placeholderData: (prev) => prev
  });
  const pick = (r: OverviewRange) => {
    setRange(r);
    try {
      localStorage.setItem('crm.overview.range', r);
    } catch {
      /* private window */
    }
  };
  if (q.isError) return null;
  const d = q.data;
  return (
    <section className="space-y-4" aria-label="Business overview">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-[13px] text-muted-foreground">
          {d ? <>Showing <span className="font-medium text-foreground">{d.period}</span> against the same days of the period before.</> : 'Loading your numbers…'}
        </p>
        <div className="inline-flex rounded-full border bg-card p-0.5 shadow-card" role="tablist" aria-label="Period">
          {RANGES.map((r) => (
            <button
              key={r.key}
              type="button"
              role="tab"
              aria-selected={range === r.key}
              onClick={() => pick(r.key)}
              className={cn('rounded-full px-3 py-1 text-xs font-medium transition-colors', range === r.key ? 'bg-sidebar-active text-sidebar-active-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
      {!d ? <OverviewSkeleton /> : <OverviewBody code={code} d={d} />}
    </section>
  );
}

function OverviewBody({ code, d }: { code: string; d: Overview }) {
  const base = workspaceBase(code);
  const won = d.funnel.find((s) => s.key === 'closed_won');
  const openBefore = d.kpis.find((k) => k.key === 'openOpportunities')?.before;
  return (
    <>
      <div className="grid gap-4 xl:grid-cols-3">
        <PipelineHero d={d} base={base} />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-2 xl:content-start">
          <div className="crm-mint-card crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift sm:col-span-2">
            <div className="flex items-start justify-between">
              <p className="text-[13px] font-medium text-white/85">Open pipeline</p>
              {d.openPipeline && openBefore !== undefined ? (
                <span className="rounded-full bg-white/15 px-2 py-0.5 text-[11px] font-semibold text-white">
                  {openBefore ? `${d.openPipeline.count >= openBefore ? '↑' : '↓'} ${Math.abs(Math.round(((d.openPipeline.count - openBefore) / openBefore) * 100))}% open deals` : 'new'}
                </span>
              ) : null}
            </div>
            <p className="mt-1 text-[34px] font-bold leading-tight tracking-tight">
              <AnimatedNumber value={d.openPipeline?.value ?? 0} format={(n) => shortMoney(n)} />
            </p>
            <p className="text-[13px] text-white/85">
              {d.openPipeline?.count ?? 0} open deals · expected {shortMoney(d.expectedRevenue)}
            </p>
            <Sparkline points={d.wonTrend.length ? d.wonTrend : [0, 0]} color="hsl(152 76% 85%)" width={520} height={38} className="mt-3 w-full" />
            <div className="mt-2 flex flex-wrap gap-2 text-[11px]">
              <span className="rounded-full bg-white/20 px-2 py-0.5 font-semibold">{shortMoney(d.wonValue)} won</span>
              <span className="rounded-full bg-white/10 px-2 py-0.5">{shortMoney(d.revenue)} received</span>
            </div>
          </div>
          <Card className="crm-rise p-4" style={{ animationDelay: '80ms' }}>
            <div className="flex items-center justify-between">
              <span className="grid size-8 place-items-center rounded-lg bg-primary-soft text-primary"><Trophy className="size-4" /></span>
              {d.winRate !== undefined && d.winRateBefore !== undefined ? <Delta now={d.winRate} before={d.winRateBefore} /> : null}
            </div>
            <p className="mt-3 text-xs text-muted-foreground">Win rate</p>
            <p className="text-[26px] font-bold leading-tight text-foreground">
              {d.winRate !== undefined ? <AnimatedNumber value={d.winRate} format={(n) => `${Math.round(n)}%`} /> : '—'}
            </p>
            <MiniBars values={d.winRateTrend?.length ? d.winRateTrend : [0]} className="mt-2" />
          </Card>
          <Card className="crm-rise p-4" style={{ animationDelay: '140ms' }}>
            <div className="flex items-center justify-between">
              <span className="grid size-8 place-items-center rounded-lg bg-navy-soft text-navy dark:text-chart-2"><Hourglass className="size-4" /></span>
              {d.avgCycleDays !== undefined && d.avgCycleDaysBefore !== undefined ? <Delta now={d.avgCycleDays} before={d.avgCycleDaysBefore} invert /> : null}
            </div>
            <p className="mt-3 text-xs text-muted-foreground">Avg. sales cycle</p>
            <p className="text-[26px] font-bold leading-tight text-foreground">
              {d.avgCycleDays !== undefined ? <><AnimatedNumber value={d.avgCycleDays} format={(n) => n.toFixed(n < 10 ? 1 : 0)} /> <span className="text-sm font-medium text-muted-foreground">days</span></> : '—'}
            </p>
            <p className="mt-2 text-[11px] text-muted-foreground">From the deal opening to won · {won?.count ?? 0} won</p>
          </Card>
        </div>
      </div>

      {d.atRisk.length ? (
        <Card className="crm-rise flex items-stretch gap-3 overflow-x-auto border-warning/30 bg-warning-soft/40 p-3">
          <div className="flex shrink-0 items-center gap-3 pr-2">
            <span className="grid size-10 place-items-center rounded-xl bg-warning text-white"><AlertTriangle className="size-5" /></span>
            <div>
              <p className="text-[13px] font-semibold text-foreground">{d.atRisk.length} deals at risk</p>
              <p className="text-xs text-warning">{shortMoney(d.atRisk.reduce((n, r) => n + (r.amount || 0), 0))} needs attention</p>
            </div>
          </div>
          {d.atRisk.map((r) => (
            <Link key={r.id} to={`${base}/opportunities/${r.id}`} className="min-w-[200px] shrink-0 rounded-xl border bg-card px-3 py-2 transition-shadow hover:shadow-lift">
              <p className="flex items-center justify-between gap-2 text-[13px] font-medium text-foreground">
                <span className="truncate">{r.name}</span>
                <span className="tabular-nums text-muted-foreground">{shortMoney(r.amount || 0)}</span>
              </p>
              <p className="truncate text-xs text-warning">{r.reason}</p>
            </Link>
          ))}
        </Card>
      ) : null}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {d.kpis.map((k, i) => (
          <KpiTile key={k.key} k={k} i={i} />
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card className="crm-rise p-5 xl:col-span-2">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div>
              <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><TrendingUp className="size-4 text-primary" /> Revenue trend</p>
              <p className="text-xs text-muted-foreground">Money received and deals won, {d.range === 'year' ? 'by month' : d.range === 'quarter' ? 'by fortnight' : 'by week'}</p>
            </div>
            <div className="flex items-center gap-3 text-xs text-muted-foreground">
              <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-chart-1" /> Received</span>
              <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-chart-2" /> Won</span>
            </div>
          </div>
          <LineChart
            labels={d.trendLabels}
            format={(n) => shortMoney(n)}
            series={[
              { key: 'rev', label: 'Received', color: 'hsl(var(--chart-1))', points: d.revenueTrend, area: true },
              { key: 'won', label: 'Won', color: 'hsl(var(--chart-2))', points: d.wonTrend }
            ]}
          />
          <div className="mt-3 grid grid-cols-2 gap-3 border-t pt-3 sm:grid-cols-4">
            <Stat label="Received" value={d.revenue} />
            <Stat label="Closed won" value={d.wonValue} />
            <Stat label="Expected" value={d.expectedRevenue} />
            <Stat label="Pending payments" value={d.pendingPayments} warn={d.pendingPayments > 0} />
          </div>
        </Card>
        <ActivityFeed d={d} base={base} />
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="crm-rise p-5">
          <p className="mb-3 flex items-center gap-2 text-[15px] font-semibold text-foreground"><Sparkles className="size-4 text-primary" /> Top products & services</p>
          {d.topProducts.length === 0 ? (
            <p className="py-6 text-center text-[13px] text-muted-foreground">Nothing invoiced in this period yet.</p>
          ) : (
            <ul className="space-y-3">
              {d.topProducts.map((p) => (
                <li key={p.name}>
                  <div className="mb-1 flex items-center justify-between gap-2 text-[13px]">
                    <span className="truncate font-medium text-foreground">{p.name}</span>
                    <span className="shrink-0 tabular-nums text-muted-foreground">{p.count} · {shortMoney(p.value)}</span>
                  </div>
                  <ProgressBar value={p.value} max={d.topProducts[0]!.value} />
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card className="crm-rise p-5">
          <p className="mb-3 flex items-center gap-2 text-[15px] font-semibold text-foreground"><Trophy className="size-4 text-primary" /> Sales leaderboard</p>
          {d.leaderboard.length === 0 ? (
            <p className="py-6 text-center text-[13px] text-muted-foreground">No deals won in this period yet.</p>
          ) : (
            <ul className="space-y-3">
              {d.leaderboard.map((l, i) => (
                <li key={l.id ?? l.name} className="flex items-center gap-3">
                  <span className="w-4 text-center text-xs font-semibold text-muted-foreground">{i + 1}</span>
                  <span className={cn('grid size-8 shrink-0 place-items-center rounded-full text-[11px] font-semibold', i === 0 ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground')}>{l.initials}</span>
                  <div className="min-w-0 flex-1">
                    <div className="mb-1 flex items-center justify-between gap-2 text-[13px]">
                      <span className="truncate font-medium text-foreground">{l.name}</span>
                      <span className="tabular-nums text-muted-foreground">{l.count} won · {shortMoney(l.value)}</span>
                    </div>
                    <ProgressBar value={l.value} max={d.leaderboard[0]!.value} />
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card className="crm-rise p-5">
          <p className="mb-3 flex items-center gap-2 text-[15px] font-semibold text-foreground"><CalendarClock className="size-4 text-primary" /> Coming up · 7 days</p>
          {d.upcoming.length === 0 ? (
            <p className="py-6 text-center text-[13px] text-muted-foreground">No tasks or appointments in the next 7 days.</p>
          ) : (
            <ul className="divide-y">
              {d.upcoming.map((u) => (
                <li key={u.object + u.id}>
                  <Link to={`${base}/${u.object}/${u.id}`} className="flex items-center gap-3 py-2 hover:text-primary">
                    <span className={cn('grid size-8 shrink-0 place-items-center rounded-lg', u.object === 'tasks' ? 'bg-primary-soft text-primary' : 'bg-navy-soft text-navy dark:text-chart-2')}>
                      {u.object === 'tasks' ? <ClipboardList className="size-4" /> : <CalendarClock className="size-4" />}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[13px] font-medium text-foreground">{u.title}</p>
                      <p className="truncate text-xs text-muted-foreground">{whenText(u.when)}{u.who ? ` · ${u.who}` : ''}</p>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </>
  );
}

function PipelineHero({ d, base }: { d: Overview; base: string }) {
  const navigate = useNavigate();
  const latest = d.activity[0];
  const won = d.funnel.find((s) => s.key === 'closed_won');
  const rate = d.winRate ?? 0;
  return (
    <div className="crm-hero crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift xl:col-span-2">
      <div className="crm-hero-stars pointer-events-none absolute inset-0 opacity-60" aria-hidden />
      <div className="relative flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="flex items-center gap-2 text-[16px] font-semibold">
            Pipeline flow
            <span className="inline-flex items-center gap-1 rounded-full bg-white/10 px-2 py-0.5 text-[11px] font-medium text-mint">
              <span className="size-1.5 rounded-full bg-mint" style={{ animation: 'crm-pulse-glow 1.6s ease-in-out infinite' }} /> Live
            </span>
          </p>
          {latest ? (
            <p className="mt-1 truncate text-xs text-white/70">
              <span className="font-semibold text-white">{latest.actor}</span> {verb(latest)} {latest.title} · {ago(latest.at)}
            </p>
          ) : null}
        </div>
        <Link to={`${base}/pipeline`} className="inline-flex items-center gap-1 rounded-full bg-white/10 px-3 py-1 text-xs font-medium text-white hover:bg-white/20">
          Open pipeline <ArrowRight className="size-3.5" />
        </Link>
      </div>
      <div className="relative mt-4 grid items-center gap-4 md:grid-cols-[minmax(150px,1fr)_minmax(200px,1.4fr)_minmax(180px,1fr)]">
        <ul className="space-y-2.5">
          {d.funnel.map((s) => (
            <li key={s.key} className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="flex items-center gap-1.5 text-[13px] font-medium text-white"><span className="size-1.5 rounded-full bg-chart-5" />{s.label}</p>
                {s.value ? <p className="pl-3 text-[11px] text-white/55">{shortMoney(s.value)}</p> : s.key === 'leads' ? <p className="pl-3 text-[11px] text-white/55">new this period</p> : null}
              </div>
              <span className="text-[18px] font-semibold tabular-nums text-white"><AnimatedNumber value={s.count} /></span>
            </li>
          ))}
        </ul>
        <Funnel stages={d.funnel} height={250} format={(n) => shortMoney(n)}
          onSelect={(s) => navigate(s.key === 'leads' ? `${base}/leads` : `${base}/pipeline?stage=${encodeURIComponent(s.key)}`)} />
        <div className="flex flex-col items-center gap-3">
          <Gauge value={rate} size={190} thickness={11}>
            <div>
              <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-white/60">Win rate</p>
              <p className="text-[34px] font-bold leading-none text-white"><AnimatedNumber value={rate} format={(n) => `${Math.round(n)}%`} /></p>
              <p className="mt-1 text-[11px] text-white/60">{won?.count ?? 0} won · {d.lost?.count ?? 0} lost</p>
            </div>
          </Gauge>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-white/15 bg-white/5 px-3 py-1 text-xs text-white">
            <CheckCircle2 className="size-3.5 text-mint" /> <span className="font-semibold">{won?.count ?? 0} won</span>
            <span className="text-white/70">{shortMoney(d.wonValue)} closed {d.range === 'month' ? 'this month' : d.range === 'quarter' ? 'this quarter' : 'this year'}</span>
          </span>
        </div>
      </div>
    </div>
  );
}

function KpiTile({ k, i }: { k: OverviewKpi; i: number }) {
  const Icon = KPI_ICONS[k.key] ?? Activity;
  const body = (
    <Card className="crm-rise h-full p-4 transition-shadow hover:shadow-lift" style={{ animationDelay: `${i * 50}ms` }}>
      <div className="flex items-start justify-between gap-2">
        <span className="grid size-8 place-items-center rounded-lg bg-primary-soft text-primary"><Icon className="size-4" /></span>
        {k.before !== undefined ? <Delta now={k.value} before={k.before} invert={k.invert} /> : null}
      </div>
      <p className="mt-3 truncate text-xs text-muted-foreground">{k.label}</p>
      <p className="text-[22px] font-bold leading-tight text-foreground"><AnimatedNumber value={k.value} format={kpiText(k)} /></p>
      <div className="mt-1 flex items-end justify-between gap-2">
        <p className="min-w-0 truncate text-[11px] text-muted-foreground">
          {k.before !== undefined ? `vs ${kpiText(k)(k.before)} before` : k.sub}
        </p>
        <Sparkline points={k.trend} width={70} height={24} color={k.invert ? 'hsl(var(--warning))' : 'hsl(var(--chart-1))'} />
      </div>
    </Card>
  );
  return k.path ? <Link to={k.path} className="block rounded-2xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">{body}</Link> : body;
}

const FEED_FILTERS = [
  { key: 'all', label: 'All' },
  { key: 'leads', label: 'Leads' },
  { key: 'deals', label: 'Deals' },
  { key: 'money', label: 'Money' },
  { key: 'follow', label: 'Follow-ups' }
];
const feedMatch: Record<string, (c: string) => boolean> = {
  all: () => true,
  leads: (c) => c === 'leads' || c === 'customers',
  deals: (c) => c === 'deals' || c === 'quotes' || c === 'orders',
  money: (c) => c === 'payments' || c === 'invoices',
  follow: (c) => c === 'follow-ups' || c === 'meetings' || c === 'calls' || c === 'service'
};
const FEED_ICONS: Record<string, typeof Users> = {
  leads: UserPlus, customers: Users, deals: Handshake, quotes: FileText, orders: ShoppingCart, invoices: Receipt, payments: Banknote,
  'follow-ups': ClipboardList, meetings: CalendarClock, calls: Activity, service: AlertTriangle
};

function ActivityFeed({ d, base }: { d: Overview; base: string }) {
  const [filter, setFilter] = useState('all');
  const rows = useMemo(() => d.activity.filter((a) => feedMatch[filter]!(a.category)), [d.activity, filter]);
  return (
    <Card className="crm-rise flex min-h-0 flex-col p-5">
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><Activity className="size-4 text-primary" /> Recent activity</p>
        <span className="text-[11px] text-muted-foreground">{d.activity.length} latest</span>
      </div>
      <div className="mb-2 flex flex-wrap gap-1">
        {FEED_FILTERS.map((f) => (
          <button key={f.key} type="button" onClick={() => setFilter(f.key)}
            className={cn('rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors', filter === f.key ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground')}>
            {f.label}
          </button>
        ))}
      </div>
      {rows.length === 0 ? (
        <p className="py-8 text-center text-[13px] text-muted-foreground">Nothing yet.</p>
      ) : (
        <ol className="crm-scroll -mr-2 max-h-[340px] space-y-0.5 overflow-y-auto pr-2">
          {rows.slice(0, 20).map((a, i) => {
            const Icon = FEED_ICONS[a.category] ?? Activity;
            return (
              <li key={a.kind + a.id + i} className="crm-rise" style={{ animationDelay: `${Math.min(i, 10) * 30}ms` }}>
                <Link to={`${base}/${a.object}/${a.id}`} className="flex items-start gap-3 rounded-lg px-1.5 py-1.5 hover:bg-muted/60">
                  <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary"><Icon className="size-3.5" /></span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] text-foreground"><span className="font-medium">{a.actor}</span> <span className="text-muted-foreground">{verb(a)}</span> {a.title}</p>
                    <p className="text-[11px] text-muted-foreground">{ago(a.at)}</p>
                  </div>
                </Link>
              </li>
            );
          })}
        </ol>
      )}
    </Card>
  );
}

const NOUN: Record<string, string> = {
  leads: 'a lead', opportunities: 'a deal', quotes: 'a quote', sales_orders: 'an order', invoices: 'an invoice', payments: 'a payment',
  tasks: 'a follow-up', appointments: 'a meeting', events: 'a meeting', call_logs: 'a call', communications: 'a message', contacts: 'a contact',
  accounts: 'an account', cases: 'a case'
};

function verb(a: { kind: string; object: string }) {
  if (a.kind === 'record.converted') return 'converted';
  if (a.kind === 'file.uploaded') return 'added a document to';
  if (a.object === 'payments') return 'recorded';
  if (a.object === 'invoices') return 'generated';
  return `added ${NOUN[a.object] ?? 'a record'} ·`;
}

function ago(iso: string) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)}m ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

function whenText(v: string) {
  if (!v) return '';
  const d = new Date(v.length === 10 ? `${v}T00:00:00` : v);
  if (Number.isNaN(d.getTime())) return v;
  const today = new Date();
  const same = d.toDateString() === today.toDateString();
  const tomorrow = new Date(today.getTime() + 86400000).toDateString() === d.toDateString();
  const day = same ? 'Today' : tomorrow ? 'Tomorrow' : d.toLocaleDateString('en-IN', { weekday: 'short', day: 'numeric', month: 'short' });
  return v.length > 10 ? `${day}, ${d.toLocaleTimeString('en-IN', { hour: 'numeric', minute: '2-digit' })}` : day;
}

function Stat({ label, value, warn }: { label: string; value: number; warn?: boolean }) {
  return (
    <div>
      <p className="text-[11px] text-muted-foreground">{label}</p>
      <p className={cn('text-[15px] font-semibold tabular-nums', warn ? 'text-warning' : 'text-foreground')} title={formatMoney(value)}>
        <AnimatedNumber value={value} format={(n) => shortMoney(n)} />
      </p>
    </div>
  );
}

function OverviewSkeleton() {
  return (
    <div className="space-y-4" aria-busy="true">
      <div className="grid gap-4 xl:grid-cols-3">
        <Skeleton className="h-[340px] rounded-2xl xl:col-span-2" />
        <div className="grid gap-4">
          <Skeleton className="h-[170px] rounded-2xl" />
          <div className="grid grid-cols-2 gap-4">
            <Skeleton className="h-[150px] rounded-2xl" />
            <Skeleton className="h-[150px] rounded-2xl" />
          </div>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-[120px] rounded-2xl" />
        ))}
      </div>
    </div>
  );
}
