import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ChevronLeft, ChevronRight, Download, Ellipsis, LayoutTemplate, Plus, RefreshCw, RotateCcw, Save, Upload } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { workspaceToolsApi, type BulkQuery } from '@crm/api/endpoints';
import type { FieldDef, ObjectKey, ObjectMeta, RecordListParams, RecordRow } from '@crm/api/types';
import type { FilterGroup, SavedView, SortSpec, ViewDefinition, ViewKind } from '@crm/api/types-features';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '@crm/components/ui/menu';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SearchInput } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { FieldValue } from './field-value';
import { NewRecordDialog } from './new-record-dialog';
import { fieldIndex, objectIcon, recordKeys, statusOption, useObjectMeta } from './use-object-meta';
import { layoutHref, useRecordScope } from './record-scope';
import { isForbidden, RecordNoAccess } from './record-states';
import { ALL_VIEW, BIN_VIEW, ViewDialog, ViewsBar } from './list/views-bar';
import { ColumnsButton, DensityButton, FilterButton, SortButton } from './list/list-controls';
import { cleanFilter, countConditions } from './list/filter-utils';
import { RecordTable, rowHref, WorkspaceChip } from './list/record-table';
import { KanbanBoard } from './list/kanban-board';
import { CalendarView } from './list/calendar-view';
import { BulkBar } from './list/bulk-bar';
import { useOwnerFilter } from '@crm/features/owner/owner-filter';
import { ImportDialog } from './list/import-dialog';

const PAGE_SIZE = 50;
const ALL_WS = 'all';
const PLATFORM_WS = 'platform';

function lastViewKey(prefix: string, object: string) {
  return `crm:lastView:${prefix}:${object}`;
}

export function RecordListPage({ object }: { object: ObjectKey }) {
  const scope = useRecordScope();
  if (!scope.can(object, 'read')) return <RecordNoAccess />;
  return <RecordListView object={object} />;
}

