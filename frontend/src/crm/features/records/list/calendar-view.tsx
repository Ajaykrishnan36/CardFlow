import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight, Plus } from 'lucide-react';
import type { FieldDef, ObjectKey, ObjectMeta, RecordListParams, RecordRow } from '@crm/api/types';
import type { FilterGroup } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { SegmentedFilter } from '@crm/components/page';
import { Spinner } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { recordKeys, statusOption } from '../use-object-meta';
import { useRecordScope } from '../record-scope';
import { rowHref } from './record-table';
import { withCondition } from './filter-utils';

type Mode = 'month' | 'week' | 'day';

const pad = (n: number) => String(n).padStart(2, '0');
const ymd = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const addDays = (d: Date, n: number) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
const startOfWeek = (d: Date) => addDays(d, -((d.getDay() + 6) % 7)); // Monday first

function rangeFor(mode: Mode, anchor: Date): [Date, Date] {
  if (mode === 'day') return [anchor, anchor];
  if (mode === 'week') {
    const s = startOfWeek(anchor);
    return [s, addDays(s, 6)];
  }
  const first = new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  const s = startOfWeek(first);
  return [s, addDays(s, 41)];
}

function dayOf(v: unknown, f: FieldDef): string | null {
  if (typeof v !== 'string' || !v) return null;
  if (f.type === 'date') return v.slice(0, 10);
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? null : ymd(d);
}

function timeOf(v: unknown, f: FieldDef): string {
  if (f.type !== 'datetime' || typeof v !== 'string') return '';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString('en-IN', { hour: 'numeric', minute: '2-digit' });
}

