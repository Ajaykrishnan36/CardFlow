import { Fragment, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowDown, ArrowUp, ArrowUpDown, Building2, ChevronDown, Crown, Pencil } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta, RecordRow, RecordWorkspaceRef } from '@crm/api/types';
import type { RecordGroup, SortSpec } from '@crm/api/types-features';
import { Badge } from '@crm/components/ui/card';
import { cn } from '@crm/lib/utils';
import { FieldValue, formatValueText } from '../field-value';
import { FieldInput } from '../field-input';
import { recordKeys, sameValue, statusOption } from '../use-object-meta';
import { formatMoney } from '@crm/lib/money';
import { recordHref, useRecordScope, type RecordScope } from '../record-scope';

export function rowHref(scope: RecordScope, object: ObjectKey, r: RecordRow): string {
  if (scope.audience === 'owner' && r.workspace && !r.workspace.isPlatform) {
    return `/crm/w/${encodeURIComponent(r.workspace.code)}/${object}/${encodeURIComponent(r.id)}`;
  }
  return recordHref(scope, object, r.id);
}

export function WorkspaceChip({ ws }: { ws?: RecordWorkspaceRef }) {
  const { t } = useTranslation();
  if (!ws) return <span className="text-muted-foreground/60">—</span>;
  const Icon = ws.isPlatform ? Crown : Building2;
  const name = ws.isPlatform ? t('records.list.platformCrm') : ws.name;
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-1 whitespace-nowrap rounded-md border px-1.5 py-0.5 text-[11px] font-medium',
        ws.isPlatform ? 'border-amber-300/50 bg-amber-50 text-amber-900 dark:border-amber-400/25 dark:bg-amber-400/10 dark:text-amber-100' : 'bg-muted/50 text-muted-foreground'
      )}
      title={name}
    >
      <Icon className="size-3 shrink-0" aria-hidden />
      <span className="truncate">{name}</span>
    </span>
  );
}

const numeric = (f: FieldDef) => ['number', 'currency', 'percent', 'rating'].includes(f.type);
const inlineEditable = (f: FieldDef) => !f.readOnly && !['files', 'json', 'address', 'textarea', 'relations', 'fullName'].includes(f.type);

function SortHeader({ label, sortKey, sorts, onSort, className }: { label: string; sortKey: string; sorts: SortSpec[]; onSort: (k: string) => void; className?: string }) {
  const { t } = useTranslation();
  const idx = sorts.findIndex((s) => s.field === sortKey);
  const s = idx >= 0 ? sorts[idx] : undefined;
  const Icon = !s ? ArrowUpDown : s.dir === 'asc' ? ArrowUp : ArrowDown;
  return (
    <th scope="col" aria-sort={s ? (s.dir === 'asc' ? 'ascending' : 'descending') : undefined} className={cn('whitespace-nowrap px-3 py-2 font-medium', className)}>
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className={cn('group -mx-1 inline-flex items-center gap-1 rounded px-1 py-0.5 hover:bg-muted hover:text-foreground', s && 'text-foreground')}
        title={t('records.list.sortBy', { field: label })}
      >
        <span className="truncate">{label}</span>
        <Icon className={cn('size-3 shrink-0', s ? 'opacity-100' : 'opacity-0 group-hover:opacity-60')} aria-hidden />
        {s && sorts.length > 1 ? <span className="text-[10px] tabular-nums text-muted-foreground">{idx + 1}</span> : null}
      </button>
    </th>
  );
}

