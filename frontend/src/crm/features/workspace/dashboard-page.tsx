import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Link, Navigate } from 'react-router-dom';
import { ArrowRight, ChevronRight, LayoutGrid, Plus, RefreshCw, ShieldCheck } from 'lucide-react';
import { workspaceApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ObjectKey, WorkspaceDashboard } from '@crm/api/types';
import { useMe } from '@crm/auth/session';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { timeOfDayGreeting } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { KpiCard, KpiSkeleton } from '@crm/features/owner/dashboard-page';
import { listHref, recordHref, useRecordScope, workspaceDashboardKey } from '@crm/features/records/record-scope';
import { guessTone, humanize, useObjectIcon } from '@crm/features/records/use-object-meta';
import { firstModulePath, hasDashboard, readableObjects, ticketAccess, useWorkspace, workspaceBase } from './workspace-context';
import { canAdminister } from './admin/admin-utils';
import { SummarySection } from './summary-section';
import { GettingStartedPanel } from './getting-started';

/** /crm/w/:ws/home — the member's dashboard (or the first module when dashboard.view isn't granted). */
export function WorkspaceDashboardPage() {
  const { code, context } = useWorkspace();
  const readable = readableObjects(context);
  if (!hasDashboard(context, code)) {
    const first = firstModulePath(context, code);
    if (first) return <Navigate to={first} replace />;
    return <NoModulesState />;
  }
  return <DashboardView code={code} workspaceName={context.workspace.name} readable={readable} />;
}

function DashboardView({ code, workspaceName, readable }: { code: string; workspaceName: string; readable: ObjectKey[] }) {
  const { t } = useTranslation();
  const { data: me } = useMe();
  const scope = useRecordScope();
  const { context } = useWorkspace();
  useDocumentTitle(t('workspaceApp.dashboard.docTitle', { workspace: workspaceName }));
  const q = useQuery({ queryKey: workspaceDashboardKey(code), queryFn: () => workspaceApi.dashboard(code), staleTime: 60_000 });
  const firstName = me?.identity.displayName.split(' ')[0] ?? '';
  const greetingKey = { morning: 'greetingMorning', afternoon: 'greetingAfternoon', evening: 'greetingEvening' }[timeOfDayGreeting()];
  // The selected app's menu decides what the dashboard shows (D-73).
  const navPaths = new Set(context.navigation.map((n) => n.path));
  const inApp = (path?: string) => !path || navPaths.has(path.split('?')[0]!);
  const creatable = readable.filter((o) => scope.can(o, 'create') && inApp(listHref(scope, o)));
  const noModules = readable.length === 0 && !ticketAccess(context).read;
  const isAdmin = canAdminister(context);

  // Lost dashboard.view since the context loaded: fall through to the first module.
  if (isApiError(q.error) && q.error.status === 403) {
    const first = firstModulePath(context, code);
    return first ? <Navigate to={first} replace /> : <NoModulesState />;
  }

  return (
    <div className="mx-auto w-full max-w-[1240px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[22px] font-semibold tracking-tight text-foreground sm:text-2xl">{t(`workspaceApp.dashboard.${greetingKey}`, { name: firstName })}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <span>{t('workspaceApp.dashboard.subtitle', { workspace: workspaceName })}</span>
            {context.role ? <Badge tone="primary">{context.role.name}</Badge> : null}
          </p>
        </div>
        {!noModules ? (
          <Button variant="outline" size="sm" onClick={() => void q.refetch()} loading={q.isFetching && !q.isPending}>
            {!q.isFetching || q.isPending ? <RefreshCw /> : null} {t('workspaceApp.dashboard.refresh')}
          </Button>
        ) : null}
      </div>

      {creatable.length || isAdmin ? (
        <nav aria-label={t('workspaceApp.dashboard.quickActions')} className="-mt-2 mb-6 flex flex-wrap items-center gap-2">
          {creatable.map((o) => (
            <Button key={o} asChild variant="outline" size="sm">
              <Link to={`${listHref(scope, o)}?new=1`}>
                <Plus /> {t(`workspaceApp.dashboard.new.${o}`)}
              </Link>
            </Button>
          ))}
          {isAdmin ? (
            <Button asChild variant="outline" size="sm">
              <Link to={`${workspaceBase(code)}/settings/access`}>
                <ShieldCheck /> {t('workspaceApp.dashboard.manageAccess')}
              </Link>
            </Button>
          ) : null}
        </nav>
      ) : null}

      {noModules ? (
        <NoModulesCard />
      ) : q.isError ? (
        <Card>
          <ErrorState
            title={t('workspaceApp.dashboard.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : (
        <div className="space-y-6">
          <GettingStartedPanel code={code} />
          <SummarySection code={code} />
          {!q.data || q.data.kpis.length ? (
            <section className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4" aria-label={t('workspaceApp.dashboard.keyMetrics')}>
              {q.data ? q.data.kpis.filter((k) => inApp(k.path)).map((k) => <KpiCard key={k.key} kpi={k} />) : Array.from({ length: 4 }).map((_, i) => <KpiSkeleton key={i} />)}
            </section>
          ) : null}
          <RecentGrid data={q.data ? { ...q.data, recent: q.data.recent.filter((g) => inApp(listHref(scope, g.object))) } : undefined} readable={readable} />
        </div>
      )}
    </div>
  );
}

function RecentGrid({ data, readable }: { data?: WorkspaceDashboard; readable: ObjectKey[] }) {
  const { t } = useTranslation();
  if (!data) {
    return (
      <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        {readable.map((o) => (
          <Card key={o} className="space-y-3 p-4" aria-busy="true">
            <Skeleton className="h-4 w-28" />
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="space-y-1.5">
                <Skeleton className="h-3.5 w-3/5" />
                <Skeleton className="h-3 w-2/5" />
              </div>
            ))}
          </Card>
        ))}
      </div>
    );
  }
  if (data.recent.length === 0) return null;
  return (
    <section aria-label={t('workspaceApp.dashboard.recentTitle')}>
      <h2 className="mb-3 text-[13px] font-semibold text-foreground">{t('workspaceApp.dashboard.recentTitle')}</h2>
      <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        {data.recent.map((g) => (
          <RecentCard key={g.object} object={g.object} label={g.label} rows={g.rows} />
        ))}
      </div>
    </section>
  );
}