export function CalendarView({
  object,
  meta,
  dateField,
  initialMode,
  params,
  filter,
  onModeChange,
  onNew
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  dateField: FieldDef;
  initialMode?: Mode;
  params: RecordListParams;
  filter?: FilterGroup;
  onModeChange?: (m: Mode) => void;
  onNew?: (values: Record<string, unknown>) => void;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const [mode, setMode] = useState<Mode>(initialMode ?? 'month');
  const [anchor, setAnchor] = useState(() => new Date());
  const [from, to] = rangeFor(mode, anchor);
  const f = withCondition(filter, { field: dateField.key, op: 'between', value: [ymd(from), ymd(to)] });
  const listParams: RecordListParams = { ...params, filter: JSON.stringify(f), sorts: JSON.stringify([{ field: dateField.key, dir: 'asc' }]), limit: 200, offset: 0 };
  const q = useQuery({ queryKey: recordKeys.list(scope.prefix, object, listParams), queryFn: () => scope.api.list(object, listParams) });
  const byDay = useMemo(() => {
    const m = new Map<string, RecordRow[]>();
    for (const r of q.data?.data ?? []) {
      const d = dayOf(r.values[dateField.key], dateField);
      if (!d) continue;
      m.set(d, [...(m.get(d) ?? []), r]);
    }
    return m;
  }, [q.data, dateField]);
  const step = (n: number) =>
    setAnchor((a) => (mode === 'month' ? new Date(a.getFullYear(), a.getMonth() + n, 1) : addDays(a, n * (mode === 'week' ? 7 : 1))));
  const title =
    mode === 'month'
      ? anchor.toLocaleDateString('en-IN', { month: 'long', year: 'numeric' })
      : mode === 'week'
        ? `${from.toLocaleDateString('en-IN', { day: 'numeric', month: 'short' })} – ${to.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })}`
        : anchor.toLocaleDateString('en-IN', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
  const today = ymd(new Date());
  const days: Date[] = [];
  for (let d = from; d <= to; d = addDays(d, 1)) days.push(d);
  const weekdays = Array.from({ length: 7 }, (_, i) => addDays(startOfWeek(new Date()), i).toLocaleDateString('en-IN', { weekday: 'short' }));
  const newAt = (d: Date) => onNew?.({ [dateField.key]: dateField.type === 'date' ? ymd(d) : new Date(d.getFullYear(), d.getMonth(), d.getDate(), 10).toISOString() });

  const item = (r: RecordRow) => {
    const s = meta.statusField ? statusOption(meta, r.values[meta.statusField]) : undefined;
    return (
      <Link
        key={r.id}
        to={rowHref(scope, object, r)}
        className={cn(
          'block truncate rounded px-1.5 py-0.5 text-[11px] font-medium hover:underline',
          s?.tone === 'success' ? 'bg-success-soft text-success' : s?.tone === 'danger' ? 'bg-danger-soft text-danger' : s?.tone === 'warning' ? 'bg-warning-soft text-warning' : 'bg-primary-soft text-primary'
        )}
        title={r.title}
      >
        {timeOf(r.values[dateField.key], dateField) ? <span className="mr-1 tabular-nums opacity-80">{timeOf(r.values[dateField.key], dateField)}</span> : null}
        {r.title || t('records.common.untitled')}
      </Link>
    );
  };

  return (
    <div className="p-3">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onClick={() => setAnchor(new Date())}>
          {t('lists.calendar.today')}
        </Button>
        <Button variant="outline" size="icon-sm" onClick={() => step(-1)} aria-label={t('lists.calendar.prev')}>
          <ChevronLeft />
        </Button>
        <Button variant="outline" size="icon-sm" onClick={() => step(1)} aria-label={t('lists.calendar.next')}>
          <ChevronRight />
        </Button>
        <h3 className="text-[15px] font-semibold">{title}</h3>
        {q.isFetching ? <Spinner className="size-4" /> : null}
        <div className="ml-auto">
          <SegmentedFilter<Mode>
            value={mode}
            onChange={(m) => {
              setMode(m);
              onModeChange?.(m);
            }}
            options={[
              { value: 'month', label: t('lists.calendar.month') },
              { value: 'week', label: t('lists.calendar.week') },
              { value: 'day', label: t('lists.calendar.day') }
            ]}
          />
        </div>
      </div>
      {mode === 'day' ? (
        <div className="space-y-1.5 rounded-lg border p-3">
          {(byDay.get(ymd(anchor)) ?? []).map((r) => item(r))}
          {!(byDay.get(ymd(anchor)) ?? []).length ? <p className="py-8 text-center text-[13px] text-muted-foreground">{t('lists.calendar.nothing')}</p> : null}
          {onNew ? (
            <Button variant="ghost" size="sm" onClick={() => newAt(anchor)}>
              <Plus /> {t('lists.calendar.addOn', { date: anchor.toLocaleDateString('en-IN', { day: 'numeric', month: 'short' }) })}
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <div className="grid min-w-[720px] grid-cols-7 overflow-hidden rounded-lg border">
            {weekdays.map((w) => (
              <div key={w} className="border-b bg-muted/40 px-2 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                {w}
              </div>
            ))}
            {days.map((d) => {
              const key = ymd(d);
              const list = byDay.get(key) ?? [];
              const outside = mode === 'month' && d.getMonth() !== anchor.getMonth();
              return (
                <div key={key} className={cn('group relative border-b border-r p-1.5 last:border-r-0', mode === 'week' ? 'min-h-[320px]' : 'min-h-[104px]', outside && 'bg-muted/20')}>
                  <div className="mb-1 flex items-center justify-between">
                    <button
                      type="button"
                      onClick={() => {
                        setAnchor(d);
                        setMode('day');
                      }}
                      className={cn('grid size-6 place-items-center rounded-full text-xs tabular-nums hover:bg-muted', key === today && 'bg-primary font-semibold text-primary-foreground hover:bg-primary', outside && 'text-muted-foreground/60')}
                    >
                      {d.getDate()}
                    </button>
                    {onNew ? (
                      <button type="button" onClick={() => newAt(d)} className="grid size-5 place-items-center rounded text-muted-foreground opacity-0 hover:bg-muted group-hover:opacity-100 focus-visible:opacity-100"
                        aria-label={t('lists.calendar.addOn', { date: d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short' }) })}>
                        <Plus className="size-3.5" />
                      </button>
                    ) : null}
                  </div>
                  <div className="space-y-0.5">
                    {list.slice(0, mode === 'week' ? 20 : 3).map((r) => item(r))}
                    {mode === 'month' && list.length > 3 ? (
                      <button type="button" className="px-1.5 text-[11px] font-medium text-muted-foreground hover:text-foreground" onClick={() => { setAnchor(d); setMode('day'); }}>
                        {t('lists.calendar.more', { count: list.length - 3 })}
                      </button>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
