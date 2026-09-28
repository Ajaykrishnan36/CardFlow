import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Building2,
  ContactRound,
  ExternalLink,
  LifeBuoy,
  LogIn,
  LogOut,
  Plug,
  RefreshCw,
  Settings,
  ShieldCheck,
  Smartphone,
  Ticket,
  UserPlus,
  UsersRound,
  type LucideIcon
} from 'lucide-react';
import { integrationsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { IntegrationInfo } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { integrationsKey, useIntegrations } from './use-integrations';

const statusTone = {
  active: 'success',
  error: 'danger',
  disabled: 'neutral'
} as const;

export function IntegrationsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('integrations.page.title'));
  const q = useIntegrations({ poll: true });
  const list = q.data;

  return (
    <PageContainer>
      <PageHeader
        title={t('integrations.page.title')}
        description={t('integrations.page.description')}
        actions={
          list && list.length > 0 ? (
            <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" aria-live="off">
              <span className={cn('size-1.5 rounded-full bg-success', q.isFetching && 'animate-pulse')} aria-hidden />
              {t('integrations.page.autoRefresh')}
            </span>
          ) : undefined
        }
      />

      {q.isError && !list ? (
        <Card>
          <ErrorState
            title={t('integrations.page.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        </Card>
      ) : !list ? (
        <CardSkeleton />
      ) : list.length === 0 ? (
        <Card>
          <EmptyState icon={Plug} title={t('integrations.page.emptyTitle')} body={t('integrations.page.emptyBody')} />
        </Card>
      ) : (
        <div className="space-y-6">
          {list.map((it) => (
            <IntegrationCard key={it.key} integration={it} />
          ))}
          <HowItWorks name={list[0]?.name ?? ''} />
        </div>
      )}
    </PageContainer>
  );
}

function IntegrationCard({ integration: it }: { integration: IntegrationInfo }) {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const sync = useMutation({
    mutationFn: () => integrationsApi.sync(it.key),
    onSuccess: (next) => {
      qc.setQueryData<IntegrationInfo[]>(integrationsKey, (old) => old?.map((x) => (x.key === next.key ? next : x)));
      if (next.status === 'error') toast.error(t('integrations.card.syncFailed', { error: next.lastError ?? '' }));
      else toast.success(t('integrations.card.synced', { name: next.name }));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const s = it.stats;
  const ws = it.workspace;

  return (
    <Card className="min-w-0">
      <div className="flex flex-col gap-4 p-5 lg:flex-row lg:items-start">
        <div className="flex min-w-0 flex-1 items-start gap-3.5">
          <span className="grid size-12 shrink-0 place-items-center rounded-xl bg-primary-soft text-primary ring-1 ring-inset ring-primary/15" aria-hidden>
            <Smartphone className="size-6" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-[17px] font-semibold tracking-tight text-foreground">{it.name}</h2>
              <Badge tone={statusTone[it.status] ?? 'neutral'}>
                {t(`integrations.status.${it.status}`, {
                  defaultValue: it.status
                })}
              </Badge>
            </div>
            <p className="mt-0.5 text-[13px] text-muted-foreground">{it.description}</p>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-muted-foreground">
              <span className="inline-flex items-center gap-1.5">
                <RefreshCw className={cn('size-3.5', sync.isPending && 'animate-spin')} aria-hidden />
                {t('integrations.card.syncing', {
                  seconds: it.intervalSeconds
                })}{' '}
                ·{' '}
                {it.lastSyncAt ? (
                  <Tooltip content={new Date(it.lastSyncAt).toLocaleString('en-IN')} side="top">
                    <span tabIndex={0} className="focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                      {t('integrations.card.lastSynced', {
                        time: relativeTime(it.lastSyncAt)
                      })}
                    </span>
                  </Tooltip>
                ) : (
                  t('integrations.card.neverSynced')
                )}
              </span>
              <span className="inline-flex items-center gap-1.5">
                <Building2 className="size-3.5" aria-hidden />
                {ws ? (
                  <Link to={`/crm/owner/workspaces/${ws.id}`} className="font-medium text-foreground hover:underline">
                    {ws.name} <span className="font-mono font-normal text-muted-foreground">· {ws.code}</span>
                  </Link>
                ) : (
                  t('integrations.card.noWorkspace')
                )}
              </span>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 lg:justify-end">
          <Button size="sm" variant="outline" onClick={() => sync.mutate()} loading={sync.isPending} disabled={it.status === 'disabled'}>
            {!sync.isPending ? <RefreshCw /> : null} {t('integrations.card.syncNow')}
          </Button>
          {ws ? (
            <>
              <Button asChild size="sm" variant="outline">
                <Link to={`/crm/owner/workspaces/${ws.id}`}>
                  <Settings /> {t('integrations.card.settings')}
                </Link>
              </Button>
              <Button asChild size="sm">
                <Link to={`/crm/w/${ws.code}/home`}>
                  <ExternalLink /> {t('integrations.card.openCrm')}
                </Link>
              </Button>
            </>
          ) : null}
        </div>
      </div>

      {it.status === 'error' && it.lastError ? (
        <div className="px-5 pb-4">
          <Alert tone="danger" title={t('integrations.card.errorTitle')}>
            <span className="break-words font-mono text-xs">{it.lastError}</span>
          </Alert>
        </div>
      ) : it.status === 'disabled' ? (
        <div className="px-5 pb-4">
          <Alert tone="info">{t('integrations.card.disabledNote')}</Alert>
        </div>
      ) : null}

      {/* Cells draw right/bottom borders; the -1px margins + clip hide the outer ones at any column count. */}
      <div className="overflow-hidden rounded-b-lg border-t">
        <dl
          className="-mb-px -mr-px grid grid-cols-2 min-[480px]:grid-cols-3 md:grid-cols-4 xl:grid-cols-7"
          aria-label={t('integrations.card.statsLabel', { name: it.name })}
        >
          <Stat icon={UsersRound} label={t('integrations.stats.appUsers')} value={s.appUsers} />
          <Stat icon={UserPlus} label={t('integrations.stats.newToday')} value={s.newToday} highlight={s.newToday > 0} />
          <Stat icon={LogIn} label={t('integrations.stats.signInsToday')} value={s.signInsToday} highlight={s.signInsToday > 0} />
          <Stat icon={Building2} label={t('integrations.stats.accounts')} value={s.accounts} />
          <Stat icon={ContactRound} label={t('integrations.stats.contacts')} value={s.contacts} />
          <Stat
            icon={Ticket}
            label={t('integrations.stats.tickets')}
            value={s.ticketsOpen}
            suffix={t('integrations.stats.ticketsOf', {
              total: s.ticketsTotal.toLocaleString('en-IN')
            })}
            warn={s.ticketsOpen > 0}
          />
          <Stat icon={ShieldCheck} label={t('integrations.stats.admins')} value={s.admins} />
        </dl>
      </div>
    </Card>
  );
}

function Stat({
  icon: Icon,
  label,
  value,
  suffix,
  highlight,
  warn
}: {
  icon: LucideIcon;
  label: string;
  value: number;
  suffix?: string;
  highlight?: boolean;
  warn?: boolean;
}) {
  return (
    <div className="min-w-0 border-b border-r px-4 py-3">
      <dt className="flex items-center gap-1.5 truncate text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        <Icon className="size-3.5 shrink-0" aria-hidden />
        <span className="truncate">{label}</span>
      </dt>
      <dd className="mt-1 flex items-baseline gap-1.5">
        <span
          className={cn(
            'text-xl font-semibold leading-none tracking-tight tabular-nums',
            warn ? 'text-warning' : highlight ? 'text-success' : 'text-foreground'
          )}
        >
          {value.toLocaleString('en-IN')}
        </span>
        {suffix ? <span className="text-xs tabular-nums text-muted-foreground">{suffix}</span> : null}
      </dd>
    </div>
  );
}

function HowItWorks({ name }: { name: string }) {
  const { t } = useTranslation();
  const steps: Array<{
    icon: LucideIcon;
    title: string;
    body: string;
    muted?: boolean;
  }> = [
    {
      icon: UserPlus,
      title: t('integrations.how.signUpTitle'),
      body: t('integrations.how.signUpBody')
    },
    {
      icon: LogIn,
      title: t('integrations.how.signInTitle'),
      body: t('integrations.how.signInBody')
    },
    {
      icon: LifeBuoy,
      title: t('integrations.how.supportTitle'),
      body: t('integrations.how.supportBody')
    },
    {
      icon: ShieldCheck,
      title: t('integrations.how.adminsTitle'),
      body: t('integrations.how.adminsBody')
    },
    {
      icon: LogOut,
      title: t('integrations.how.logoutTitle'),
      body: t('integrations.how.logoutBody'),
      muted: true
    }
  ];
  return (
    <Card className="min-w-0">
      <CardHeader title={t('integrations.how.title')} description={t('integrations.how.description', { name })} />
      <div className="overflow-hidden rounded-b-lg">
        <ol className="-mb-px -mr-px grid sm:grid-cols-2 lg:grid-cols-5">
          {steps.map((st, i) => (
            <HowStep key={st.title} n={i + 1} icon={st.icon} title={st.title} muted={st.muted}>
              {st.body}
            </HowStep>
          ))}
        </ol>
      </div>
    </Card>
  );
}

function HowStep({ n, icon: Icon, title, muted, children }: { n: number; icon: LucideIcon; title: string; muted?: boolean; children: ReactNode }) {
  return (
    <li className="flex gap-3 border-b border-r px-5 py-4">
      <span
        className={cn('grid size-8 shrink-0 place-items-center rounded-lg', muted ? 'bg-muted text-muted-foreground' : 'bg-primary-soft text-primary')}
        aria-hidden
      >
        <Icon className="size-4" />
      </span>
      <div className="min-w-0">
        <p className={cn('text-[13px] font-medium', muted ? 'text-muted-foreground' : 'text-foreground')}>
          <span className="mr-1 tabular-nums text-muted-foreground">{n}.</span>
          {title}
        </p>
        <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{children}</p>
      </div>
    </li>
  );
}

function CardSkeleton() {
  return (
    <Card className="overflow-hidden" aria-busy>
      <div className="flex items-start gap-3.5 p-5">
        <Skeleton className="size-12 rounded-xl" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-3.5 w-80 max-w-full" />
          <Skeleton className="h-3 w-64 max-w-full" />
        </div>
        <Skeleton className="hidden h-8 w-64 lg:block" />
      </div>
      <div className="grid grid-cols-2 border-t min-[480px]:grid-cols-3 md:grid-cols-4 xl:grid-cols-7">
        {Array.from({ length: 7 }).map((_, i) => (
          <div key={i} className="space-y-2 px-4 py-3">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-5 w-12" />
          </div>
        ))}
      </div>
    </Card>
  );
}
