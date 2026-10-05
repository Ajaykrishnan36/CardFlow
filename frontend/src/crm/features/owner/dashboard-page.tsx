import { useTranslation } from 'react-i18next';
import { EditLayoutButton, useLayout, type Device } from '@crm/features/shell/ui-layout';
import { arrange, isHidden, OWNER_SECTIONS } from '../../../navigation/layout-items';
import { navIcon } from '@crm/features/shell/nav-icons';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { LifeBuoy, ArrowRight, Boxes, Briefcase, Building2, Check, ChevronRight, Clock, Contact, LayoutGrid, MailPlus, Plus, RefreshCw, UserPlus, UsersRound, type LucideIcon } from 'lucide-react';
import { platformApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ChecklistStep, Kpi, RecentWorkspace } from '@crm/api/types';
import { useMe } from '@crm/auth/session';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime, timeOfDayGreeting } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { OwnerScopeBar, ownerFilterParams, useOwnerFilter } from './owner-filter';

const kpiIcons: Record<string, LucideIcon> = {
  products: Boxes,
  apps: LayoutGrid,
  leads: UserPlus,
  accounts: Briefcase,
  contacts: Contact,
  users: UsersRound,
  identities: UsersRound,
  workspaces: Building2,
  invites: MailPlus,
  tickets: LifeBuoy
};

