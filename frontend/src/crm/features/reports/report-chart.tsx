import { useTranslation } from 'react-i18next';
import type { ReportChart as ChartType, ReportResult } from '@crm/api/types';
import { cn } from '@crm/lib/utils';

// Dependency-free charts for reports (D-50): plain SVG/HTML using the theme colours.

const PALETTE = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#a855f7', '#ec4899', '#84cc16', '#f97316', '#64748b'];

export function formatValue(v: number, currency?: boolean) {
  if (currency) return `₹${v.toLocaleString('en-IN', { maximumFractionDigits: 0 })}`;
  return Number.isInteger(v) ? v.toLocaleString('en-IN') : v.toLocaleString('en-IN', { maximumFractionDigits: 2 });
}

export function ReportChart({ result, chart, compact }: { result: ReportResult; chart?: ChartType; compact?: boolean }) {
  const { t } = useTranslation();
  const kind = chart ?? result.chart;
  const rows = result.rows;
  if (kind === 'number' || !result.groupLabel) {
    return (
      <div className={cn('flex flex-col justify-center', compact ? 'py-2' : 'py-6')}>
        <p className={cn('font-semibold tabular-nums tracking-tight text-foreground', compact ? 'text-3xl' : 'text-5xl')}>{formatValue(result.total, result.currency)}</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {result.measureLabel} · {t('reports.recordsCount', { count: result.count })}
        </p>
      </div>
    );
  }
  if (rows.length === 0) return <p className="py-8 text-center text-[13px] text-muted-foreground">{t('reports.noData')}</p>;
  if (kind === 'table') return <ResultTable result={result} />;
  if (kind === 'donut') return <Donut result={result} compact={compact} />;
  if (kind === 'line') return <Line result={result} compact={compact} />;
  return <Bars result={result} compact={compact} />;
}

function Bars({ result, compact }: { result: ReportResult; compact?: boolean }) {
  const max = Math.max(...result.rows.map((r) => r.value), 1);
  const rows = compact ? result.rows.slice(0, 8) : result.rows;
  return (
    <ul className="space-y-2">
      {rows.map((r, i) => (
        <li key={r.key + i} className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)_auto] items-center gap-2 text-xs">
          <span className="truncate text-muted-foreground" title={r.label}>
            {r.label}
          </span>
          <span className="h-5 overflow-hidden rounded bg-muted">
            <span className="block h-full rounded bg-primary transition-[width] duration-500" style={{ width: `${Math.max(2, (r.value / max) * 100)}%` }} />
          </span>
          <span className="w-16 text-right font-medium tabular-nums text-foreground">{formatValue(r.value, result.currency)}</span>
        </li>
      ))}
    </ul>
  );
}

function Line({ result, compact }: { result: ReportResult; compact?: boolean }) {
  const w = 600;
  const h = compact ? 150 : 220;
  const pad = 28;
  const rows = result.rows;
  const max = Math.max(...rows.map((r) => r.value), 1);
  const x = (i: number) => pad + (rows.length === 1 ? (w - 2 * pad) / 2 : (i * (w - 2 * pad)) / (rows.length - 1));
  const y = (v: number) => h - pad - (v / max) * (h - 2 * pad);
  const points = rows.map((r, i) => `${x(i)},${y(r.value)}`).join(' ');
  const every = Math.max(1, Math.ceil(rows.length / 8));
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="w-full" role="img" aria-label={`${result.measureLabel} by ${result.groupLabel}`}>
      {[0, 0.5, 1].map((f) => (
        <g key={f}>
          <line x1={pad} x2={w - pad} y1={y(max * f)} y2={y(max * f)} className="stroke-border" strokeDasharray="3 3" />
          <text x={4} y={y(max * f) + 4} className="fill-muted-foreground" fontSize="10">
            {formatValue(Math.round(max * f), result.currency)}
          </text>
        </g>
      ))}
      <polygon points={`${x(0)},${h - pad} ${points} ${x(rows.length - 1)},${h - pad}`} className="fill-primary/10" />
      <polyline points={points} fill="none" className="stroke-primary" strokeWidth="2.5" strokeLinejoin="round" strokeLinecap="round" />
      {rows.map((r, i) => (
        <g key={r.key + i}>
          <circle cx={x(i)} cy={y(r.value)} r="3.5" className="fill-background stroke-primary" strokeWidth="2">
            <title>{`${r.label}: ${formatValue(r.value, result.currency)}`}</title>
          </circle>
          {i % every === 0 ? (
            <text x={x(i)} y={h - 8} textAnchor="middle" className="fill-muted-foreground" fontSize="10">
              {r.label.length > 10 ? `${r.label.slice(0, 9)}…` : r.label}
            </text>
          ) : null}
        </g>
      ))}
    </svg>
  );
}