export function RecordTable({
  object,
  meta,
  columns,
  rows,
  sorts,
  onSort,
  showWorkspace,
  selected,
  onSelect,
  aggregates,
  compact,
  binMode,
  sections,
  onShowAll
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  columns: FieldDef[];
  rows: RecordRow[];
  sorts: SortSpec[];
  onSort: (k: string) => void;
  showWorkspace?: boolean;
  selected: Set<string>;
  onSelect: (ids: Set<string>) => void;
  aggregates?: Record<string, Record<string, number>>;
  compact?: boolean;
  binMode?: boolean;
  /** Grouped table: one section per value, rows as returned for that group. */
  sections?: RecordGroup[];
  onShowAll?: (group: RecordGroup) => void;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const scope = useRecordScope();
  const canEdit = !binMode && scope.can(object, 'update');
  const allOn = rows.length > 0 && rows.every((r) => selected.has(r.id));
  const toggleAll = () => {
    const next = new Set(selected);
    if (allOn) rows.forEach((r) => next.delete(r.id));
    else rows.forEach((r) => next.add(r.id));
    onSelect(next);
  };
  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onSelect(next);
  };
  const aggCols = columns.filter((f) => aggregates?.[f.key]);
  const py = compact ? 'py-1' : 'py-2';
  const colCount = 2 + (showWorkspace ? 1 : 0) + (binMode ? 2 : 0) + columns.length;
  const renderRow = (r: RecordRow) => {
            const href = rowHref(scope, object, r);
            const on = selected.has(r.id);
            return (
              <tr key={r.id} className={cn('group border-b last:border-0', on ? 'bg-primary-soft/60' : 'hover:bg-muted/50')}>
                <td className={cn('w-10 pl-4', py)}>
                  <input type="checkbox" className="size-4 accent-[hsl(var(--primary))]" checked={on} onChange={() => toggle(r.id)} aria-label={t('lists.selectRow', { name: r.title })} />
                </td>
                <td className={cn('max-w-[280px] pl-1 pr-3', py)}>
                  {binMode ? (
                    <span className="block truncate font-medium text-foreground">{r.title || t('records.common.untitled')}</span>
                  ) : (
                    <Link to={href} className="block truncate font-medium text-foreground hover:text-primary hover:underline">
                      {r.title || t('records.common.untitled')}
                    </Link>
                  )}
                  {!compact ? <span className="font-mono text-[11px] text-muted-foreground">{r.code}</span> : null}
                </td>
                {showWorkspace ? (
                  <td className={cn('max-w-[200px] px-3', py)}>
                    <WorkspaceChip ws={r.workspace} />
                  </td>
                ) : null}
                {binMode ? (
                  <>
                    <td className={cn('whitespace-nowrap px-3 text-muted-foreground', py)}>{r.deletedAt ? new Date(r.deletedAt).toLocaleString('en-IN') : '—'}</td>
                    <td className={cn('px-3 text-muted-foreground', py)}>{r.lookups.deletedBy?.label || '—'}</td>
                  </>
                ) : null}
                {columns.map((f) => (
                  <EditableCell key={f.key} object={object} meta={meta} field={f} row={r} editable={canEdit && inlineEditable(f)} py={py} onOpen={() => navigate(href)} />
                ))}
              </tr>
            );
  };
  return (
    <div className="hidden overflow-x-auto sm:block">
      <table className="w-full text-left text-[13px]">
        <thead className="sticky top-0 z-10 bg-muted/60 backdrop-blur">
          <tr className="border-b text-xs text-muted-foreground">
            <th scope="col" className="w-10 pl-4">
              <input type="checkbox" className="size-4 accent-[hsl(var(--primary))]" checked={allOn} onChange={toggleAll} aria-label={t('lists.selectPage')} />
            </th>
            <SortHeader label={t('records.list.name')} sortKey="title" sorts={sorts} onSort={onSort} className="pl-1" />
            {showWorkspace ? (
              <th scope="col" className="px-3 py-2 font-medium">
                {t('records.list.workspace')}
              </th>
            ) : null}
            {binMode ? (
              <>
                <th scope="col" className="px-3 py-2 font-medium">{t('lists.bin.deletedAt')}</th>
                <th scope="col" className="px-3 py-2 font-medium">{t('lists.bin.deletedBy')}</th>
              </>
            ) : null}
            {columns.map((f) => (
              <SortHeader key={f.key} label={f.label} sortKey={f.key} sorts={sorts} onSort={onSort} className={cn(numeric(f) && 'text-right')} />
            ))}
          </tr>
        </thead>
        <tbody>
          {sections
            ? sections.map((g) => {
                const shut = collapsed.has(g.value);
                return (
                  <Fragment key={`g:${g.value}`}>
                    <tr className="border-b bg-muted/40">
                      <td colSpan={colCount} className="px-3 py-1.5">
                        <button
                          type="button"
                          className="inline-flex items-center gap-2 text-[13px] font-medium text-foreground"
                          aria-expanded={!shut}
                          onClick={() =>
                            setCollapsed((c) => {
                              const n = new Set(c);
                              if (n.has(g.value)) n.delete(g.value);
                              else n.add(g.value);
                              return n;
                            })
                          }
                        >
                          <ChevronDown className={cn('size-4 text-muted-foreground transition-transform', shut && '-rotate-90')} aria-hidden />
                          {g.tone ? <Badge tone={g.tone}>{g.label}</Badge> : <span>{g.label}</span>}
                          <span className="tabular-nums text-xs text-muted-foreground">{g.count}</span>
                        </button>
                      </td>
                    </tr>
                    {shut ? null : g.rows.map((r) => renderRow(r))}
                    {!shut && g.count > g.rows.length && onShowAll ? (
                      <tr className="border-b">
                        <td colSpan={colCount} className="px-4 py-1.5">
                          <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => onShowAll(g)}>
                            {t('lists.group.showAll', { count: g.count, label: g.label })}
                          </button>
                        </td>
                      </tr>
                    ) : null}
                  </Fragment>
                );
              })
            : rows.map((r) => renderRow(r))}
        </tbody>
        {aggCols.length ? (
          <tfoot className="border-t bg-muted/40 text-xs">
            <tr>
              <td />
              <td className="px-1 py-2 font-medium text-muted-foreground">{t('lists.totals')}</td>
              {showWorkspace ? <td /> : null}
              {binMode ? <><td /><td /></> : null}
              {columns.map((f) => {
                const a = aggregates?.[f.key];
                return (
                  <td key={f.key} className="px-3 py-2 text-right tabular-nums">
                    {a
                      ? Object.entries(a).map(([fn, v]) => (
                          <span key={fn} className="block">
                            <span className="text-muted-foreground">{t(`lists.agg.${fn}`)} </span>
                            <span className="font-semibold text-foreground">{f.type === 'currency' ? formatMoney(v) : v.toLocaleString('en-IN', { maximumFractionDigits: 2 })}</span>
                          </span>
                        ))
                      : null}
                  </td>
                );
              })}
            </tr>
          </tfoot>
        ) : null}
      </table>
    </div>
  );
}

