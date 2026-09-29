import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowLeft, ArrowRight, ChevronRight, LayoutGrid, Maximize2, Minimize2, Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-react';
import { reportsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { DashboardDetail, DashboardWidget, ReportChart as ChartType } from '@crm/api/types';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, ConfirmDialog, PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { ReportChart } from './report-chart';
import { reportKeys } from './reports-page';

const CHARTS: ChartType[] = ['bar', 'line', 'donut', 'number', 'table'];

/** /crm/w/:ws/dashboards */
export function DashboardsPage() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const qc = useQueryClient();
  const navigate = useNavigate();
  useDocumentTitle(t('reports.dashboardsTitle'));
  const q = useQuery({ queryKey: reportKeys.dashboards(code), queryFn: () => reportsApi(code).dashboards() });
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const create = useMutation({
    mutationFn: () => reportsApi(code).createDashboard({ name: name.trim() }),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: reportKeys.all(code) });
      navigate(`${workspaceBase(code)}/dashboards/${d.id}`);
    },
    onError: (e) => toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError'))
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) create.mutate();
  };
  return (
    <PageContainer>
      <PageHeader
        title={t('reports.dashboardsTitle')}
        description={t('reports.dashboardsSubtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <LayoutGrid className="size-5" aria-hidden />
          </span>
        }
        actions={
          <Button onClick={() => setCreating(true)}>
            <Plus /> {t('reports.newDashboard')}
          </Button>
        }
      />
      <Card className="overflow-hidden">
        {q.isError ? (
          <ErrorState title={t('reports.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        ) : !q.data ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </div>
        ) : q.data.length === 0 ? (
          <EmptyState
            icon={LayoutGrid}
            title={t('reports.noDashboardsTitle')}
            body={t('reports.noDashboardsBody')}
            action={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus /> {t('reports.newDashboard')}
              </Button>
            }
          />
        ) : (
          <ul className="divide-y">
            {q.data.map((d) => (
              <li key={d.id}>
                <Link to={`${workspaceBase(code)}/dashboards/${d.id}`} className="group flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
                  <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
                    <LayoutGrid className="size-4" aria-hidden />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-semibold group-hover:text-primary">{d.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {t('reports.widgetsCount', { count: d.widgets })} · {t('reports.by', { name: d.ownerName || '—' })} · {relativeTime(d.updatedAt)}
                    </p>
                  </div>
                  <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="max-w-md p-5">
          <DialogTitle className="pr-8 text-base font-semibold">{t('reports.newDashboard')}</DialogTitle>
          <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('reports.newDashboardBody')}</DialogDescription>
          <form onSubmit={submit} className="mt-4 space-y-4">
            <Field label={t('reports.name')}>
              <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={120} autoFocus placeholder={t('reports.dashboardPlaceholder')} />
            </Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => setCreating(false)}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
                {t('reports.create')}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </PageContainer>
  );
}

/** /crm/w/:ws/dashboards/:id */
export function DashboardPage() {
  const { id = '' } = useParams();
  return <DashboardView key={id} id={id} />;
}

type Draft = Pick<DashboardWidget, 'reportId' | 'chart' | 'size'>;

function DashboardView({ id }: { id: string }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const api = reportsApi(code);
  const q = useQuery({ queryKey: reportKeys.dashboard(code, id), queryFn: () => api.dashboard(id) });
  const d = q.data;
  useDocumentTitle(d?.name ?? t('reports.dashboardsTitle'));
  const [editing, setEditing] = useState(false);
  const [adding, setAdding] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [rename, setRename] = useState<string | null>(null);

  const drafts = (w: DashboardWidget[]): Draft[] => w.map((x) => ({ reportId: x.reportId, chart: x.chart, size: x.size }));
  const saveWidgets = useMutation({
    mutationFn: (body: { widgets?: Draft[]; name?: string }) => api.updateDashboard(id, body),
    onSuccess: (next) => {
      qc.setQueryData<DashboardDetail>(reportKeys.dashboard(code, id), next);
      void qc.invalidateQueries({ queryKey: reportKeys.dashboards(code) });
    },
    onError: (e) => toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError'))
  });
  const remove = useMutation({
    mutationFn: () => api.removeDashboard(id),
    onSuccess: () => {
      toast.success(t('reports.dashboardDeleted'));
      void qc.invalidateQueries({ queryKey: reportKeys.all(code) });
      navigate(`${workspaceBase(code)}/dashboards`, { replace: true });
    }
  });

  if (q.isError) {
    return (
      <PageContainer>
        <ErrorState title={t('reports.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
      </PageContainer>
    );
  }
  if (!d) {
    return (
      <PageContainer wide>
        <Skeleton className="h-8 w-60" />
        <div className="mt-4 grid gap-4 md:grid-cols-2">
          <Skeleton className="h-64" />
          <Skeleton className="h-64" />
        </div>
      </PageContainer>
    );
  }
  const list = drafts(d.widgets);
  const update = (next: Draft[]) => saveWidgets.mutate({ widgets: next });
  const move = (i: number, by: number) => {
    const j = i + by;
    if (j < 0 || j >= list.length) return;
    const n = [...list];
    [n[i], n[j]] = [n[j]!, n[i]!];
    update(n);
  };

  return (
    <PageContainer wide>
      <Breadcrumbs items={[{ label: t('reports.dashboardsTitle'), to: `${workspaceBase(code)}/dashboards` }, { label: d.name }]} />
      <div className="mt-3 flex flex-col gap-3 sm:flex-row sm:items-center">
        {rename !== null ? (
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (rename.trim()) saveWidgets.mutate({ name: rename.trim() }, { onSuccess: () => setRename(null) });
            }}
          >
            <Input value={rename} onChange={(e) => setRename(e.target.value)} maxLength={120} autoFocus className="sm:w-80" />
            <Button type="submit" size="sm" loading={saveWidgets.isPending}>
              {t('common.save')}
            </Button>
          </form>
        ) : (
          <h1 className="text-xl font-semibold tracking-tight">{d.name}</h1>
        )}
        <div className="flex flex-wrap gap-2 sm:ml-auto">
          <Button variant="outline" size="sm" onClick={() => void q.refetch()} loading={q.isFetching}>
            {!q.isFetching ? <RefreshCw /> : null} {t('reports.refresh')}
          </Button>
          {d.canEdit ? (
            <>
              <Button variant={editing ? 'primary' : 'outline'} size="sm" onClick={() => setEditing((e) => !e)}>
                <Pencil /> {editing ? t('reports.doneEditing') : t('reports.edit')}
              </Button>
              {editing ? (
                <>
                  <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
                    <Plus /> {t('reports.addReport')}
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setRename(d.name)}>
                    {t('reports.rename')}
                  </Button>
                  <Button size="sm" variant="outline" className="text-danger" onClick={() => setConfirmDelete(true)}>
                    <Trash2 />
                  </Button>
                </>
              ) : null}
            </>
          ) : null}
        </div>
      </div>

      {d.widgets.length === 0 ? (
        <Card className="mt-4">
          <EmptyState
            icon={LayoutGrid}
            title={t('reports.emptyDashboardTitle')}
            body={t('reports.emptyDashboardBody')}
            action={
              d.canEdit ? (
                <Button size="sm" onClick={() => setAdding(true)}>
                  <Plus /> {t('reports.addReport')}
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-6">
          {d.widgets.map((w, i) => (
            <Card key={`${w.reportId}-${i}`} className={cn('min-w-0 overflow-hidden', w.size === 'sm' ? 'md:col-span-2' : w.size === 'lg' ? 'md:col-span-6' : 'md:col-span-3')}>
              <CardHeader
                className="py-3"
                title={
                  w.report ? (
                    <Link to={`${workspaceBase(code)}/reports/${w.reportId}`} className="text-[13px] hover:text-primary hover:underline">
                      {w.report.name}
                    </Link>
                  ) : (
                    t('reports.missing')
                  )
                }
                description={w.result ? `${w.result.objectLabel} · ${w.result.measureLabel}` : undefined}
                actions={
                  editing ? (
                    <div className="flex items-center gap-0.5">
                      <Select
                        value={w.chart || w.report?.definition.chart || 'bar'}
                        onChange={(e) => update(list.map((x, j) => (j === i ? { ...x, chart: e.target.value as ChartType } : x)))}
                        options={CHARTS.map((c) => ({ value: c, label: t(`reports.charts.${c}`) }))}
                        className="w-24"
                        aria-label={t('reports.chart')}
                      />
                      <Button variant="ghost" size="icon-sm" aria-label={t('reports.moveLeft')} onClick={() => move(i, -1)} disabled={i === 0}>
                        <ArrowLeft />
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label={t('reports.moveRight')} onClick={() => move(i, 1)} disabled={i === list.length - 1}>
                        <ArrowRight />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t('reports.resize')}
                        onClick={() => update(list.map((x, j) => (j === i ? { ...x, size: x.size === 'sm' ? 'md' : x.size === 'md' ? 'lg' : 'sm' } : x)))}
                      >
                        {w.size === 'lg' ? <Minimize2 /> : <Maximize2 />}
                      </Button>
                      <Button variant="ghost" size="icon-sm" aria-label={t('reports.removeWidget')} onClick={() => update(list.filter((_, j) => j !== i))}>
                        <X />
                      </Button>
                    </div>
                  ) : null
                }
              />
              <div className="border-t px-4 py-3">
                {w.error ? (
                  <Alert tone="warning">{w.error}</Alert>
                ) : w.result ? (
                  <ReportChart result={w.result} chart={(w.chart || undefined) as ChartType | undefined} compact={w.size !== 'lg'} />
                ) : (
                  <Skeleton className="h-40" />
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {adding ? (
        <AddReportDialog
          onClose={() => setAdding(false)}
          onAdd={(reportId, chart) => {
            update([...list, { reportId, chart, size: 'md' }]);
            setAdding(false);
          }}
        />
      ) : null}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('reports.deleteDashboardTitle', { name: d.name })}
        body={t('reports.deleteDashboardBody')}
        confirmLabel={t('reports.delete')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => remove.mutate()}
      />
    </PageContainer>
  );
}

function AddReportDialog({ onClose, onAdd }: { onClose: () => void; onAdd: (reportId: string, chart: ChartType | '') => void }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const reports = useQuery({ queryKey: reportKeys.list(code), queryFn: () => reportsApi(code).list() });
  const [pick, setPick] = useState('');
  const [chart, setChart] = useState<ChartType | ''>('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('reports.addReport')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('reports.addReportBody')}</DialogDescription>
        <div className="mt-4 space-y-4">
          {reports.data && reports.data.length === 0 ? (
            <Alert tone="info">
              {t('reports.noReportsYet')}{' '}
              <Link to={`${workspaceBase(code)}/reports/new`} className="font-medium text-primary hover:underline">
                {t('reports.new')}
              </Link>
            </Alert>
          ) : (
            <Field label={t('reports.report')}>
              <Select
                value={pick}
                onChange={(e) => setPick(e.target.value)}
                placeholder={t('reports.pick')}
                options={(reports.data ?? []).map((r) => ({ value: r.id, label: `${r.name} · ${r.objectLabel}` }))}
              />
            </Field>
          )}
          <Field label={t('reports.chart')}>
            <Select
              value={chart}
              onChange={(e) => setChart(e.target.value as ChartType | '')}
              options={[{ value: '', label: t('reports.chartFromReport') }, ...CHARTS.map((c) => ({ value: c, label: t(`reports.charts.${c}`) }))]}
            />
          </Field>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button disabled={!pick} onClick={() => onAdd(pick, chart)}>
              <Plus /> {t('reports.add')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