export function OwnerDashboardPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('brand.ownerConsole'));
  const { data: me } = useMe();
  const filter = useOwnerFilter();
  const q = useQuery({ queryKey: ['platform', 'dashboard', filter.product, filter.app], queryFn: () => platformApi.dashboard(ownerFilterParams(filter)), staleTime: 60_000 });
  const firstName = me?.identity.displayName.split(' ')[0] ?? '';
  const greetingKey = { morning: 'greetingMorning', afternoon: 'greetingAfternoon', evening: 'greetingEvening' }[timeOfDayGreeting()];
  // The owner console's own arrangement of this page (D-130).
  const { layout } = useLayout('/platform', 'dashboard');
  const ownerGroups = (_d: Device) => [
    { title: 'Sections', items: OWNER_SECTIONS },
    { title: 'Totals', items: (q.data?.kpis ?? []).map((k) => ({ key: `kpi:${k.key}`, label: k.label })) }
  ];

  return (
    <div className="mx-auto w-full max-w-[1240px] px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight text-foreground sm:text-2xl">
            {t(`owner.dashboard.${greetingKey}`, { name: firstName })}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">{t('owner.dashboard.subtitle')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <OwnerScopeBar />
          {q.data ? (
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Clock className="size-3.5" aria-hidden />
              {t('owner.dashboard.refreshedAt', { time: relativeTime(q.data.refreshedAt) })}
            </span>
          ) : null}
          <EditLayoutButton prefix="/platform" surface="dashboard" title="Edit dashboard layout" groups={ownerGroups} />
          <Button variant="outline" size="sm" onClick={() => void q.refetch()} loading={q.isFetching && !q.isPending}>
            {!q.isFetching || q.isPending ? <RefreshCw /> : null} {t('owner.dashboard.refresh')}
          </Button>
        </div>
      </div>

      {isHidden(layout, 'section:quick') ? null : (
      <nav aria-label={t('owner.dashboard.quickActions')} className="-mt-2 mb-6 hidden flex-wrap items-center gap-2 sm:flex">
        <Button asChild variant="outline" size="sm">
          <Link to="/crm/owner/leads?new=1">
            <Plus /> {t('owner.dashboard.newLead')}
          </Link>
        </Button>
        <Button asChild variant="outline" size="sm">
          <Link to="/crm/owner/workspaces/new">
            <Building2 /> {t('owner.dashboard.provisionWorkspace')}
          </Link>
        </Button>
      </nav>
      )}

      {q.isError ? (
        <Card>
          <ErrorState
            title={t('owner.dashboard.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : (
        <div className="space-y-6">
          {arrange(OWNER_SECTIONS.filter((x) => x.key !== 'section:quick'), (x) => x.key, layout).map((x, i, shown) =>
            x.key === 'section:kpis' ? (
              <section key={x.key} className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6" aria-label={t('owner.dashboard.keyMetrics')}>
                {q.data ? arrange(q.data.kpis, (k) => `kpi:${k.key}`, layout).map((k) => <KpiCard key={k.key} kpi={k} />) : Array.from({ length: 6 }).map((_, n) => <KpiSkeleton key={n} />)}
              </section>
            ) : shown[i - 1] && shown[i - 1]!.key !== 'section:kpis' ? null : (
              // The checklist and the recent products sit side by side when they follow each other.
              <div key={x.key} className={shown[i + 1] && shown[i + 1]!.key !== 'section:kpis' ? 'grid gap-6 xl:grid-cols-2' : undefined}>
                {[x, shown[i + 1] && shown[i + 1]!.key !== 'section:kpis' ? shown[i + 1]! : null].map((y) =>
                  !y ? null : y.key === 'section:checklist' ? <Checklist key={y.key} steps={q.data?.checklist} /> : <RecentWorkspaces key={y.key} rows={q.data?.recentWorkspaces} />
                )}
              </div>
            )
          )}
        </div>
      )}
    </div>
  );
}

/** KPI tile (also used by the workspace dashboard). */
export function KpiCard({ kpi }: { kpi: Kpi }) {
  const { t } = useTranslation();
  const Icon = kpiIcons[kpi.key] ?? (kpi.icon ? navIcon(kpi.icon) : Boxes);
  const body = (
    <>
      <div className="flex items-start justify-between gap-2">
        <p className="min-w-0 truncate text-[13px] font-medium text-muted-foreground">{kpi.label}</p>
        <span className="grid size-7 shrink-0 place-items-center rounded-md bg-primary-soft text-primary">
          <Icon className="size-3.5" aria-hidden />
        </span>
      </div>
      <p className="mt-1.5 text-[26px] font-semibold leading-none tracking-tight tabular-nums text-foreground">{kpi.value.toLocaleString('en-IN')}</p>
      <div className="mt-2 flex h-4 items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="min-w-0 truncate">{kpi.hint}</span>
        {kpi.path ? (
          <span className="flex shrink-0 items-center gap-0.5 font-medium text-muted-foreground/60 transition-colors group-hover:text-primary group-focus-visible:text-primary">
            {kpi.hint ? null : (
              <span className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">{t('owner.dashboard.viewAll')}</span>
            )}
            <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden />
          </span>
        ) : null}
      </div>
    </>
  );
  if (!kpi.path) return <Card className="min-w-0 p-4">{body}</Card>;
  return (
    <Link
      to={kpi.path}
      aria-label={`${kpi.label}: ${kpi.value.toLocaleString('en-IN')}. ${t('owner.dashboard.viewAllOf', { label: kpi.label.toLowerCase() })}`}
      className="group block min-w-0 rounded-lg border bg-card p-4 text-card-foreground shadow-card transition-[box-shadow,border-color] hover:border-primary/30 hover:shadow-pop focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      {body}
    </Link>
  );
}

export function KpiSkeleton() {
  return (
    <Card className="p-4">
      <Skeleton className="h-4 w-20" />
      <Skeleton className="mt-3 h-6 w-12" />
      <Skeleton className="mt-3 h-3 w-16" />
    </Card>
  );
}

function Checklist({ steps }: { steps?: ChecklistStep[] }) {
  const { t } = useTranslation();
  const done = steps?.filter((s) => s.status === 'done').length ?? 0;
  const total = steps?.length ?? 5;
  const pct = Math.round((done / total) * 100);
  return (
    <Card>
      <CardHeader
        title={t('owner.dashboard.checklistTitle')}
        description={steps ? t('owner.dashboard.checklistProgress', { done, total }) : undefined}
        actions={steps ? <span className="text-sm font-semibold tabular-nums text-primary">{pct}%</span> : undefined}
      />
      <div className="px-5 pt-4">
        <div className="h-1.5 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div className="h-full rounded-full bg-primary transition-[width] duration-700 ease-out" style={{ width: `${steps ? pct : 0}%` }} />
        </div>
      </div>
      <ol className="p-2">
        {steps
          ? steps.map((s, i) => <ChecklistRow key={s.key} step={s} index={i + 1} />)
          : Array.from({ length: 5 }).map((_, i) => (
              <li key={i} className="flex items-center gap-3 px-3 py-3">
                <Skeleton className="size-7 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-40" />
                  <Skeleton className="h-3 w-56" />
                </div>
              </li>
            ))}
      </ol>
    </Card>
  );
}

function ChecklistRow({ step, index }: { step: ChecklistStep; index: number }) {
  const { t } = useTranslation();
  const isDone = step.status === 'done';
  const isUpcoming = step.status === 'upcoming';
  const content = (
    <>
      <span
        className={cn(
          'grid size-7 shrink-0 place-items-center rounded-full border text-xs font-semibold',
          isDone ? 'border-success bg-success text-white' : isUpcoming ? 'border-dashed text-muted-foreground' : 'border-primary/40 text-primary'
        )}
      >
        {isDone ? <Check className="size-4" aria-hidden /> : index}
      </span>
      <span className="min-w-0 flex-1">
        <span className={cn('block text-[13px] font-medium', isDone ? 'text-muted-foreground line-through decoration-muted-foreground/40' : 'text-foreground')}>
          {step.title}
        </span>
        <span className="block text-xs text-muted-foreground">{step.description}</span>
      </span>
      {isDone ? (
        <Badge tone="success">{t('owner.dashboard.stepDone')}</Badge>
      ) : isUpcoming ? (
        <Badge>{t('owner.dashboard.stepUpcoming')}</Badge>
      ) : (
        <span className="flex items-center gap-1 text-xs font-medium text-primary">
          {t('owner.dashboard.stepStart')} <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden />
        </span>
      )}
    </>
  );
  const cls = 'group flex items-center gap-3 rounded-md px-3 py-3 transition-colors';
  return (
    <li>
      {!isDone && !isUpcoming && step.path ? (
        <Link to={step.path} className={cn(cls, 'hover:bg-muted')}>
          {content}
        </Link>
      ) : (
        <div className={cls}>{content}</div>
      )}
    </li>
  );
}

const statusTone: Record<string, 'success' | 'warning' | 'danger' | 'neutral' | 'primary'> = {
  active: 'success',
  provisioning: 'primary',
  draft: 'neutral',
  failed: 'danger',
  suspended: 'warning'
};

function RecentWorkspaces({ rows }: { rows?: RecentWorkspace[] }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  return (
    <Card className="min-w-0">
      <CardHeader title={t('owner.dashboard.recentTitle')} />
      {!rows ? (
        <div className="space-y-3 p-5">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-9" />
          ))}
        </div>
      ) : rows.length === 0 ? (
        <EmptyState icon={Building2} title={t('owner.dashboard.recentEmpty')} />
      ) : (
        <>
          {/* Table on ≥640px, cards below (PRD §10.1: tables become card lists on small screens). */}
          <div className="hidden overflow-x-auto sm:block">
            <table className="w-full text-left text-[13px]">
              <thead>
                <tr className="border-b text-xs text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">{t('owner.dashboard.colWorkspace')}</th>
                  <th className="px-3 py-2.5 font-medium">{t('owner.dashboard.colStatus')}</th>
                  <th className="px-3 py-2.5 text-left font-medium">{t('owner.dashboard.colProducts')}</th>
                  <th className="px-3 py-2.5 text-right font-medium">{t('owner.dashboard.colMembers')}</th>
                  <th className="px-5 py-2.5 text-right font-medium">{t('owner.dashboard.colCreated')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((w) => (
                  <tr
                    key={w.id}
                    onClick={() => navigate(`/crm/owner/workspaces/${w.id}`)}
                    className="cursor-pointer border-b transition-colors last:border-0 hover:bg-muted/50"
                  >
                    <td className="px-5 py-3">
                      <Link
                        to={`/crm/owner/workspaces/${w.id}`}
                        onClick={(e) => e.stopPropagation()}
                        className="font-medium text-foreground hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        {w.name}
                      </Link>
                      <p className="font-mono text-xs text-muted-foreground">{w.code}</p>
                    </td>
                    <td className="px-3 py-3">
                      <Badge tone={statusTone[w.status] ?? 'neutral'}>{t(`status.${w.status}`, { defaultValue: w.status })}</Badge>
                    </td>
                    <td className="max-w-[200px] truncate px-3 py-3 text-[13px]" title={w.setupNames?.join(', ')}>{w.setupNames?.length ? w.setupNames.join(', ') : '—'}</td>
                    <td className="px-3 py-3 text-right tabular-nums">{w.members}</td>
                    <td className="px-5 py-3 text-right text-muted-foreground">{relativeTime(w.createdAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ul className="divide-y sm:hidden">
            {rows.map((w) => (
              <li key={w.id}>
                <Link
                  to={`/crm/owner/workspaces/${w.id}`}
                  className="flex items-center justify-between gap-3 px-5 py-3 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
                >
                  <div className="min-w-0">
                    <p className="truncate font-medium text-foreground">{w.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {t('owner.dashboard.productsCount', { count: w.products })} · {t('owner.dashboard.membersCount', { count: w.members })} ·{' '}
                      {relativeTime(w.createdAt)}
                    </p>
                  </div>
                  <span className="flex shrink-0 items-center gap-1.5">
                    <Badge tone={statusTone[w.status] ?? 'neutral'}>{t(`status.${w.status}`, { defaultValue: w.status })}</Badge>
                    <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </Card>
  );
}