function Donut({ result, compact }: { result: ReportResult; compact?: boolean }) {
  const top = result.rows.slice(0, 9);
  const rest = result.rows.slice(9).reduce((n, r) => n + r.value, 0);
  const slices = rest > 0 ? [...top, { key: '_other', label: 'Other', value: rest, count: 0 }] : top;
  const total = slices.reduce((n, r) => n + r.value, 0) || 1;
  const r = 60;
  const c = 2 * Math.PI * r;
  let offset = 0;
  return (
    <div className={cn('flex flex-col items-center gap-4 sm:flex-row', compact && 'gap-3')}>
      <svg viewBox="0 0 160 160" className={cn('shrink-0', compact ? 'size-28' : 'size-40')} role="img" aria-label={result.groupLabel}>
        <circle cx="80" cy="80" r={r} fill="none" className="stroke-muted" strokeWidth="22" />
        {slices.map((s, i) => {
          const len = (s.value / total) * c;
          const el = (
            <circle
              key={s.key + i}
              cx="80"
              cy="80"
              r={r}
              fill="none"
              stroke={PALETTE[i % PALETTE.length]}
              strokeWidth="22"
              strokeDasharray={`${len} ${c - len}`}
              strokeDashoffset={-offset}
              transform="rotate(-90 80 80)"
            >
              <title>{`${s.label}: ${formatValue(s.value, result.currency)}`}</title>
            </circle>
          );
          offset += len;
          return el;
        })}
        <text x="80" y="78" textAnchor="middle" className="fill-foreground" fontSize="18" fontWeight="600">
          {formatValue(result.total, result.currency)}
        </text>
        <text x="80" y="96" textAnchor="middle" className="fill-muted-foreground" fontSize="9">
          {result.measureLabel}
        </text>
      </svg>
      <ul className="w-full min-w-0 space-y-1 text-xs">
        {slices.map((s, i) => (
          <li key={s.key + i} className="flex items-center gap-2">
            <span className="size-2.5 shrink-0 rounded-sm" style={{ background: PALETTE[i % PALETTE.length] }} />
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{s.label}</span>
            <span className="font-medium tabular-nums">{formatValue(s.value, result.currency)}</span>
            <span className="w-10 text-right tabular-nums text-muted-foreground">{Math.round((s.value / total) * 100)}%</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function ResultTable({ result }: { result: ReportResult }) {
  const { t } = useTranslation();
  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-left text-xs">
        <thead className="bg-muted/50 text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">{result.groupLabel ?? t('reports.group')}</th>
            <th className="px-3 py-2 text-right font-medium">{t('reports.records')}</th>
            <th className="px-3 py-2 text-right font-medium">{result.measureLabel}</th>
          </tr>
        </thead>
        <tbody className="divide-y">
          {result.rows.map((r, i) => (
            <tr key={r.key + i}>
              <td className="px-3 py-1.5">{r.label}</td>
              <td className="px-3 py-1.5 text-right tabular-nums text-muted-foreground">{r.count}</td>
              <td className="px-3 py-1.5 text-right font-medium tabular-nums">{formatValue(r.value, result.currency)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot className="border-t bg-muted/30 font-medium">
          <tr>
            <td className="px-3 py-2">{t('reports.total')}</td>
            <td className="px-3 py-2 text-right tabular-nums">{result.count}</td>
            <td className="px-3 py-2 text-right tabular-nums">{formatValue(result.total, result.currency)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
}