/** A cell that turns into an editor on click (fields the viewer may edit). */
function EditableCell({ object, meta, field, row, editable, py }: { object: ObjectKey; meta: ObjectMeta; field: FieldDef; row: RecordRow; editable: boolean; py: string; onOpen: () => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<unknown>(row.values[field.key]);
  const cellRef = useRef<HTMLTableCellElement>(null);
  useEffect(() => {
    if (!editing) setDraft(row.values[field.key]);
  }, [row.values, field.key, editing]);

  const save = useMutation({
    mutationFn: (v: unknown) => scope.api.update(object, row.id, { [field.key]: v }, row.version),
    onSuccess: () => {
      setEditing(false);
      void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) });
    },
    onError: (e) => {
      const msg = isApiError(e) ? e.fieldErrors[field.key] ?? e.message : t('common.genericError');
      toast.error(msg);
    }
  });
  const commit = (v: unknown = draft) => {
    if (sameValue(v, row.values[field.key])) return setEditing(false);
    save.mutate(v === '' ? null : v);
  };

  // Clicking outside finishes the edit (selects and lookups keep focus inside the cell).
  useEffect(() => {
    if (!editing) return;
    const onDown = (e: MouseEvent) => {
      if (cellRef.current && !cellRef.current.contains(e.target as Node)) commit();
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  });

  const isStatus = meta.statusField === field.key;
  if (editing) {
    return (
      <td ref={cellRef} className={cn('min-w-[180px] max-w-[320px] px-2', py)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            e.stopPropagation();
            setEditing(false);
          } else if (e.key === 'Enter' && field.type !== 'multiselect' && !(e.target as HTMLElement).getAttribute('aria-expanded')) {
            e.preventDefault();
            commit();
          }
        }}>
        <FieldInput
          field={field}
          value={draft}
          lookupLabel={row.lookups[field.key]?.label}
          links={row.links?.[field.key]}
          currencyHint={row.values[`${field.key}__currency`] as string | undefined}
          disabled={save.isPending}
          onChange={(v) => {
            setDraft(v);
            // Pick-lists, yes/no and ratings save as soon as they change.
            if (['select', 'boolean', 'rating', 'lookup'].includes(field.type)) commit(v);
          }}
        />
      </td>
    );
  }
  return (
    <td
      className={cn('relative max-w-[260px] px-3 text-foreground', py, numeric(field) && 'text-right', editable && 'cursor-text')}
      onClick={() => editable && setEditing(true)}
      title={editable ? t('lists.clickToEdit', { value: formatValueText(field, row.values[field.key], row.lookups[field.key]) }) : undefined}
    >
      <div className={cn('flex min-w-0 items-center gap-1', numeric(field) && 'justify-end')}>
        {isStatus ? (
          (() => {
            const s = statusOption(meta, row.values[field.key]);
            return s ? <Badge tone={s.tone}>{s.label}</Badge> : <FieldValue field={field} record={row} meta={meta} compact className="truncate" />;
          })()
        ) : (
          <FieldValue field={field} record={row} meta={meta} compact className="truncate" />
        )}
        {editable ? <Pencil className="ml-auto size-3 shrink-0 text-muted-foreground opacity-0 group-hover:opacity-50" aria-hidden /> : null}
      </div>
    </td>
  );
}
