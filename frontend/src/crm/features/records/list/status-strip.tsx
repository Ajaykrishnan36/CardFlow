import { useQueries } from '@tanstack/react-query';
import type { ObjectKey, ObjectMeta, RecordListParams } from '@crm/api/types';
import type { FilterCondition, FilterGroup, FilterNode } from '@crm/api/types-features';
import { AnimatedNumber } from '@crm/components/charts';
import { cn } from '@crm/lib/utils';
import { useRecordScope } from '../record-scope';
import { recordKeys } from '../use-object-meta';

// The list's summary (D-177): how many records there are and how many in each status, each a
// chip that filters the list. The numbers come from the same list calls (and the same access
// rules) as the table, one count per status.

const TONE_DOT: Record<string, string> = { primary: 'bg-primary', success: 'bg-success', warning: 'bg-warning', danger: 'bg-danger', neutral: 'bg-muted-foreground/50' };

function isCond(n: FilterNode): n is FilterCondition {
  return 'field' in n && Boolean(n.field);
}

export function StatusStrip({
  object,
  meta,
  base,
  filter,
  onFilter,
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  /** The list's own params, without paging (search, workspace…). */
  base: RecordListParams;
  filter: FilterGroup | undefined;
  onFilter: (f: FilterGroup | undefined) => void;
}) {
  const scope = useRecordScope();
  const field = meta.statusField;
  const statuses = (meta.statuses ?? []).slice(0, 8);
  const others = (filter?.filters ?? []).filter((f) => !(isCond(f) && f.field === field));
  const othersJson = others.length ? JSON.stringify({ op: 'and', filters: others }) : undefined;
  const current = (filter?.filters ?? []).find((f): f is FilterCondition => isCond(f) && f.field === field && f.op === 'eq');
  const keys = ['__all', ...statuses.map((s) => s.value)];
  const results = useQueries({
    queries: keys.map((k) => {
      const filters: FilterNode[] = [...others, ...(k === '__all' ? [] : [{ field: field!, op: 'eq', value: k } as FilterCondition])];
      const p: RecordListParams = { ...base, limit: 1, offset: 0, filter: filters.length ? JSON.stringify({ op: 'and', filters }) : undefined };
      return { queryKey: [...recordKeys.list(scope.prefix, object, p), 'count'], queryFn: () => scope.api.list(object, p), staleTime: 30_000, enabled: Boolean(field) };
    })
  });
  if (!field || statuses.length === 0) return null;
  const count = (i: number) => results[i]?.data?.total;
  const pick = (value?: string) => onFilter(value ? { op: 'and', filters: [...others, { field, op: 'eq', value }] } : others.length ? { op: 'and', filters: others } : undefined);
  void othersJson;
  return (
    <div className="mb-4 flex gap-2 overflow-x-auto pb-1">
      <Chip label={`All ${meta.labelPlural.toLowerCase()}`} n={count(0)} active={!current} dot="bg-foreground" onClick={() => pick()} big />
      {statuses.map((s, i) => (
        <Chip key={s.value} label={s.label} n={count(i + 1)} active={current?.value === s.value} dot={TONE_DOT[s.tone ?? 'neutral'] ?? TONE_DOT.neutral!} onClick={() => pick(s.value)} />
      ))}
    </div>
  );
}

function Chip({ label, n, active, dot, onClick, big }: { label: string; n?: number; active: boolean; dot: string; onClick: () => void; big?: boolean }) {
  return (
    <button type="button" onClick={onClick}
      className={cn('crm-rise flex min-w-[120px] shrink-0 flex-col items-start rounded-2xl border px-4 py-2.5 text-left transition-[background-color,box-shadow,transform] hover:-translate-y-0.5 hover:shadow-lift',
        active ? 'border-transparent bg-sidebar-active text-sidebar-active-foreground shadow-lift' : 'bg-card shadow-card', big && 'min-w-[150px]')}>
      <span className={cn('flex items-center gap-1.5 text-[11px] font-medium', active ? 'opacity-85' : 'text-muted-foreground')}>
        <span className={cn('size-1.5 rounded-full', active ? 'bg-current' : dot)} /> {label}
      </span>
      <span className="text-[20px] font-bold leading-tight tabular-nums">{n === undefined ? '·' : <AnimatedNumber value={n} />}</span>
    </button>
  );
}
