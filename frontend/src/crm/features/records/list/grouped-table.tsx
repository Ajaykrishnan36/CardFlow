import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import type { FieldDef, ObjectKey, ObjectMeta, RecordListParams } from '@crm/api/types';
import type { RecordGroup, SortSpec } from '@crm/api/types-features';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { recordKeys } from '../use-object-meta';
import { useRecordScope } from '../record-scope';
import { RecordTable } from './record-table';

/** Table view grouped by a field (D-77): one section per value, up to 50 rows each. */
export function GroupedTable({
  object,
  meta,
  columns,
  groupField,
  params,
  sorts,
  onSort,
  showWorkspace,
  selected,
  onSelect,
  compact,
  onShowAll
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  columns: FieldDef[];
  groupField: FieldDef;
  params: RecordListParams;
  sorts: SortSpec[];
  onSort: (k: string) => void;
  showWorkspace?: boolean;
  selected: Set<string>;
  onSelect: (ids: Set<string>) => void;
  compact?: boolean;
  onShowAll: (group: RecordGroup) => void;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const query = { ...params, limit: 50, offset: undefined, agg: undefined, field: groupField.key };
  const q = useQuery({
    queryKey: [...recordKeys.lists(scope.prefix, object), 'table-groups', query],
    queryFn: () => scope.api.groups(object, query)
  });
  if (q.isPending) return <Skeleton className="m-4 h-40" />;
  if (q.isError) return <ErrorState title={t('lists.group.error')} onRetry={() => void q.refetch()} />;
  const groups = q.data.groups.filter((g) => g.count > 0);
  const rows = groups.flatMap((g) => g.rows);
  const total = groups.reduce((n, g) => n + g.count, 0);
  return (
    <>
      <RecordTable
        object={object}
        meta={meta}
        columns={columns.filter((c) => c.key !== groupField.key)}
        rows={rows}
        sections={groups}
        onShowAll={onShowAll}
        sorts={sorts}
        onSort={onSort}
        showWorkspace={showWorkspace}
        selected={selected}
        onSelect={onSelect}
        compact={compact}
      />
      <p className="border-t px-4 py-2.5 text-[13px] text-muted-foreground">{t('lists.group.summary', { count: total, groups: groups.length, field: groupField.label })}</p>
    </>
  );
}