function RecordListView({ object }: { object: ObjectKey }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const scope = useRecordScope();
  const canCreate = scope.can(object, 'create');
  const metaQ = useObjectMeta(object);
  const meta = metaQ.data;
  useDocumentTitle(meta?.labelPlural ?? t('records.list.loadingTitle'));
  const [sp, setSp] = useSearchParams();
  const isOwner = scope.audience === 'owner';
  const workspaceCode = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';

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

  // ---- views ----
  const viewsQ = useQuery({ queryKey: [...recordKeys.all(scope.prefix, object), 'views'], queryFn: () => scope.api.views(object), enabled: Boolean(meta) });
  const views = viewsQ.data?.data ?? [];
  const storedView = (() => {
    try {
      return localStorage.getItem(lastViewKey(scope.prefix, object));
    } catch {
      return null;
    }
  })();
  const viewId = sp.get('view') ?? storedView ?? ALL_VIEW;
  const view = views.find((v) => v.id === viewId);
  const binMode = viewId === BIN_VIEW;
  const effectiveViewId = binMode || view || viewId === ALL_VIEW || !viewsQ.data ? viewId : ALL_VIEW;
  useEffect(() => {
    try {
      localStorage.setItem(lastViewKey(scope.prefix, object), effectiveViewId);
    } catch {
      /* private mode */
    }
  }, [effectiveViewId, scope.prefix, object]);

  const baseDef = useMemo<ViewDefinition>(
    () => view?.definition ?? { sorts: binMode ? [] : [{ field: 'createdAt', dir: 'desc' }], columns: meta?.listColumns.filter((k) => k !== 'code') ?? [] },
    [view, meta, binMode]
  );
  const baseKind: ViewKind = binMode ? 'table' : view?.kind ?? 'table';
  const [draft, setDraft] = useState<ViewDefinition>(baseDef);
  const [kind, setKind] = useState<ViewKind>(baseKind);
  useEffect(() => {
    setDraft(baseDef);
    setKind(baseKind);
  }, [baseDef, baseKind]);
  const dirty = JSON.stringify(draft) !== JSON.stringify(baseDef) || kind !== baseKind;

  const q = sp.get('q') ?? '';
  const page = Math.max(1, Number(sp.get('page')) || 1);
  const ownerFilter = useOwnerFilter();
  const ws = isOwner ? sp.get('ws') || ownerFilter.productCode || ALL_WS : undefined;
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [allMatching, setAllMatching] = useState(false);
  const [newOpen, setNewOpen] = useState(false);
  const [newDefaults, setNewDefaults] = useState<Record<string, unknown> | undefined>();
  const [importOpen, setImportOpen] = useState(false);
  const [viewDialog, setViewDialog] = useState<null | { mode: 'create' | 'copy' | 'rename'; view?: SavedView }>(null);

  useEffect(() => {
    setSelected(new Set());
    setAllMatching(false);
  }, [effectiveViewId, q, page, draft.filter]);

  // ?new=1 (dashboard quick actions) opens the create dialog once.
  useEffect(() => {
    if (sp.get('new') !== '1') return;
    if (canCreate) setNewOpen(true);
    patchParams({ new: null });
  }, [sp, canCreate, patchParams]);

  const byKey = useMemo(() => fieldIndex(meta), [meta]);
  const filter = useMemo(() => cleanFilter(draft.filter, byKey), [draft.filter, byKey]);
  const sorts: SortSpec[] = draft.sorts ?? [];
  const columns = useMemo(
    () => (draft.columns?.length ? draft.columns : meta?.listColumns ?? []).filter((k) => k !== 'code').map((k) => byKey.get(k)).filter((f): f is FieldDef => Boolean(f)),
    [draft.columns, meta, byKey]
  );
  const numericCols = columns.filter((f) => f.type === 'currency' || f.type === 'number');
  const agg = draft.aggregates?.length ? draft.aggregates.join(',') : numericCols.map((f) => `${f.key}:sum`).join(',');
  const params: RecordListParams = {
    workspace: ws,
    q: q || undefined,
    filter: filter ? JSON.stringify(filter) : undefined,
    sorts: sorts.length ? JSON.stringify(sorts) : undefined,
    deleted: binMode ? '1' : undefined,
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    agg: agg || undefined
  };
  const listQ = useQuery({
    queryKey: recordKeys.list(scope.prefix, object, params),
    queryFn: () => scope.api.list(object, params),
    enabled: Boolean(meta) && kind === 'table',
    placeholderData: keepPreviousData
  });
  const [wsOptions, setWsOptions] = useState<Array<{ code: string; name: string; isPlatform: boolean; count: number }>>([]);
  useEffect(() => {
    if (listQ.data?.workspaces) setWsOptions(listQ.data.workspaces);
  }, [listQ.data]);

  const favQ = useQuery({ queryKey: ['favorites', scope.prefix], queryFn: () => scope.api.favorites(), staleTime: 60_000 });
  const favViews = new Set((favQ.data ?? []).filter((f) => f.kind === 'view').map((f) => f.targetId));
  const toggleFav = useMutation({
    mutationFn: (v: SavedView) => (favViews.has(v.id) ? scope.api.removeFavorite(v.id) : scope.api.addFavorite({ kind: 'view', object, targetId: v.id })),
    onSuccess: (list) => qc.setQueryData(['favorites', scope.prefix], list)
  });

  const manualWorkflowsQ = useQuery({
    queryKey: ['workflows', workspaceCode],
    queryFn: () => workspaceToolsApi(workspaceCode).workflows(),
    enabled: Boolean(workspaceCode),
    staleTime: 60_000
  });
  const manualWorkflows = (manualWorkflowsQ.data?.data ?? [])
    .filter((w) => w.status === 'active' && w.published?.trigger.type === 'manual' && w.published.trigger.object === object && w.published.trigger.manual?.mode !== 'global')
    .map((w) => ({ id: w.id, name: w.name }));

  const viewsKey = [...recordKeys.all(scope.prefix, object), 'views'];
  const saveView = useMutation({
    mutationFn: async (arg: { mode: 'create' | 'copy' | 'rename' | 'update' | 'share'; view?: SavedView; name?: string; kind?: ViewKind; visibility?: 'personal' | 'shared'; definition?: ViewDefinition }) => {
      if (arg.mode === 'create' || arg.mode === 'copy') return scope.api.createView(object, { name: arg.name, kind: arg.kind, visibility: arg.visibility, definition: arg.definition });
      return scope.api.updateView(object, arg.view!.id, { name: arg.name, kind: arg.kind, visibility: arg.visibility, definition: arg.definition });
    },
    onSuccess: (v, arg) => {
      void qc.invalidateQueries({ queryKey: viewsKey });
      setViewDialog(null);
      if (arg.mode === 'create' || arg.mode === 'copy') patchParams({ view: v.id, page: null });
      toast.success(t(`lists.views.saved.${arg.mode}`, { name: v.name }));
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const deleteView = useMutation({
    mutationFn: (v: SavedView) => scope.api.deleteView(object, v.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: viewsKey });
      patchParams({ view: ALL_VIEW });
    }
  });
  const runWorkflow = useMutation({
    mutationFn: (id: string) => workspaceToolsApi(workspaceCode).runWorkflow(id, { recordIds: [...selected] }),
    onSuccess: (r) => {
      toast.success(t('lists.bulk.workflowStarted', { count: r.runs.length }));
      setSelected(new Set());
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  if (isForbidden(metaQ.error) || isForbidden(listQ.error)) return <RecordNoAccess />;
  if (metaQ.isError) {
    return (
      <PageContainer wide>
        <Card>
          <ErrorState title={t('records.common.loadError')} message={isApiError(metaQ.error) ? metaQ.error.message : undefined} onRetry={() => void metaQ.refetch()} />
        </Card>
      </PageContainer>
    );
  }

  const Icon = objectIcon(object, meta?.icon);
  const total = listQ.data?.total ?? 0;
  const rows = listQ.data?.data;
  const filtered = Boolean(q) || countConditions(filter) > 0;
  const from = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const to = Math.min(page * PAGE_SIZE, total);
  const plural = meta?.labelPlural ?? '';
  const rowScope = scope.rowScope(object);
  const showWsColumn = isOwner && ws !== PLATFORM_WS;
  const subtitle = !isOwner
    ? t(rowScope === 'own' ? 'records.list.subtitleOwn' : 'records.list.subtitleWorkspace', { objects: plural.toLowerCase() })
    : ws === ALL_WS
      ? t('records.list.subtitleAll')
      : t('records.list.subtitleIn', { name: ws === PLATFORM_WS ? t('records.list.platformCrm') : wsOptions.find((w) => w.code === ws)?.name ?? ws });
  const groupField = kind === 'kanban' ? byKey.get(draft.groupBy ?? meta?.statusField ?? '') : undefined;
  const dateField = kind === 'calendar' ? byKey.get(draft.calendarField ?? '') : undefined;
  const bulkQuery: BulkQuery = allMatching ? { filter: filter ?? { op: 'and', filters: [] }, q: q || undefined, workspace: ws } : { ids: [...selected] };
  const exportParams = { ...params, columns: columns.map((c) => c.key).join(','), limit: undefined, offset: undefined, agg: undefined };
  const exportHref = selected.size && !allMatching ? scope.api.exportUrl(object, { ...exportParams, ids: [...selected].join(',') }) : scope.api.exportUrl(object, exportParams);
  const openNew = (defaults?: Record<string, unknown>) => {
    setNewDefaults(defaults);
    setNewOpen(true);
  };

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
              {canCreate && !binMode ? (
                <Button onClick={() => openNew()}>
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
                  {scope.can(object, 'import') && canCreate ? (
                    <MenuItem onSelect={() => setImportOpen(true)}>
                      <Upload /> {t('lists.import.menu')}
                    </MenuItem>
                  ) : null}
                  {scope.can(object, 'export') ? (
                    <MenuItem asChild>
                      <a href={exportHref}>
                        <Download /> {t('lists.export.menu')}
                      </a>
                    </MenuItem>
                  ) : null}
                  {scope.canCustomize ? (
                    <MenuItem onSelect={() => navigate(layoutHref(scope, object))}>
                      <LayoutTemplate /> {t('records.common.editLayout')}
                    </MenuItem>
                  ) : null}
                  <MenuSeparator />
                  <MenuItem onSelect={() => void qc.invalidateQueries({ queryKey: recordKeys.all(scope.prefix, object) })}>
                    <RefreshCw /> {t('records.list.refresh')}
                  </MenuItem>
                </MenuContent>
              </Menu>
            </>
          ) : null
        }
      />

      <Card className="min-w-0 overflow-hidden">
        {meta ? (
          <ViewsBar
            meta={meta}
            views={views}
            current={effectiveViewId}
            onSelect={(id) => patchParams({ view: id, page: null })}
            canShare={Boolean(viewsQ.data?.canShare)}
            canBin={scope.can(object, 'delete')}
            favorites={favViews}
            onToggleFavorite={(v) => toggleFav.mutate(v)}
            onCreate={() => setViewDialog({ mode: 'create' })}
            onRename={(v) => setViewDialog({ mode: 'rename', view: v })}
            onDuplicate={(v) => setViewDialog({ mode: 'copy', view: v })}
            onShare={(v, shared) => saveView.mutate({ mode: 'share', view: v, visibility: shared ? 'shared' : 'personal' })}
            onDelete={(v) => deleteView.mutate(v)}
          />
        ) : null}
        <div className="flex flex-col gap-2 border-b px-4 py-2.5 lg:flex-row lg:items-center">
          <div className="min-w-0 lg:w-72 lg:shrink">
            <SearchInput value={q} onChange={(v) => patchParams({ q: v, page: null })} placeholder={t('records.list.search', { objects: plural.toLowerCase() })} className="lg:max-w-none" />
          </div>
          {isOwner ? (
            <Select className="lg:w-56" aria-label={t('records.list.workspaceFilter')} value={ws} onChange={(e) => patchParams({ ws: e.target.value === ALL_WS ? null : e.target.value, page: null })}>
              <option value={ALL_WS}>{t('records.list.allWorkspaces')}</option>
              <option value={PLATFORM_WS}>{t('records.list.platformCrm')}</option>
              {wsOptions.filter((w) => !w.isPlatform).map((w) => (
                <option key={w.code} value={w.code}>
                  {t('records.list.wsOption', { name: w.name, count: w.count })}
                </option>
              ))}
            </Select>
          ) : null}
          {meta ? (
            <div className="flex flex-wrap items-center gap-1.5 lg:shrink-0 lg:flex-nowrap">
              <FilterButton meta={meta} value={draft.filter} onChange={(g: FilterGroup | undefined) => { setDraft((d) => ({ ...d, filter: g })); patchParams({ page: null }); }} />
              {kind !== 'calendar' ? <SortButton meta={meta} value={sorts} onChange={(s) => setDraft((d) => ({ ...d, sorts: s }))} /> : null}
              {kind === 'table' ? <ColumnsButton meta={meta} value={columns.map((c) => c.key)} onChange={(cols) => setDraft((d) => ({ ...d, columns: cols }))} /> : null}
              {kind === 'table' ? <DensityButton compact={draft.density === 'compact'} onChange={(c) => setDraft((d) => ({ ...d, density: c ? 'compact' : '' }))} /> : null}
              {kind === 'kanban' ? (
                <Select className="w-44" aria-label={t('lists.views.columnsBy')} value={draft.groupBy ?? meta.statusField ?? ''}
                  onChange={(e) => setDraft((d) => ({ ...d, groupBy: e.target.value }))}
                  options={meta.fields.filter((f) => ['select', 'boolean', 'lookup', 'rating'].includes(f.type)).map((f) => ({ value: f.key, label: t('lists.board.by', { field: f.label }) }))} />
              ) : null}
            </div>
          ) : null}
          <div className="flex items-center gap-2 lg:ml-auto">
            {dirty && !binMode ? (
              <>
                <Button variant="ghost" size="sm" onClick={() => { setDraft(baseDef); setKind(baseKind); }}>
                  <RotateCcw /> {t('lists.views.reset')}
                </Button>
                {view?.canEdit ? (
                  <Button size="sm" variant="outline" loading={saveView.isPending} onClick={() => saveView.mutate({ mode: 'update', view, definition: draft, kind })}>
                    <Save /> {t('lists.views.saveChanges')}
                  </Button>
                ) : null}
                <Button size="sm" onClick={() => setViewDialog({ mode: 'create' })}>
                  <Save /> {t('lists.views.saveAs')}
                </Button>
              </>
            ) : kind === 'table' ? (
              <p className="text-[13px] text-muted-foreground" aria-live="polite">
                {listQ.data ? t('records.list.count', { count: total }) : ' '}
              </p>
            ) : null}
          </div>
        </div>

        {selected.size > 0 && meta ? (
          <BulkBar
            object={object}
            meta={meta}
            count={selected.size}
            allMatching={allMatching}
            totalMatching={total}
            query={bulkQuery}
            onSelectAll={() => setAllMatching(true)}
            onClear={() => {
              setSelected(new Set());
              setAllMatching(false);
            }}
            binMode={binMode}
            exportHref={exportHref}
            workflows={manualWorkflows}
            onRunWorkflow={(id) => runWorkflow.mutate(id)}
          />
        ) : null}

        {!meta ? (
          <ListSkeleton columns={3} />
        ) : kind === 'kanban' && groupField ? (
          <KanbanBoard object={object} meta={meta} groupField={groupField} filter={filter}
            params={{ q: q || undefined, filter: params.filter, workspace: ws }}
            cardFields={columns.filter((c) => c.key !== groupField.key).slice(0, 4)}
            amountField={byKey.get('amount')}
            onNew={canCreate ? openNew : undefined} />
        ) : kind === 'calendar' && dateField ? (
          <CalendarView object={object} meta={meta} dateField={dateField} filter={filter} initialMode={draft.calendarMode}
            params={{ q: q || undefined, workspace: ws }}
            onModeChange={(m) => setDraft((d) => ({ ...d, calendarMode: m }))}
            onNew={canCreate ? openNew : undefined} />
        ) : listQ.isError ? (
          <ErrorState
            title={t('records.list.errorTitle', { objects: plural.toLowerCase() })}
            message={isApiError(listQ.error) ? listQ.error.message : undefined}
            requestId={isApiError(listQ.error) ? listQ.error.requestId : undefined}
            onRetry={() => void listQ.refetch()}
          />
        ) : !rows ? (
          <ListSkeleton columns={Math.max(columns.length, 3)} />
        ) : rows.length === 0 ? (
          binMode ? (
            <EmptyState icon={Icon} title={t('lists.bin.emptyTitle')} body={t('lists.bin.emptyBody', { objects: plural.toLowerCase() })} />
          ) : filtered ? (
            <EmptyState
              icon={Icon}
              title={t('records.list.noMatchTitle')}
              body={t('records.list.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => { patchParams({ q: null, page: null }); setDraft((d) => ({ ...d, filter: undefined })); }}>
                  {t('records.list.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={Icon}
              title={t('records.list.emptyTitle', { objects: plural.toLowerCase() })}
              body={canCreate ? t('records.list.emptyBody', { object: meta.labelSingular.toLowerCase() }) : t(rowScope === 'own' ? 'records.list.emptyBodyOwn' : 'records.list.emptyBodyReadOnly', { objects: plural.toLowerCase() })}
              action={
                canCreate ? (
                  <div className="flex flex-wrap justify-center gap-2">
                    <Button onClick={() => openNew()}>
                      <Plus /> {t('records.list.new', { object: meta.labelSingular })}
                    </Button>
                    {scope.can(object, 'import') ? (
                      <Button variant="outline" onClick={() => setImportOpen(true)}>
                        <Upload /> {t('lists.import.menu')}
                      </Button>
                    ) : null}
                  </div>
                ) : undefined
              }
            />
          )
        ) : (
          <div className={cn('transition-opacity', listQ.isPlaceholderData && 'opacity-60')}>
            {binMode ? <p className="border-b bg-muted/30 px-4 py-2 text-xs text-muted-foreground">{t('lists.bin.hint')}</p> : null}
            <RecordTable
              object={object}
              meta={meta}
              columns={columns}
              rows={rows}
              sorts={sorts}
              onSort={(k) => {
                const cur = sorts[0];
                setDraft((d) => ({ ...d, sorts: [{ field: k, dir: cur?.field === k && cur.dir === 'asc' ? 'desc' : 'asc' }] }));
              }}
              showWorkspace={showWsColumn}
              selected={selected}
              onSelect={(s) => {
                setSelected(s);
                setAllMatching(false);
              }}
              aggregates={listQ.data?.aggregates}
              compact={draft.density === 'compact'}
              binMode={binMode}
            />
            <RecordCards object={object} meta={meta} columns={columns} rows={rows} showWorkspace={showWsColumn} binMode={binMode} />
            <div className="flex items-center justify-between gap-3 border-t px-4 py-2.5">
              <p className="text-[13px] tabular-nums text-muted-foreground">{t('records.list.range', { from, to, total })}</p>
              <div className="flex items-center gap-1">
                <Button variant="outline" size="icon-sm" disabled={page <= 1} onClick={() => patchParams({ page: page - 1 <= 1 ? null : String(page - 1) })} aria-label={t('records.list.prev')}>
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
        <NewRecordDialog object={object} meta={meta} open={newOpen} onOpenChange={setNewOpen} defaults={newDefaults}
          note={isOwner && ws !== PLATFORM_WS ? t('records.new.platformNote') : undefined} />
      ) : null}
      {meta ? <ImportDialog object={object} meta={meta} open={importOpen} onOpenChange={setImportOpen} /> : null}
      {meta && viewDialog ? (
        <ViewDialog
          key={viewDialog.mode + (viewDialog.view?.id ?? '')}
          meta={meta}
          open
          onOpenChange={(o) => !o && setViewDialog(null)}
          canShare={Boolean(viewsQ.data?.canShare)}
          busy={saveView.isPending}
          title={t(`lists.views.dialog.${viewDialog.mode}`)}
          initial={
            viewDialog.mode === 'rename' && viewDialog.view
              ? { name: viewDialog.view.name, kind: viewDialog.view.kind, visibility: viewDialog.view.visibility, definition: viewDialog.view.definition }
              : viewDialog.mode === 'copy' && viewDialog.view
                ? { name: t('lists.views.copyOf', { name: viewDialog.view.name }), kind: viewDialog.view.kind, visibility: 'personal', definition: viewDialog.view.definition }
                : { name: '', kind, visibility: 'personal', definition: draft }
          }
          onSave={(v) =>
            viewDialog.mode === 'rename' && viewDialog.view
              ? saveView.mutate({ mode: 'rename', view: viewDialog.view, name: v.name, kind: v.kind, visibility: v.visibility, definition: v.definition })
              : saveView.mutate({ mode: viewDialog.mode, ...v })
          }
        />
      ) : null}
    </PageContainer>
  );
}

function RecordCards({ object, meta, columns, rows, showWorkspace, binMode }: { object: ObjectKey; meta: ObjectMeta; columns: FieldDef[]; rows: RecordRow[]; showWorkspace?: boolean; binMode?: boolean }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const detailCols = columns.filter((c) => c.key !== meta.statusField).slice(0, 3);
  return (
    <ul className="divide-y sm:hidden">
      {rows.map((r) => {
        const st = meta.statusField ? statusOption(meta, r.values[meta.statusField]) : undefined;
        const body = (
          <>
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
          </>
        );
        return (
          <li key={r.id}>
            {binMode ? <div className="px-4 py-3">{body}</div> : <Link to={rowHref(scope, object, r)} className="block px-4 py-3 hover:bg-muted/50">{body}</Link>}
          </li>
        );
      })}
    </ul>
  );
}

function ListSkeleton({ columns }: { columns: number }) {
  return (
    <div aria-busy="true">
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="flex items-center gap-6 border-b px-4 py-3 last:border-0">
          <div className="w-48 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-20" />
          </div>
          {Array.from({ length: columns }).map((__, j) => (
            <Skeleton key={j} className="hidden h-3.5 flex-1 sm:block" />
          ))}
        </div>
      ))}
    </div>
  );
}