function RecentCard({ object, label, rows }: { object: ObjectKey; label: string; rows: WorkspaceDashboard['recent'][number]['rows'] }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const Icon = useObjectIcon(object);
  const canCreate = scope.can(object, 'create');
  return (
    <Card className="flex min-w-0 flex-col overflow-hidden">
      <CardHeader
        className="px-4 py-3"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <span className="grid size-6 place-items-center rounded-md bg-primary-soft text-primary">
              <Icon className="size-3.5" aria-hidden />
            </span>
            {label}
          </span>
        }
        actions={
          <Link to={listHref(scope, object)} className="group inline-flex items-center gap-1 rounded text-xs font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {t('workspaceApp.dashboard.viewAll')}
            <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden />
          </Link>
        }
      />
      {rows.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center px-4 py-8 text-center">
          <p className="text-[13px] text-muted-foreground">{t('workspaceApp.dashboard.recentEmpty', { label: label.toLowerCase() })}</p>
          {canCreate ? (
            <Button asChild variant="outline" size="sm" className="mt-3">
              <Link to={`${listHref(scope, object)}?new=1`}>
                <Plus /> {t(`workspaceApp.dashboard.new.${object}`, { defaultValue: t('workspaceApp.dashboard.newRecord', { label: singular(label) }) })}
              </Link>
            </Button>
          ) : null}
        </div>
      ) : (
        <ul className="divide-y">
          {rows.map((r) => (
            <li key={r.id}>
              <Link to={recordHref(scope, object, r.id)} className="group flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none">
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-medium text-foreground group-hover:text-primary">{r.title || t('records.common.untitled')}</p>
                  {r.code || r.subtitle ? (
                    <p className="truncate text-xs text-muted-foreground">
                      {r.code ? <span className="font-mono">{r.code}</span> : null}
                      {r.code && r.subtitle ? ' · ' : null}
                      {r.subtitle}
                    </p>
                  ) : null}
                </div>
                {r.status ? <Badge tone={guessTone(r.status)}>{humanize(r.status)}</Badge> : null}
                <ChevronRight className="size-4 shrink-0 text-muted-foreground/60 sm:hidden" aria-hidden />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function NoModulesCard() {
  const { t } = useTranslation();
  return (
    <Card>
      <EmptyState icon={LayoutGrid} title={t('workspaceApp.noModules.title')} body={t('workspaceApp.noModules.body')} />
    </Card>
  );
}

/** Nothing permitted at all: no dashboard and no modules. */
function NoModulesState() {
  const { t } = useTranslation();
  const { context } = useWorkspace();
  useDocumentTitle(context.workspace.name);
  return (
    <div className="mx-auto w-full max-w-2xl px-4 py-10 sm:px-6 lg:py-16">
      <p className="mb-3 text-center text-[13px] font-medium text-muted-foreground">{context.workspace.name}</p>
      <NoModulesCard />
      <p className="mt-4 text-center text-xs text-muted-foreground">
        {t('workspaceApp.noModules.hint')}{' '}
        <Link to="/crm/me" className="font-medium text-primary hover:underline">
          {t('workspaceApp.noModules.profile')}
        </Link>
      </p>
    </div>
  );
}

/** "Opportunities" → "opportunity", "Calendar events" → "calendar event" (for "New …" buttons). */
function singular(plural: string): string {
  const w = plural.trim().toLowerCase().replace(/^recent /, '');
  if (w.endsWith('ies')) return `${w.slice(0, -3)}y`;
  if (w.endsWith('ses') || w.endsWith('xes')) return w.slice(0, -2);
  return w.endsWith('s') ? w.slice(0, -1) : w;
}
