import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Plus } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta, RecordListParams, RecordRow } from '@crm/api/types';
import type { FilterGroup, RecordGroup } from '@crm/api/types-features';
import { Badge } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { FieldValue } from '../field-value';
import { recordKeys } from '../use-object-meta';
import { formatMoney } from '@crm/lib/money';
import { useRecordScope } from '../record-scope';
import { rowHref } from './record-table';
import { withCondition } from './filter-utils';

const toneBar: Record<string, string> = {
  neutral: 'bg-muted-foreground/40',
  primary: 'bg-primary',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger'
};

/** Records as cards in columns, one per value of a pick-list (stage, status…). */
export function KanbanBoard({
  object,
  meta,
  groupField,
  cardFields,
  params,
  filter,
  amountField,
  onNew
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  groupField: FieldDef;
  cardFields: FieldDef[];
  params: RecordListParams;
  filter?: FilterGroup;
  amountField?: FieldDef;
  onNew?: (values: Record<string, unknown>) => void;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const [dragging, setDragging] = useState<RecordRow | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const [extra, setExtra] = useState<Record<string, RecordRow[]>>({});
  const q = useQuery({
    queryKey: [...recordKeys.lists(scope.prefix, object), 'groups', groupField.key, params],
    queryFn: () => scope.api.groups(object, { ...params, field: groupField.key, limit: 25 })
  });
  const canEdit = scope.can(object, 'update') && !groupField.readOnly;
  const move = useMutation({
    mutationFn: ({ row, value }: { row: RecordRow; value: string }) =>
      scope.api.update(object, row.id, { [groupField.key]: value === '' ? null : groupField.type === 'boolean' ? value === 'true' : groupField.type === 'rating' ? Number(value) : value }, row.version),
    onMutate: async ({ row, value }) => {
      const key = [...recordKeys.lists(scope.prefix, object), 'groups', groupField.key, params];
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<{ field: string; groups: RecordGroup[] }>(key);
      if (prev) {
        qc.setQueryData(key, {
          ...prev,
          groups: prev.groups.map((g) => {
            const has = g.rows.some((r) => r.id === row.id);
            if (g.value === value) return { ...g, count: g.count + (has ? 0 : 1), rows: has ? g.rows : [{ ...row, values: { ...row.values, [groupField.key]: value } }, ...g.rows] };
            if (has) return { ...g, count: g.count - 1, rows: g.rows.filter((r) => r.id !== row.id) };
            return g;
          })
        });
      }
      return { prev, key };
    },
    onError: (e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
      toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'));
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) })
  });
  const loadMore = async (g: RecordGroup) => {
    const have = g.rows.length + (extra[g.value]?.length ?? 0);
    const cond = g.value === '' ? { field: groupField.key, op: 'empty' as const } : { field: groupField.key, op: groupField.type === 'select' ? ('in' as const) : ('eq' as const), value: groupField.type === 'select' ? [g.value] : g.value };
    const page = await scope.api.list(object, { ...params, filter: JSON.stringify(withCondition(filter, cond)), limit: 25, offset: have });
    setExtra((m) => ({ ...m, [g.value]: [...(m[g.value] ?? []), ...page.data] }));
  };

  if (q.isError) return <ErrorState title={t('lists.board.error')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />;
  if (!q.data) {
    return (
      <div className="flex gap-3 overflow-x-auto p-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-72 w-72 shrink-0" />
        ))}
      </div>
    );
  }
  return (
    <div className="flex min-h-[420px] gap-3 overflow-x-auto p-3" role="list" aria-label={t('lists.board.label', { field: groupField.label })}>
      {q.data.groups.map((g) => {
        const rows = [...g.rows, ...(extra[g.value] ?? []).filter((r) => !g.rows.some((x) => x.id === r.id))];
        const total = amountField ? rows.reduce((n, r) => n + (Number(r.values[amountField.key]) || 0), 0) : 0;
        return (
          <section
            key={g.value || '_empty'}
            role="listitem"
            aria-label={`${g.label} (${g.count})`}
            onDragOver={(e) => {
              if (!canEdit || !dragging) return;
              e.preventDefault();
              setOver(g.value);
            }}
            onDragLeave={() => setOver((o) => (o === g.value ? null : o))}
            onDrop={(e) => {
              e.preventDefault();
              setOver(null);
              if (dragging && String(dragging.values[groupField.key] ?? '') !== g.value) move.mutate({ row: dragging, value: g.value });
              setDragging(null);
            }}
            className={cn('flex w-72 shrink-0 flex-col rounded-lg border bg-muted/30', over === g.value && 'ring-2 ring-primary/40')}
          >
            <header className="flex items-center gap-2 border-b px-3 py-2">
              <span className={cn('size-2 rounded-full', toneBar[g.tone ?? 'neutral'])} aria-hidden />
              <h3 className="truncate text-[13px] font-semibold">{g.label}</h3>
              <span className="rounded-full bg-background px-1.5 text-[11px] tabular-nums text-muted-foreground">{g.count}</span>
              {amountField && total > 0 ? <span className="ml-auto text-[11px] font-medium tabular-nums text-muted-foreground">{formatMoney(total)}</span> : null}
              {onNew && canEdit && g.value ? (
                <Button variant="subtle" size="icon-sm" className={cn(!(amountField && total > 0) && 'ml-auto')} onClick={() => onNew({ [groupField.key]: g.value })}
                  aria-label={t('lists.board.addIn', { column: g.label })}>
                  <Plus />
                </Button>
              ) : null}
            </header>
            <div className="flex-1 space-y-2 overflow-y-auto p-2">
              {rows.map((r) => (
                <article
                  key={r.id}
                  draggable={canEdit}
                  onDragStart={() => setDragging(r)}
                  onDragEnd={() => setDragging(null)}
                  className={cn('rounded-md border bg-card p-2.5 shadow-sm transition-shadow hover:shadow-md', canEdit && 'cursor-grab active:cursor-grabbing', dragging?.id === r.id && 'opacity-50')}
                >
                  <Link to={rowHref(scope, object, r)} className="block truncate text-[13px] font-medium text-foreground hover:text-primary hover:underline">
                    {r.title || t('records.common.untitled')}
                  </Link>
                  <dl className="mt-1 space-y-0.5 text-xs">
                    {cardFields.map((f) =>
                      r.values[f.key] === null || r.values[f.key] === undefined || r.values[f.key] === '' ? null : (
                        <div key={f.key} className="flex min-w-0 gap-1.5">
                          <dt className="shrink-0 text-muted-foreground">{f.label}</dt>
                          <dd className="min-w-0 truncate text-foreground">
                            <FieldValue field={f} record={r} meta={meta} compact />
                          </dd>
                        </div>
                      )
                    )}
                  </dl>
                </article>
              ))}
              {rows.length === 0 ? <p className="px-1 py-6 text-center text-xs text-muted-foreground">{canEdit ? t('lists.board.dropHere') : t('lists.board.empty')}</p> : null}
              {rows.length < g.count ? (
                <Button variant="ghost" size="sm" className="w-full" onClick={() => void loadMore(g)}>
                  {t('lists.board.more', { count: g.count - rows.length })}
                </Button>
              ) : null}
            </div>
          </section>
        );
      })}
    </div>
  );
}

export function BoardBadge({ label }: { label: string }) {
  return <Badge tone="primary">{label}</Badge>;
}
