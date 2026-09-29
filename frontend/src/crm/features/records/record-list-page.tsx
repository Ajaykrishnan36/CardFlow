import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { ArrowDown, ArrowUp, ArrowUpDown, Building2, ChevronLeft, ChevronRight, Crown, Ellipsis, LayoutTemplate, Plus, RefreshCw } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { FieldDef, ObjectKey, ObjectMeta, RecordListParams, RecordPage, RecordRow, RecordWorkspaceRef } from '@crm/api/types';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '@crm/components/ui/menu';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { FieldValue } from './field-value';
import { NewRecordDialog } from './new-record-dialog';
import { fieldIndex, objectIcon, recordKeys, statusOption, useObjectMeta } from './use-object-meta';
import { layoutHref, recordHref, useRecordScope, type RecordScope } from './record-scope';
import { isForbidden, RecordNoAccess } from './record-states';

const PAGE_SIZE = 50;
/** Sort key for the primary "Name" column (record title). */
const TITLE_SORT = 'title';
/** Owner lists: which workspaces to show ('all' | 'platform' | workspace code). */
const ALL_WS = 'all';
const PLATFORM_WS = 'platform';

type WsOption = RecordWorkspaceRef & { count: number };

/**
 * Where a row opens. Records of a customer workspace open inside that workspace
 * (its own layout and custom fields; the owner has full access there).
 */
function rowHref(scope: RecordScope, object: ObjectKey, r: RecordRow): string {
  if (scope.audience === 'owner' && r.workspace && !r.workspace.isPlatform) {
    return `/crm/w/${encodeURIComponent(r.workspace.code)}/${object}/${encodeURIComponent(r.id)}`;
  }
  return recordHref(scope, object, r.id);
}

