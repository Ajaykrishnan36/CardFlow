import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { BarChart3, LayoutGrid, Plus, Save, Trash2, X } from 'lucide-react';
import { reportsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { FieldDef, FilterOp, ReportChart as ChartType, ReportDefinition, ReportFilter, ReportObject } from '@crm/api/types';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, PageContainer } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { ReportChart } from './report-chart';
import { reportKeys } from './reports-page';

const CHARTS: ChartType[] = ['bar', 'line', 'donut', 'number', 'table'];
const NUMBER = new Set(['number', 'currency', 'percent']);
const DATE = new Set(['date', 'datetime']);

function opsFor(f?: FieldDef): FilterOp[] {
  if (!f) return ['eq'];
  if (NUMBER.has(f.type)) return ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'empty', 'notEmpty'];
  if (DATE.has(f.type)) return ['lastDays', 'gte', 'lte', 'eq', 'empty', 'notEmpty'];
  if (f.type === 'boolean') return ['eq'];
  if (f.type === 'select' || f.type === 'lookup') return ['eq', 'neq', 'empty', 'notEmpty'];
  return ['contains', 'eq', 'neq', 'empty', 'notEmpty'];
}

const emptyDef = (): ReportDefinition => ({ filters: [], groupBy: '', dateBucket: '', measure: { fn: 'count' }, chart: 'bar', limit: 25 });

/** /crm/w/:ws/reports/new and /reports/:id — build a report with a live preview. */
export function ReportBuilderPage() {
  const { id = 'new' } = useParams();
  return <Builder key={id} id={id} />;
}

function useDebounced<T>(value: T, ms = 350): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const h = setTimeout(() => setV(value), ms);
    return () => clearTimeout(h);
  }, [value, ms]);
  return v;
}

