import { useQuery } from '@tanstack/react-query';
import { BarChart3, Crown, LineChart as LineIcon, Target, Trophy } from 'lucide-react';
import { enterpriseApi, type Forecast } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { AnimatedNumber, Gauge, LineChart, ProgressBar, Waterfall, shortMoney } from '@crm/components/charts';
import { cn } from '@crm/lib/utils';

// The forecast at a glance (D-176): the target as a gauge, the path to it (closed, then
// commit, best case and the rest of the pipeline stacked on top), who is selling against
// their own target, and closed won week by week with the pace it is on.

export function ForecastVisuals({ code, f }: { code: string; f: Forecast }) {
  const t = f.totals;
  const quota = t.quota;
  const pct = quota ? Math.min(100, (t.closed / quota) * 100) : 0;
  const trend = useQuery({
    queryKey: ['forecast-trend', code, f.period.from, f.period.to],
    queryFn: () => enterpriseApi(code).forecastTrend(f.period.from, f.period.to),
    staleTime: 60_000
  });
  const daysLeft = Math.max(0, Math.ceil((new Date(f.period.to).getTime() - Date.now()) / 86400000) + 1);
  const people = f.groupBy === 'owner' ? [...f.rows].filter((r) => r.id).sort((a, b) => b.closed - a.closed) : [];

  // Pace: the weekly average so far, carried to the end of the period (shown dashed, labelled).
  const weeks = trend.data?.weeks ?? [];
  const actual = weeks.map((w) => w.closed);
  const lastIdx = actual.reduce<number>((n, v, i) => (v !== null ? i : n), -1);
  const pace = lastIdx >= 0 && actual[lastIdx]! > 0 ? actual[lastIdx]! / (lastIdx + 1) : 0;
  const projected = weeks.map((_, i) => (i < lastIdx ? null : lastIdx >= 0 ? (actual[lastIdx] ?? 0) + pace * (i - lastIdx) : null));

  return (
    <div className="mb-4 space-y-4">
      <div className="grid gap-4 xl:grid-cols-[1fr_1.25fr]">
        <div className="crm-hero crm-rise relative overflow-hidden rounded-2xl p-5 shadow-lift">
          <div className="crm-hero-stars pointer-events-none absolute inset-0 opacity-50" aria-hidden />
          <div className="relative flex items-start justify-between gap-2">
            <div>
              <p className="flex items-center gap-2 text-[16px] font-semibold"><Target className="size-4 text-mint" /> Target reached</p>
              <p className="text-xs text-white/60">{f.period.label} · {daysLeft} days left</p>
            </div>
            {quota ? (
              <span className={cn('rounded-full px-2.5 py-0.5 text-[11px] font-semibold', t.forecast >= quota ? 'bg-mint/20 text-mint' : 'bg-warning/20 text-warning')}>
                {t.forecast >= quota ? 'Forecast covers the target' : 'Forecast below target'}
              </span>
            ) : null}
          </div>
          <div className="relative mt-2 grid place-items-center">
            <Gauge value={pct} semi size={300} thickness={16} marker={quota && t.forecast ? Math.min(100, (t.forecast / quota) * 100) : undefined}>
              <div className="pt-6">
                <p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-white/60">Closed won</p>
                <p className="text-[36px] font-bold leading-none text-white"><AnimatedNumber value={t.closed} format={(n) => shortMoney(n)} /></p>
                <p className="mt-1 text-xs text-mint">
                  {quota ? <><AnimatedNumber value={pct} format={(n) => `${Math.round(n)}%`} /> of {shortMoney(quota)} · {t.wonDeals} deals</> : `${t.wonDeals} deals · no target set`}
                </p>
              </div>
            </Gauge>
          </div>
          <div className="relative mt-2 grid grid-cols-3 gap-2">
            <HeroTile label="Commit" dot="bg-chart-1" value={t.forecast} sub={quota ? `${Math.round((t.forecast / quota) * 100)}% of target` : 'closed + commit'} />
            <HeroTile label="Best case" dot="bg-chart-2" value={t.bestCaseTotal} sub={quota ? `${Math.round((t.bestCaseTotal / quota) * 100)}% of target` : 'if these close too'} />
            <HeroTile label={quota ? 'Gap to target' : 'Open pipeline'} dot="bg-white/40" value={quota ? Math.max(0, t.gap) : t.openPipeline} sub={quota ? `${Math.round((Math.max(0, t.gap) / quota) * 100)}% of target` : `${t.openDeals} open deals`} />
          </div>
        </div>
        <Card className="crm-rise p-5">
          <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><BarChart3 className="size-4 text-primary" /> Path to target</p>
          <p className="mb-2 text-xs text-muted-foreground">Closed, then commit, best case and the rest of the pipeline stacked on top</p>
          <Waterfall
            format={(n) => shortMoney(n)}
            reference={quota ? { value: quota, label: `Target ${shortMoney(quota)}` } : undefined}
            steps={[
              { key: 'closed', label: 'Closed won', sub: `${t.wonDeals} deals`, from: 0, to: t.closed, color: 'hsl(var(--chart-1))' },
              { key: 'commit', label: 'Commit', sub: `+${shortMoney(t.commit)}`, from: t.closed, to: t.forecast, color: 'hsl(var(--chart-1))', striped: true },
              { key: 'best', label: 'Best case', sub: `+${shortMoney(t.bestCase)}`, from: t.forecast, to: t.bestCaseTotal, color: 'hsl(var(--chart-2))' },
              { key: 'pipe', label: 'Pipeline', sub: `+${shortMoney(t.pipeline)} · ${t.openDeals} open`, from: t.bestCaseTotal, to: t.bestCaseTotal + t.pipeline, color: 'hsl(var(--muted))', ghost: true }
            ]}
          />
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-[1fr_1.25fr]">
        <Card className="crm-rise p-5">
          <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><Trophy className="size-4 text-primary" /> Leaderboard</p>
          <p className="mb-3 text-xs text-muted-foreground">Closed won against each person's own target</p>
          {people.length === 0 ? (
            <p className="py-8 text-center text-[13px] text-muted-foreground">{f.groupBy === 'owner' ? 'No deals closing in this period yet.' : 'Shown when grouped by person.'}</p>
          ) : (
            <ul className="space-y-3.5">
              {people.slice(0, 8).map((r, i) => {
                const reach = r.quota ? (r.closed / r.quota) * 100 : 0;
                return (
                  <li key={r.id} className={cn('flex items-center gap-3 rounded-xl px-2 py-1.5', i === 0 && 'bg-primary-soft/60')}>
                    <span className="w-4 text-center text-xs font-semibold text-muted-foreground">{i + 1}</span>
                    <span className={cn('relative grid size-9 shrink-0 place-items-center rounded-full text-[11px] font-bold', i === 0 ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground')}>
                      {r.name.split(' ').map((w) => w[0]).slice(0, 2).join('')}
                      {i === 0 ? <Crown className="absolute -right-1 -top-1.5 size-3.5 text-warning" /> : null}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-2 text-[13px]">
                        <span className="truncate font-medium text-foreground">{r.name}{r.id === f.me ? <span className="text-muted-foreground"> (you)</span> : null}</span>
                        <span className="shrink-0 tabular-nums text-muted-foreground">{shortMoney(r.closed)}{r.quota ? ` / ${shortMoney(r.quota)}` : ''}</span>
                      </div>
                      <ProgressBar value={r.closed} max={r.quota || Math.max(1, people[0]!.closed)} className="mt-1.5" marker={r.quota ? Math.min(100, (r.forecast / r.quota) * 100) : undefined} />
                    </div>
                    <span className={cn('w-12 text-right text-[13px] font-bold tabular-nums', !r.quota ? 'text-muted-foreground' : reach >= 100 ? 'text-success' : reach >= 70 ? 'text-foreground' : 'text-warning')}>
                      {r.quota ? `${Math.round(reach)}%` : '—'}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>
        <Card className="crm-rise p-5">
          <div className="mb-2 flex flex-wrap items-start justify-between gap-2">
            <div>
              <p className="flex items-center gap-2 text-[15px] font-semibold text-foreground"><LineIcon className="size-4 text-primary" /> Weekly trend</p>
              <p className="text-xs text-muted-foreground">Closed won week by week; dashed = at this pace</p>
            </div>
            <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
              <span className="flex items-center gap-1.5"><span className="size-2 rounded-full bg-chart-1" /> Closed won</span>
              <span className="flex items-center gap-1.5"><span className="h-0.5 w-3 bg-chart-2" /> Best case today</span>
              <span className="flex items-center gap-1.5"><span className="h-0.5 w-3 bg-chart-3" /> Commit today</span>
            </div>
          </div>
          {trend.isPending ? (
            <div className="h-[220px] animate-pulse rounded-xl bg-muted" />
          ) : weeks.length === 0 ? (
            <p className="py-8 text-center text-[13px] text-muted-foreground">No weeks in this period.</p>
          ) : (
            <LineChart
              labels={weeks.map((w) => w.label)}
              format={(n) => shortMoney(n)}
              reference={quota ? { value: quota, label: `Target ${shortMoney(quota)}` } : undefined}
              series={[
                { key: 'best', label: 'Best case today', color: 'hsl(var(--chart-2))', points: weeks.map(() => t.bestCaseTotal) },
                { key: 'commit', label: 'Commit today', color: 'hsl(var(--chart-3))', points: weeks.map(() => t.forecast) },
                { key: 'pace', label: 'At this pace', color: 'hsl(var(--chart-1))', points: projected, dashedFrom: Math.max(0, lastIdx) },
                { key: 'closed', label: 'Closed won', color: 'hsl(var(--chart-1))', points: actual, area: true }
              ]}
            />
          )}
        </Card>
      </div>
    </div>
  );
}

function HeroTile({ label, value, sub, dot }: { label: string; value: number; sub: string; dot: string }) {
  return (
    <div className="rounded-xl border border-white/10 bg-white/5 px-3 py-2.5">
      <p className="flex items-center gap-1.5 text-[11px] text-white/70"><span className={cn('size-1.5 rounded-full', dot)} />{label}</p>
      <p className="text-[18px] font-bold text-white"><AnimatedNumber value={value} format={(n) => shortMoney(n)} /></p>
      <p className="text-[10px] text-white/50">{sub}</p>
    </div>
  );
}
