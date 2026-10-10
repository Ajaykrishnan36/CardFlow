import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Download, Users } from 'lucide-react';
import { businessApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { money } from './summary-section';

const ranges = [
  { key: 'week', label: 'This week' },
  { key: 'month', label: 'This month' },
  { key: 'year', label: 'This year' },
  { key: 'all', label: 'All time' }
] as const;
type RangeKey = (typeof ranges)[number]['key'];

const PALETTE = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#a855f7', '#ec4899', '#84cc16', '#f97316', '#64748b'];

/** Saves rows as a CSV file (opens in Excel and Google Sheets). */
export function downloadCsv(name: string, rows: Array<Array<string | number>>) {
  const cell = (v: string | number) => {
    const s = String(v);
    return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  const blob = new Blob(['﻿' + rows.map((r) => r.map(cell).join(',')).join('\n')], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

/**
 * Team performance for people who see everyone's records (D-137): who added how many
 * leads, how many they converted, and how that moved over time. Hidden for anyone else.
 */
export function TeamSection({ code }: { code: string }) {
  const [range, setRange] = useState<RangeKey>('month');
  const q = useQuery({ queryKey: ['dashboard-team', code, range], queryFn: () => businessApi.team(code, { range }), retry: false });
  if (q.isError && isApiError(q.error) && q.error.status === 403) return null;
  const d = q.data;
  const exportCsv = () => {
    if (!d) return;
    downloadCsv(`team-performance-${d.range.key}.csv`, [
      ['Person', 'Role', 'Leads added', 'Leads converted', 'Conversion %', 'Contacts added', 'Accounts added', 'Deals won', 'Won value', ...d.buckets.map((b) => `Leads ${b.label}`)],
      ...d.members.map((m) => [m.name, m.role, m.leadsAdded, m.leadsConverted, m.leadsAdded ? Math.round((m.leadsConverted * 100) / m.leadsAdded) : 0, m.contactsAdded, m.accountsAdded, m.dealsWon, m.wonValue, ...m.leads])
    ]);
  };
  return (
    <section aria-label="Team performance">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
          <Users className="size-4 text-muted-foreground" aria-hidden /> Team performance
        </h2>
        <div className="flex items-center gap-2">
          <div className="flex rounded-lg border bg-muted/40 p-0.5" role="tablist" aria-label="Period">
            {ranges.map((r) => (
              <button key={r.key} type="button" role="tab" aria-selected={range === r.key} onClick={() => setRange(r.key)}
                className={cn('rounded-md px-2.5 py-1 text-xs font-medium', range === r.key ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}>
                {r.label}
              </button>
            ))}
          </div>
          <Button variant="outline" size="sm" onClick={exportCsv} disabled={!d}>
            <Download /> Download CSV
          </Button>
        </div>
      </div>
      {!d ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <div className="grid gap-3 xl:grid-cols-2">
          <Card className="p-4">
            <p className="text-[13px] font-medium text-foreground">Leads added per {d.bucket}</p>
            <p className="mb-2 text-xs text-muted-foreground">Everyone together · {d.buckets.reduce((n, b) => n + b.leads, 0)} leads</p>
            <BarChart labels={d.buckets.map((b) => b.label)} values={d.buckets.map((b) => b.leads)} />
          </Card>
          <Card className="p-4">
            <p className="text-[13px] font-medium text-foreground">Leads added by each person</p>
            <p className="mb-2 text-xs text-muted-foreground">One line per person, running total over the period</p>
            <LineChart labels={d.buckets.map((b) => b.label)} series={d.members.filter((m) => m.leadsAdded > 0).slice(0, 10).map((m) => ({ name: m.name, values: cumulative(m.leads) }))} />
          </Card>
          <Card className="overflow-x-auto p-0 xl:col-span-2">
            <table className="w-full text-[13px]">
              <thead>
                <tr className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                  <th className="px-4 py-2 font-medium">Person</th>
                  <th className="px-3 py-2 text-right font-medium">Leads added</th>
                  <th className="px-3 py-2 text-right font-medium">Converted</th>
                  <th className="px-3 py-2 text-right font-medium">Conversion</th>
                  <th className="px-3 py-2 text-right font-medium">Contacts added</th>
                  <th className="px-3 py-2 text-right font-medium">Accounts added</th>
                  <th className="px-3 py-2 text-right font-medium">Deals won</th>
                  <th className="px-4 py-2 text-right font-medium">Won value</th>
                </tr>
              </thead>
              <tbody>
                {d.members.length === 0 ? (
                  <tr><td colSpan={8} className="px-4 py-6 text-center text-muted-foreground">Nobody has added anything in this period.</td></tr>
                ) : d.members.map((m, i) => (
                  <tr key={m.id} className="border-b last:border-0">
                    <td className="px-4 py-2">
                      <span className="mr-2 inline-block size-2 rounded-full align-middle" style={{ background: PALETTE[i % PALETTE.length] }} aria-hidden />
                      <span className="font-medium text-foreground">{m.name}</span>
                      <span className="ml-2 text-xs text-muted-foreground">{m.role}</span>
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.leadsAdded}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.leadsConverted}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.leadsAdded ? `${Math.round((m.leadsConverted * 100) / m.leadsAdded)}%` : '—'}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.contactsAdded}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.accountsAdded}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{m.dealsWon}</td>
                    <td className="px-4 py-2 text-right tabular-nums">{m.wonValue ? money(m.wonValue, d.currency) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </div>
      )}
    </section>
  );
}

const cumulative = (v: number[]) => v.reduce<number[]>((acc, n) => [...acc, (acc[acc.length - 1] ?? 0) + n], []);

// Dependency-free SVG charts, in the theme colours (the same approach as report charts).
const W = 600, H = 200, PAD = { l: 30, r: 10, t: 10, b: 26 };
const ticks = (max: number) => { const step = Math.max(1, Math.ceil(max / 4)); return Array.from({ length: Math.floor(max / step) + 1 }, (_, i) => i * step); };
const everyNth = (n: number) => Math.max(1, Math.ceil(n / 8));

function Axis({ max, labels }: { max: number; labels: string[] }) {
  const y = (v: number) => PAD.t + (H - PAD.t - PAD.b) * (1 - v / max);
  const step = (W - PAD.l - PAD.r) / Math.max(labels.length, 1);
  return (
    <g className="text-muted-foreground" fontSize="10" fill="currentColor">
      {ticks(max).map((v) => (
        <g key={v}>
          <line x1={PAD.l} x2={W - PAD.r} y1={y(v)} y2={y(v)} stroke="currentColor" strokeOpacity="0.15" />
          <text x={PAD.l - 6} y={y(v) + 3} textAnchor="end">{v}</text>
        </g>
      ))}
      {labels.map((l, i) => (i % everyNth(labels.length) === 0 ? <text key={i} x={PAD.l + step * (i + 0.5)} y={H - 8} textAnchor="middle">{l}</text> : null))}
    </g>
  );
}

function BarChart({ labels, values }: { labels: string[]; values: number[] }) {
  const max = Math.max(...values, 1);
  const step = (W - PAD.l - PAD.r) / Math.max(values.length, 1);
  const h = (v: number) => (H - PAD.t - PAD.b) * (v / max);
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Leads added over time">
      <Axis max={max} labels={labels} />
      {values.map((v, i) => (
        <rect key={i} x={PAD.l + step * i + step * 0.15} y={H - PAD.b - h(v)} width={Math.max(1, step * 0.7)} height={h(v)} rx="2" fill="#6366f1">
          <title>{`${labels[i]}: ${v}`}</title>
        </rect>
      ))}
    </svg>
  );
}

function LineChart({ labels, series }: { labels: string[]; series: Array<{ name: string; values: number[] }> }) {
  const max = Math.max(...series.flatMap((s) => s.values), 1);
  const step = (W - PAD.l - PAD.r) / Math.max(labels.length, 1);
  const x = (i: number) => PAD.l + step * (i + 0.5);
  const y = (v: number) => PAD.t + (H - PAD.t - PAD.b) * (1 - v / max);
  if (series.length === 0) return <p className="py-10 text-center text-[13px] text-muted-foreground">No leads were added in this period.</p>;
  return (
    <div>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Leads added by each person">
        <Axis max={max} labels={labels} />
        {series.map((s, k) => (
          <g key={s.name + k}>
            <polyline fill="none" stroke={PALETTE[k % PALETTE.length]} strokeWidth="2" strokeLinejoin="round" points={s.values.map((v, i) => `${x(i)},${y(v)}`).join(' ')} />
            {s.values.length <= 31 ? s.values.map((v, i) => <circle key={i} cx={x(i)} cy={y(v)} r="2.5" fill={PALETTE[k % PALETTE.length]}><title>{`${s.name} · ${labels[i]}: ${v}`}</title></circle>) : null}
          </g>
        ))}
      </svg>
      <ul className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {series.map((s, k) => (
          <li key={s.name + k} className="flex items-center gap-1.5"><span className="inline-block size-2 rounded-full" style={{ background: PALETTE[k % PALETTE.length] }} aria-hidden />{s.name}</li>
        ))}
      </ul>
    </div>
  );
}