function Builder({ id }: { id: string }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const api = reportsApi(code);
  const isNew = id === 'new';
  const objects = useQuery({ queryKey: reportKeys.objects(code), queryFn: () => api.objects(), staleTime: 60_000 });
  const saved = useQuery({ queryKey: reportKeys.one(code, id), queryFn: () => api.get(id), enabled: !isNew });
  const [name, setName] = useState('');
  const [object, setObject] = useState('');
  const [def, setDef] = useState<ReportDefinition>(emptyDef);
  const [loaded, setLoaded] = useState(isNew);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [dashPick, setDashPick] = useState(false);

  useEffect(() => {
    if (saved.data && !loaded) {
      setName(saved.data.name);
      setObject(saved.data.object);
      setDef({ ...emptyDef(), ...saved.data.definition });
      setLoaded(true);
    }
  }, [saved.data, loaded]);
  useEffect(() => {
    if (isNew && !object && objects.data?.length) setObject(objects.data[0]!.key);
  }, [isNew, object, objects.data]);
  useDocumentTitle(name || t('reports.newTitle'));

  const obj: ReportObject | undefined = objects.data?.find((o) => o.key === object);
  const fields = useMemo(() => obj?.fields ?? [], [obj]);
  const fieldByKey = (k?: string) => fields.find((f) => f.key === k);
  const groupable = fields.filter((f) => f.type !== 'textarea' && f.type !== 'multiselect' && f.key !== 'code');
  const numeric = fields.filter((f) => NUMBER.has(f.type));
  const groupField = fieldByKey(def.groupBy);

  const debounced = useDebounced({ object, def });
  const preview = useQuery({
    queryKey: ['workspace', code, 'reports', 'preview', debounced],
    queryFn: () => api.run(debounced.object, debounced.def),
    enabled: Boolean(debounced.object) && loaded,
    retry: false
  });

  const save = useMutation({
    mutationFn: () => (isNew ? api.create({ name: name.trim(), object, definition: def }) : api.update(id, { name: name.trim(), object, definition: def })),
    onSuccess: (r) => {
      toast.success(t('reports.savedToast', { name: r.name }));
      void qc.invalidateQueries({ queryKey: reportKeys.all(code) });
      if (isNew) navigate(`${workspaceBase(code)}/reports/${r.id}`, { replace: true });
    },
    onError: (e) => toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError'))
  });
  const remove = useMutation({
    mutationFn: () => api.remove(id),
    onSuccess: () => {
      toast.success(t('reports.deletedToast'));
      void qc.invalidateQueries({ queryKey: reportKeys.all(code) });
      navigate(`${workspaceBase(code)}/reports`, { replace: true });
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const setFilter = (i: number, patch: Partial<ReportFilter>) => setDef((d) => ({ ...d, filters: d.filters.map((f, j) => (j === i ? { ...f, ...patch } : f)) }));
  const canEdit = isNew || Boolean(saved.data?.canEdit);

  if (saved.isError) {
    return (
      <PageContainer>
        <ErrorState title={t('reports.loadError')} message={isApiError(saved.error) ? saved.error.message : undefined} onRetry={() => void saved.refetch()} />
      </PageContainer>
    );
  }
  if (!objects.data || !loaded) {
    return (
      <PageContainer wide>
        <Skeleton className="h-8 w-60" />
        <Skeleton className="mt-4 h-96 w-full" />
      </PageContainer>
    );
  }
  if (objects.data.length === 0) {
    return (
      <PageContainer>
        <Alert tone="warning">{t('reports.noObjects')}</Alert>
      </PageContainer>
    );
  }

  return (
    <PageContainer wide>
      <Breadcrumbs items={[{ label: t('reports.title'), to: `${workspaceBase(code)}/reports` }, { label: name || t('reports.newTitle') }]} />
      <div className="mt-3 flex flex-col gap-3 sm:flex-row sm:items-center">
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t('reports.namePlaceholder')}
          maxLength={120}
          disabled={!canEdit}
          className="text-base font-semibold sm:max-w-md"
          aria-label={t('reports.name')}
        />
        <div className="flex gap-2 sm:ml-auto">
          {!isNew ? (
            <Button variant="outline" onClick={() => setDashPick(true)}>
              <LayoutGrid /> {t('reports.addToDashboard')}
            </Button>
          ) : null}
          {!isNew && canEdit ? (
            <Button variant="outline" className="text-danger" onClick={() => setConfirmDelete(true)}>
              <Trash2 /> {t('reports.delete')}
            </Button>
          ) : null}
          {canEdit ? (
            <Button onClick={() => save.mutate()} loading={save.isPending} disabled={!name.trim() || !object}>
              <Save /> {t('reports.save')}
            </Button>
          ) : null}
        </div>
      </div>
      {!canEdit ? <Alert tone="info" className="mt-3">{t('reports.readOnly', { name: saved.data?.ownerName })}</Alert> : null}

      <div className="mt-4 grid gap-4 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <Card className="h-fit">
          <CardHeader title={t('reports.build')} />
          <fieldset disabled={!canEdit} className="space-y-4 px-4 pb-4">
            <Field label={t('reports.object')}>
              <Select
                value={object}
                onChange={(e) => {
                  setObject(e.target.value);
                  setDef(emptyDef());
                }}
                options={objects.data.map((o) => ({ value: o.key, label: o.label }))}
              />
            </Field>

            <div>
              <p className="mb-1.5 text-[13px] font-medium">{t('reports.filters')}</p>
              <ul className="space-y-2">
                {def.filters.map((fl, i) => {
                  const f = fieldByKey(fl.field);
                  const ops = opsFor(f);
                  const needsValue = fl.op !== 'empty' && fl.op !== 'notEmpty';
                  return (
                    <li key={i} className="space-y-1.5 rounded-md border bg-muted/30 p-2">
                      <div className="flex gap-1.5">
                        <Select
                          value={fl.field}
                          onChange={(e) => setFilter(i, { field: e.target.value, op: opsFor(fieldByKey(e.target.value))[0], value: '' })}
                          options={fields.map((x) => ({ value: x.key, label: x.label }))}
                          className="min-w-0 flex-1"
                          aria-label={t('reports.filterField')}
                        />
                        <Button variant="ghost" size="icon-sm" aria-label={t('reports.removeFilter')} onClick={() => setDef((d) => ({ ...d, filters: d.filters.filter((_, j) => j !== i) }))}>
                          <X />
                        </Button>
                      </div>
                      <div className="flex gap-1.5">
                        <Select
                          value={fl.op}
                          onChange={(e) => setFilter(i, { op: e.target.value as FilterOp })}
                          options={ops.map((o) => ({ value: o, label: t(`reports.ops.${o}`) }))}
                          className="w-36 shrink-0"
                          aria-label={t('reports.filterOp')}
                        />
                        {needsValue ? <FilterValue field={f} op={fl.op} value={fl.value} onChange={(v) => setFilter(i, { value: v })} /> : null}
                      </div>
                    </li>
                  );
                })}
              </ul>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => {
                  const f = fields.find((x) => x.key !== 'code') ?? fields[0];
                  if (f) setDef((d) => ({ ...d, filters: [...d.filters, { field: f.key, op: opsFor(f)[0]!, value: '' }] }));
                }}
              >
                <Plus /> {t('reports.addFilter')}
              </Button>
            </div>

            <Field label={t('reports.groupBy')}>
              <Select
                value={def.groupBy ?? ''}
                onChange={(e) => setDef((d) => ({ ...d, groupBy: e.target.value, dateBucket: DATE.has(fieldByKey(e.target.value)?.type ?? '') ? 'month' : '' }))}
                placeholder={t('reports.noGroup')}
                options={groupable.map((x) => ({ value: x.key, label: x.label }))}
              />
            </Field>
            {groupField && DATE.has(groupField.type) ? (
              <Field label={t('reports.bucket')}>
                <Select
                  value={def.dateBucket || 'month'}
                  onChange={(e) => setDef((d) => ({ ...d, dateBucket: e.target.value as ReportDefinition['dateBucket'] }))}
                  options={(['day', 'week', 'month', 'quarter', 'year'] as const).map((b) => ({ value: b, label: t(`reports.buckets.${b}`) }))}
                />
              </Field>
            ) : null}

            <div className="grid grid-cols-2 gap-2">
              <Field label={t('reports.measure')}>
                <Select
                  value={def.measure.fn}
                  onChange={(e) => {
                    const fn = e.target.value as ReportDefinition['measure']['fn'];
                    setDef((d) => ({ ...d, measure: { fn, field: fn === 'count' ? undefined : d.measure.field ?? numeric[0]?.key } }));
                  }}
                  options={(['count', 'sum', 'avg', 'min', 'max'] as const)
                    .filter((f) => f === 'count' || numeric.length > 0)
                    .map((f) => ({ value: f, label: t(`reports.fns.${f}`) }))}
                />
              </Field>
              {def.measure.fn !== 'count' ? (
                <Field label={t('reports.ofField')}>
                  <Select
                    value={def.measure.field ?? ''}
                    onChange={(e) => setDef((d) => ({ ...d, measure: { ...d.measure, field: e.target.value } }))}
                    options={numeric.map((x) => ({ value: x.key, label: x.label }))}
                  />
                </Field>
              ) : null}
            </div>

            <div>
              <p className="mb-1.5 text-[13px] font-medium">{t('reports.chart')}</p>
              <div role="radiogroup" className="grid grid-cols-5 gap-1">
                {CHARTS.map((c) => (
                  <button
                    key={c}
                    type="button"
                    role="radio"
                    aria-checked={def.chart === c}
                    onClick={() => setDef((d) => ({ ...d, chart: c }))}
                    className={cn(
                      'rounded-md border px-1 py-1.5 text-[11px] font-medium transition-colors',
                      def.chart === c ? 'border-primary bg-primary-soft text-primary' : 'text-muted-foreground hover:bg-muted'
                    )}
                  >
                    {t(`reports.charts.${c}`)}
                  </button>
                ))}
              </div>
            </div>
          </fieldset>
        </Card>

        <Card className="min-w-0">
          <CardHeader
            title={
              <span className="flex items-center gap-2">
                <BarChart3 className="size-4 text-muted-foreground" aria-hidden /> {obj?.label ?? ''}
                {preview.data?.groupLabel ? <span className="font-normal text-muted-foreground">· {t('reports.byGroup', { group: preview.data.groupLabel })}</span> : null}
              </span>
            }
            description={preview.data ? t('reports.previewMeta', { count: preview.data.count, measure: preview.data.measureLabel }) : t('reports.previewHint')}
          />
          <div className="px-4 pb-4">
            {preview.isError ? (
              <Alert tone="warning">{isApiError(preview.error) ? (Object.values(preview.error.fieldErrors)[0] ?? preview.error.message) : t('common.genericError')}</Alert>
            ) : !preview.data ? (
              <Skeleton className="h-64 w-full" />
            ) : (
              <div className={cn('transition-opacity', preview.isFetching && 'opacity-60')}>
                <ReportChart result={preview.data} chart={def.chart} />
              </div>
            )}
          </div>
        </Card>
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('reports.deleteTitle', { name })}
        body={t('reports.deleteBody')}
        confirmLabel={t('reports.delete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
      {dashPick ? <AddToDashboard reportId={id} chart={def.chart} onClose={() => setDashPick(false)} /> : null}
    </PageContainer>
  );
}

function FilterValue({ field, op, value, onChange }: { field?: FieldDef; op: FilterOp; value: unknown; onChange: (v: string) => void }) {
  const { t } = useTranslation();
  const v = value === undefined || value === null ? '' : String(value);
  if (op === 'lastDays') return <Input type="number" min={1} value={v} onChange={(e) => onChange(e.target.value)} placeholder={t('reports.days')} className="min-w-0 flex-1" />;
  if (field?.type === 'select')
    return <Select value={v} onChange={(e) => onChange(e.target.value)} placeholder={t('reports.pick')} options={(field.options ?? []).map((o) => ({ value: o.value, label: o.label }))} className="min-w-0 flex-1" />;
  if (field?.type === 'boolean')
    return (
      <Select
        value={v || 'true'}
        onChange={(e) => onChange(e.target.value)}
        options={[
          { value: 'true', label: t('reports.yes') },
          { value: 'false', label: t('reports.no') }
        ]}
        className="min-w-0 flex-1"
      />
    );
  const type = field && DATE.has(field.type) ? 'date' : field && NUMBER.has(field.type) ? 'number' : 'text';
  return <Input type={type} value={v} onChange={(e) => onChange(e.target.value)} className="min-w-0 flex-1" aria-label={t('reports.value')} />;
}

function AddToDashboard({ reportId, chart, onClose }: { reportId: string; chart: ChartType; onClose: () => void }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const api = reportsApi(code);
  const list = useQuery({ queryKey: reportKeys.dashboards(code), queryFn: () => api.dashboards() });
  const add = useMutation({
    mutationFn: async (target: string) => {
      if (target === '__new') return api.createDashboard({ name: t('reports.newDashboardName'), widgets: [{ reportId, chart, size: 'md' }] });
      const d = await api.dashboard(target);
      return api.updateDashboard(target, { widgets: [...d.widgets.map((w) => ({ reportId: w.reportId, chart: w.chart, size: w.size })), { reportId, chart, size: 'md' }] });
    },
    onSuccess: (d) => {
      toast.success(t('reports.addedToDashboard', { name: d.name }));
      void qc.invalidateQueries({ queryKey: reportKeys.all(code) });
      navigate(`${workspaceBase(code)}/dashboards/${d.id}`);
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={t('reports.addToDashboard')}
      body={
        <span className="mt-2 block">
          {!list.data ? (
            <span className="text-muted-foreground">{t('common.loading')}</span>
          ) : (
            <span className="flex flex-col gap-1.5">
              {list.data.map((d) => (
                <Button key={d.id} variant="outline" className="justify-start" disabled={!d.canEdit || add.isPending} onClick={() => add.mutate(d.id)}>
                  <LayoutGrid /> {d.name}
                </Button>
              ))}
              <Button variant="outline" className="justify-start" disabled={add.isPending} onClick={() => add.mutate('__new')}>
                <Plus /> {t('reports.newDashboard')}
              </Button>
            </span>
          )}
        </span>
      }
      confirmLabel={t('common.close')}
      onConfirm={onClose}
    />
  );
}