function WorkspaceChip({ ws }: { ws?: RecordWorkspaceRef }) {
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

export function RecordListPage({ object }: { object: ObjectKey }) {
  const scope = useRecordScope();
  if (!scope.can(object, 'read')) return <RecordNoAccess />;
  return <RecordListView object={object} />;
}

function RecordListView({ object }: { object: ObjectKey }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const scope = useRecordScope();
  const canCreate = scope.can(object, 'create');
  const metaQ = useObjectMeta(object);
  const meta = metaQ.data;
  useDocumentTitle(meta?.labelPlural ?? t('records.list.loadingTitle'));

  const [sp, setSp] = useSearchParams();
  const q = sp.get('q') ?? '';
  const status = sp.get('status') ?? 'all';
  const sort = sp.get('sort') ?? '';
  const dir: 'asc' | 'desc' = sp.get('dir') === 'desc' ? 'desc' : 'asc';
  const page = Math.max(1, Number(sp.get('page')) || 1);
  const isOwner = scope.audience === 'owner';
  const ws = isOwner ? sp.get('ws') || ALL_WS : undefined;
  const [newOpen, setNewOpen] = useState(false);

  // ?new=1 (dashboard quick actions) opens the create dialog once.
  const wantsNew = sp.get('new') === '1';
  useEffect(() => {
    if (!wantsNew) return;
    if (canCreate) setNewOpen(true);
    setSp(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('new');
        return next;
      },
      { replace: true }
    );
  }, [wantsNew, canCreate, setSp]);

  const patchParams = useCallback(
    (patch: Record<string, string | null>) =>
      setSp(
        (prev) => {
          const next = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(patch)) {
            if (v === null || v === '') next.delete(k);
            else next.set(k, v);
          }
          return next;
        },
        { replace: true }
      ),
    [setSp]
  );
  const onSearch = useCallback((v: string) => patchParams({ q: v, page: null }), [patchParams]);

  const params: RecordListParams = {
    workspace: ws,
    q: q || undefined,
    status: status !== 'all' && meta?.statusField ? status : undefined,
    sort: sort || undefined,
    dir: sort ? dir : undefined,
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE
  };
  const listQ = useQuery({
    queryKey: recordKeys.list(scope.prefix, object, params),
    queryFn: () => scope.api.list(object, params),
    enabled: Boolean(meta),
    placeholderData: keepPreviousData
  });

  const columns = useMemo(() => {
    const byKey = fieldIndex(meta);
    return (meta?.listColumns ?? []).filter((k) => k !== 'code').map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f));
  }, [meta]);

  // Per-workspace totals (ignore q/status). Keep the last ones while a new page loads.
  const [wsOptions, setWsOptions] = useState<WsOption[]>([]);
  const pageData: RecordPage | undefined = listQ.data;
  useEffect(() => {
    if (pageData?.workspaces) setWsOptions(pageData.workspaces);
  }, [pageData]);

  const toggleSort = (key: string) => {
    if (sort === key) patchParams({ dir: dir === 'asc' ? 'desc' : 'asc', page: null });
    else patchParams({ sort: key, dir: 'asc', page: null });
  };

  if (isForbidden(metaQ.error) || isForbidden(listQ.error)) return <RecordNoAccess />;

  if (metaQ.isError) {
    return (
      <PageContainer wide>
        <Card>
          <ErrorState
            title={t('records.common.loadError')}
            message={isApiError(metaQ.error) ? metaQ.error.message : undefined}
            requestId={isApiError(metaQ.error) ? metaQ.error.requestId : undefined}
            onRetry={() => void metaQ.refetch()}
          />
        </Card>
      </PageContainer>
    );
  }

  const Icon = objectIcon(object, meta?.icon);
  const total = listQ.data?.total ?? 0;
  const rows = listQ.data?.data;
  const filtered = Boolean(q) || status !== 'all';
  const from = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const to = Math.min(page * PAGE_SIZE, total);
  const plural = meta?.labelPlural ?? '';
  const rowScope = scope.rowScope(object);
  const platformOpt = wsOptions.find((w) => w.isPlatform);
  const customerOpts = wsOptions.filter((w) => !w.isPlatform);
  const selectedWs = ws && ws !== ALL_WS && ws !== PLATFORM_WS ? customerOpts.find((w) => w.code === ws) : undefined;
  const selectedWsName = ws === PLATFORM_WS ? t('records.list.platformCrm') : (selectedWs?.name ?? ws ?? '');
  const showWsColumn = isOwner && ws !== PLATFORM_WS;
  const wsFiltered = isOwner && ws !== ALL_WS;
  const subtitle = !isOwner
    ? t(rowScope === 'own' ? 'records.list.subtitleOwn' : 'records.list.subtitleWorkspace', { objects: plural.toLowerCase() })
    : ws === ALL_WS
      ? t('records.list.subtitleAll')
      : t('records.list.subtitleIn', { name: selectedWsName });

  return (
    <PageContainer wide>
      <PageHeader
        title={meta ? meta.labelPlural : <Skeleton className="h-7 w-32" />}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <Icon className="size-5" aria-hidden />
          </span>
        }
        description={meta ? subtitle : undefined}
        actions={
          meta ? (
            <>
              {canCreate ? (
                <Button onClick={() => setNewOpen(true)}>
                  <Plus /> {t('records.list.new', { object: meta.labelSingular })}
                </Button>
              ) : null}
              <Menu>
                <MenuTrigger asChild>
                  <Button variant="outline" size="icon" aria-label={t('records.common.moreActions')}>
                    <Ellipsis />
                  </Button>
                </MenuTrigger>
                <MenuContent align="end">
                  {scope.canCustomize ? (
                    <MenuItem onSelect={() => navigate(layoutHref(scope, object))}>
                      <LayoutTemplate /> {t('records.common.editLayout')}
                    </MenuItem>
                  ) : null}
                  <MenuItem onSelect={() => void listQ.refetch()}>
                    <RefreshCw /> {t('records.list.refresh')}
                  </MenuItem>
                </MenuContent>
              </Menu>
            </>
          ) : null
        }
      />

      <Card className="min-w-0 overflow-hidden">
        <div className="flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center">
          <SearchInput value={q} onChange={onSearch} placeholder={t('records.list.search', { objects: plural.toLowerCase() })} className="lg:max-w-xs" />
          {isOwner ? (
            <Select
              className="lg:w-60"
              aria-label={t('records.list.workspaceFilter')}
              value={ws}
              onChange={(e) => patchParams({ ws: e.target.value === ALL_WS ? null : e.target.value, page: null })}
            >
              <option value={ALL_WS}>
                {wsOptions.length ? t('records.list.allWorkspacesN', { count: wsOptions.reduce((n, w) => n + w.count, 0) }) : t('records.list.allWorkspaces')}
              </option>
              <option value={PLATFORM_WS}>
                {platformOpt ? t('records.list.wsOption', { name: t('records.list.platformCrm'), count: platformOpt.count }) : t('records.list.platformCrm')}
              </option>
              {customerOpts.map((w) => (
                <option key={w.code} value={w.code}>
                  {t('records.list.wsOption', { name: w.name, count: w.count })}
                </option>
              ))}
              {/* A code from the URL that isn't in the list (yet). */}
              {ws && ws !== ALL_WS && ws !== PLATFORM_WS && !selectedWs ? <option value={ws}>{ws}</option> : null}
            </Select>
          ) : null}
          {meta?.statusField && meta.statuses?.length ? (
            <SegmentedFilter
              value={status}
              onChange={(v) => patchParams({ status: v === 'all' ? null : v, page: null })}
              options={[{ value: 'all', label: t('records.list.all') }, ...meta.statuses.map((s) => ({ value: s.value, label: s.label }))]}
            />
          ) : null}
          <p className="text-[13px] text-muted-foreground lg:ml-auto" aria-live="polite">
            {listQ.data ? t('records.list.count', { count: total }) : ' '}
          </p>
        </div>

        {listQ.isError ? (
          <ErrorState
            title={t('records.list.errorTitle', { objects: plural.toLowerCase() })}
            message={isApiError(listQ.error) ? listQ.error.message : undefined}
            requestId={isApiError(listQ.error) ? listQ.error.requestId : undefined}
            onRetry={() => void listQ.refetch()}
          />
        ) : !meta || !rows ? (
          <ListSkeleton columns={Math.max(columns.length, 3)} />
        ) : rows.length === 0 ? (
          wsFiltered && !filtered ? (
            <EmptyState
              icon={Icon}
              title={t('records.list.wsEmptyTitle', { objects: plural.toLowerCase(), name: selectedWsName })}
              body={t('records.list.wsEmptyBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => patchParams({ ws: null, page: null })}>
                  {t('records.list.showAllWorkspaces')}
                </Button>
              }
            />
          ) : filtered ? (
            <EmptyState
              icon={Icon}
              title={t('records.list.noMatchTitle')}
              body={t('records.list.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => patchParams({ q: null, status: null, page: null })}>
                  {t('records.list.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={Icon}
              title={t('records.list.emptyTitle', { objects: plural.toLowerCase() })}
              body={
                canCreate
                  ? t('records.list.emptyBody', { object: meta.labelSingular.toLowerCase() })
                  : t(rowScope === 'own' ? 'records.list.emptyBodyOwn' : 'records.list.emptyBodyReadOnly', { objects: plural.toLowerCase() })
              }
              action={
                canCreate ? (
                  <Button onClick={() => setNewOpen(true)}>
                    <Plus /> {t('records.list.new', { object: meta.labelSingular })}
                  </Button>
                ) : undefined
              }
            />
          )
        ) : (
          <div className={cn('transition-opacity', listQ.isPlaceholderData && 'opacity-60')}>
            <RecordTable object={object} meta={meta} columns={columns} rows={rows} sort={sort} dir={dir} onSort={toggleSort} showWorkspace={showWsColumn} />
            <RecordCards object={object} meta={meta} columns={columns} rows={rows} showWorkspace={showWsColumn} />
            <div className="flex items-center justify-between gap-3 border-t px-4 py-2.5">
              <p className="text-[13px] tabular-nums text-muted-foreground">{t('records.list.range', { from, to, total })}</p>
              <div className="flex items-center gap-1">
                <Button
                  variant="outline"
                  size="icon-sm"
                  disabled={page <= 1}
                  onClick={() => patchParams({ page: page - 1 <= 1 ? null : String(page - 1) })}
                  aria-label={t('records.list.prev')}
                >
                  <ChevronLeft />
                </Button>
                <Button variant="outline" size="icon-sm" disabled={to >= total} onClick={() => patchParams({ page: String(page + 1) })} aria-label={t('records.list.next')}>
                  <ChevronRight />
                </Button>
              </div>
            </div>
          </div>
        )}
      </Card>

      {meta && canCreate ? (
        <NewRecordDialog
          object={object}
          meta={meta}
          open={newOpen}
          onOpenChange={setNewOpen}
          note={isOwner && ws !== PLATFORM_WS ? t('records.new.platformNote') : undefined}
        />
      ) : null}
    </PageContainer>
  );
}

function SortHeader({ label, sortKey, sort, dir, onSort, className }: { label: string; sortKey: string; sort: string; dir: 'asc' | 'desc'; onSort: (k: string) => void; className?: string }) {
  const { t } = useTranslation();
  const active = sort === sortKey;
  const SortIcon = !active ? ArrowUpDown : dir === 'asc' ? ArrowUp : ArrowDown;
  return (
    <th scope="col" aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : undefined} className={cn('px-3 py-2 font-medium', className)}>
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className={cn('group inline-flex items-center gap-1 rounded px-1 py-0.5 -mx-1 hover:bg-muted hover:text-foreground', active && 'text-foreground')}
        title={t('records.list.sortBy', { field: label })}
      >
        <span className="truncate">{label}</span>
        <SortIcon className={cn('size-3 shrink-0', active ? 'opacity-100' : 'opacity-0 group-hover:opacity-60 group-focus-visible:opacity-60')} aria-hidden />
      </button>
    </th>
  );
}

function RecordTable({
  object,
  meta,
  columns,
  rows,
  sort,
  dir,
  onSort,
  showWorkspace
}: {
  object: ObjectKey;
  meta: ObjectMeta;
  columns: FieldDef[];
  rows: RecordRow[];
  sort: string;
  dir: 'asc' | 'desc';
  onSort: (k: string) => void;
  showWorkspace?: boolean;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const scope = useRecordScope();
  const numeric = (f: FieldDef) => f.type === 'number' || f.type === 'currency' || f.type === 'percent';
  return (
    <div className="hidden overflow-x-auto sm:block">
      <table className="w-full text-left text-[13px]">
        <thead className="bg-muted/40">
          <tr className="border-b text-xs text-muted-foreground">
            <SortHeader label={t('records.list.name')} sortKey={TITLE_SORT} sort={sort} dir={dir} onSort={onSort} className="pl-4" />
            {showWorkspace ? (
              <th scope="col" className="px-3 py-2 font-medium">
                {t('records.list.workspace')}
              </th>
            ) : null}
            {columns.map((f) => (
              <SortHeader key={f.key} label={f.label} sortKey={f.key} sort={sort} dir={dir} onSort={onSort} className={cn(numeric(f) && 'text-right')} />
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const href = rowHref(scope, object, r);
            return (
              <tr key={r.id} className="cursor-pointer border-b last:border-0 hover:bg-muted/50" onClick={() => navigate(href)}>
                <td className="max-w-[280px] py-2 pl-4 pr-3">
                  <Link to={href} className="block truncate font-medium text-foreground hover:text-primary hover:underline" onClick={(e) => e.stopPropagation()}>
                    {r.title || t('records.common.untitled')}
                  </Link>
                  <span className="font-mono text-[11px] text-muted-foreground">{r.code}</span>
                </td>
                {showWorkspace ? (
                  <td className="max-w-[200px] px-3 py-2">
                    <WorkspaceChip ws={r.workspace} />
                  </td>
                ) : null}
                {columns.map((f) => (
                  <td key={f.key} className={cn('max-w-[240px] px-3 py-2 text-foreground', numeric(f) && 'text-right')}>
                    <div className={cn('flex min-w-0', numeric(f) && 'justify-end')}>
                      <FieldValue field={f} record={r} meta={meta} compact className="truncate" />
                    </div>
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function RecordCards({ object, meta, columns, rows, showWorkspace }: { object: ObjectKey; meta: ObjectMeta; columns: FieldDef[]; rows: RecordRow[]; showWorkspace?: boolean }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const detailCols = columns.filter((c) => c.key !== meta.statusField).slice(0, 3);
  return (
    <ul className="divide-y sm:hidden">
      {rows.map((r) => {
        const st = meta.statusField ? statusOption(meta, r.values[meta.statusField]) : undefined;
        return (
          <li key={r.id}>
            <Link to={rowHref(scope, object, r)} className="block px-4 py-3 hover:bg-muted/50">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-[13px] font-medium text-foreground">{r.title || t('records.common.untitled')}</p>
                  <p className="font-mono text-[11px] text-muted-foreground">{r.code}</p>
                  {showWorkspace && r.workspace ? (
                    <div className="mt-1">
                      <WorkspaceChip ws={r.workspace} />
                    </div>
                  ) : null}
                </div>
                {st ? <Badge tone={st.tone}>{st.label}</Badge> : null}
              </div>
              {detailCols.length ? (
                <dl className="mt-1.5 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
                  {detailCols.map((f) => (
                    <div key={f.key} className="contents">
                      <dt className="text-muted-foreground">{f.label}</dt>
                      <dd className="min-w-0 truncate text-foreground">
                        <FieldValue field={f} record={r} meta={meta} compact />
                      </dd>
                    </div>
                  ))}
                </dl>
              ) : null}
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

function ListSkeleton({ columns }: { columns: number }) {
  return (
    <div aria-busy="true">
      <div className="hidden sm:block">
        {Array.from({ length: 8 }).map((_, i) => (
          <div key={i} className="flex items-center gap-6 border-b px-4 py-3 last:border-0">
            <div className="w-48 space-y-1.5">
              <Skeleton className="h-3.5 w-40" />
              <Skeleton className="h-3 w-20" />
            </div>
            {Array.from({ length: columns }).map((__, j) => (
              <Skeleton key={j} className="h-3.5 flex-1" />
            ))}
          </div>
        ))}
      </div>
      <div className="divide-y sm:hidden">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="space-y-2 px-4 py-3">
            <Skeleton className="h-3.5 w-3/5" />
            <Skeleton className="h-3 w-2/5" />
          </div>
        ))}
      </div>
    </div>
  );
}
