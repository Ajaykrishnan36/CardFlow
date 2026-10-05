import { useState } from 'react';
import { arrange, type UILayout } from '../../../navigation/layout-items';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { businessApi, type BusinessDashboardSummary } from '@crm/api/endpoints';
import { Card } from '@crm/components/ui/card';
import { Input } from '@crm/components/ui/input';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { workspaceBase } from './workspace-context';

const ranges = [
  { key: 'today', label: 'Today' },
  { key: 'week', label: 'This week' },
  { key: 'month', label: 'This month' },
  { key: 'year', label: 'This year' },
  { key: 'all', label: 'All time' },
  { key: 'custom', label: 'Custom' }
] as const;

type RangeKey = (typeof ranges)[number]['key'];

export function money(amount: number, currency: string): string {
  try {
    return new Intl.NumberFormat('en-IN', { style: 'currency', currency: currency || 'INR', maximumFractionDigits: 0 }).format(amount);
  } catch {
    return `${currency} ${Math.round(amount).toLocaleString('en-IN')}`;
  }
}

/**
 * The numbers of one business for a date range (D-96): leads, pipeline, tasks, cases
 * and income / expenses / net. Only what the member may read is shown; nothing is
 * sample data. The same endpoint feeds the mobile Home screen.
 */
export function SummarySection({ code, layout }: { code: string; layout?: UILayout | null }) {
  const [range, setRange] = useState<RangeKey>('month');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const ready = range !== 'custom' || Boolean(from && to);
  const q = useQuery({
    queryKey: ['workspace', code, 'summary', range, from, to],
    queryFn: () => businessApi.summary(code, { range, from: range === 'custom' ? from : undefined, to: range === 'custom' ? to : undefined }),
    enabled: ready,
    staleTime: 30_000
  });
  const base = workspaceBase(code);
  const s: BusinessDashboardSummary | undefined = q.data;
  const can = (k: string) => Boolean(s?.can[`${k}.read`]);
  const m = (k: string) => s?.metrics[k] ?? 0;

  type Tile = { key: string; label: string; value: string; hint?: string; to?: string; show: boolean; tone?: 'good' | 'bad' };
  const allTiles: Tile[] = s
    ? ([
        { key: 'summary:activeLeads', label: 'Active leads', value: String(m('activeLeads')), hint: `${m('newLeads')} new · ${m('convertedLeads')} converted`, to: `${base}/leads`, show: can('leads') },
        { key: 'summary:followUps', label: 'Follow-ups due', value: String(m('followUpsToday')), hint: 'leads to call today', to: `${base}/leads`, show: can('leads') },
        { key: 'summary:contacts', label: 'Contacts', value: String(m('activeContacts')), hint: `${m('newContacts')} new`, to: `${base}/contacts`, show: can('contacts') },
        { key: 'summary:accounts', label: 'Accounts', value: String(m('activeAccounts')), hint: `${m('newAccounts')} new`, to: `${base}/accounts`, show: can('accounts') },
        { key: 'summary:openDeals', label: 'Open deals', value: String(m('openOpportunities')), hint: money(m('pipelineValue'), s.currency) + ' in pipeline', to: `${base}/opportunities`, show: can('opportunities') },
        { key: 'summary:won', label: 'Won', value: money(m('wonValue'), s.currency), hint: `${m('wonDeals')} deals · ${Math.round(m('winRate'))}% win rate`, to: `${base}/opportunities`, show: can('opportunities'), tone: 'good' },
        { key: 'summary:tasks', label: 'Tasks due today', value: String(m('tasksDueToday')), hint: `${m('overdueTasks')} overdue`, to: `${base}/tasks`, show: can('tasks'), tone: m('overdueTasks') > 0 ? 'bad' : undefined },
        { key: 'summary:cases', label: 'Open cases', value: String(m('openCases')), hint: `${m('newCases')} new`, to: `${base}/cases`, show: can('cases') },
        { key: 'summary:income', label: 'Income', value: money(s.finance?.income ?? 0, s.finance?.currency ?? s.currency), to: `${base}/income`, show: Boolean(s.finance), tone: 'good' },
        { key: 'summary:expenses', label: 'Expenses', value: money(s.finance?.expenses ?? 0, s.finance?.currency ?? s.currency), to: `${base}/expenses`, show: Boolean(s.finance) },
        {
          key: 'summary:net',
          label: 'Net income',
          value: money(s.finance?.net ?? 0, s.finance?.currency ?? s.currency),
          hint: 'income − expenses',
          show: Boolean(s.finance),
          tone: (s.finance?.net ?? 0) < 0 ? 'bad' : 'good'
        }
      ] as Tile[]).filter((t) => t.show)
    : [];
  const tiles = arrange(allTiles, (t) => t.key, layout);

  return (
    <section aria-label="Business summary" className="mb-6">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-[13px] font-semibold text-foreground">Business summary</h2>
        <div role="tablist" aria-label="Date range" className="flex flex-wrap gap-1 rounded-lg border bg-muted/60 p-1">
          {ranges.map((r) => (
            <button
              key={r.key}
              type="button"
              role="tab"
              aria-selected={range === r.key}
              onClick={() => setRange(r.key)}
              className={cn('rounded-md px-2.5 py-1 text-xs font-medium transition-colors', range === r.key ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
      {range === 'custom' ? (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
          <Input type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="w-40" aria-label="From" />
          <span className="text-muted-foreground">to</span>
          <Input type="date" value={to} min={from} onChange={(e) => setTo(e.target.value)} className="w-40" aria-label="To" />
        </div>
      ) : null}
      {!ready ? (
        <p className="text-sm text-muted-foreground">Choose a start and an end date.</p>
      ) : q.isError ? (
        <p className="text-sm text-danger">Couldn’t load the summary. {q.error instanceof Error ? q.error.message : ''}</p>
      ) : !s ? (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <Card key={i} className="space-y-2 p-4">
              <Skeleton className="h-3 w-20" />
              <Skeleton className="h-6 w-16" />
            </Card>
          ))}
        </div>
      ) : tiles.length === 0 ? null : (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4">
          {tiles.map((t) => {
            const body = (
              <>
                <p className="truncate text-[13px] font-medium text-muted-foreground">{t.label}</p>
                <p className={cn('mt-1.5 truncate text-[22px] font-semibold leading-none tracking-tight tabular-nums', t.tone === 'bad' ? 'text-danger' : t.tone === 'good' ? 'text-success' : 'text-foreground')}>
                  {t.value}
                </p>
                <p className="mt-2 h-4 truncate text-xs text-muted-foreground">{t.hint}</p>
              </>
            );
            return t.to ? (
              <Link key={t.label} to={t.to} className="block min-w-0 rounded-lg border bg-card p-4 shadow-card transition-colors hover:border-primary/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                {body}
              </Link>
            ) : (
              <Card key={t.label} className="min-w-0 p-4">
                {body}
              </Card>
            );
          })}
        </div>
      )}
    </section>
  );
}
