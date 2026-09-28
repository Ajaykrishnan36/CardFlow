import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { CreditCard, LifeBuoy, Smartphone, Store } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AppUser } from '@crm/api/types';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { useWorkspace } from '../workspace-context';
import { AccessBadge, appAccess, appKeys, appUserPath, formatPhone, RoleBadge, UserStatusBadge } from './app-utils';

const FILTERS = ['all', 'premium', 'free', 'admin', 'suspended'] as const;
type Filter = (typeof FILTERS)[number];

/** Search params as list state (shareable, survives back/forward). */
export function useListParams<F extends string>(filters: readonly F[], fallback: F) {
  const [sp, setSp] = useSearchParams();
  const q = sp.get('q') ?? '';
  const raw = sp.get('filter');
  const filter = raw && (filters as readonly string[]).includes(raw) ? (raw as F) : fallback;
  const patch = useCallback(
    (p: Record<string, string | null>) =>
      setSp(
        (prev) => {
          const n = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(p)) {
            if (v === null || v === '') n.delete(k);
            else n.set(k, v);
          }
          return n;
        },
        { replace: true }
      ),
    [setSp]
  );
  return { q, filter, patch };
}

/** /crm/w/:ws/app-users — everyone who signed up in the connected app. */
export function AppUsersPage() {
  const { context } = useWorkspace();
  if (!appAccess(context, 'app_user').read) return <RecordNoAccess />;
  return <AppUsersView />;
}

function AppUsersView() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  useDocumentTitle(t('workspaceApp.app.usersTitle'));
  const { q, filter, patch } = useListParams(FILTERS, 'all');
  const onSearch = useCallback((v: string) => patch({ q: v }), [patch]);
  const params = { q: q || undefined, filter: filter === 'all' ? undefined : filter };
  const list = useQuery({
    queryKey: appKeys.users(code, params),
    queryFn: () => workspaceAppApi(code).users(params),
    placeholderData: keepPreviousData
  });
  if (isApiError(list.error) && list.error.status === 403) return <RecordNoAccess />;
  const counts = list.data?.counts;
  const rows = list.data?.data;

  return (
    <PageContainer wide>
      <PageHeader
        title={t('workspaceApp.app.usersTitle')}
        description={t('workspaceApp.app.usersSubtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <Smartphone className="size-5" aria-hidden />
          </span>
        }
      />
      <Card className="min-w-0 overflow-hidden">
        <div className="flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center">
          <SearchInput value={q} onChange={onSearch} placeholder={t('workspaceApp.app.usersSearch')} className="lg:max-w-xs" />
          <SegmentedFilter<Filter>
            value={filter}
            onChange={(v) => patch({ filter: v === 'all' ? null : v })}
            options={FILTERS.map((f) => ({ value: f, label: t(`workspaceApp.app.userFilter.${f}`), count: counts?.[f] }))}
          />
        </div>
        {list.isError ? (
          <ErrorState
            title={t('workspaceApp.app.loadError')}
            message={isApiError(list.error) ? list.error.message : undefined}
            requestId={isApiError(list.error) ? list.error.requestId : undefined}
            onRetry={() => void list.refetch()}
          />
        ) : !rows ? (
          <RowsSkeleton />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Smartphone}
            title={q || filter !== 'all' ? t('workspaceApp.app.noMatch') : t('workspaceApp.app.noUsers')}
            action={
              q || filter !== 'all' ? (
                <Button variant="outline" size="sm" onClick={() => patch({ q: null, filter: null })}>
                  {t('workspaceApp.support.clearFilters')}
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ul className={cn('divide-y transition-opacity', list.isPlaceholderData && 'opacity-60')}>
            {rows.map((u) => (
              <UserRow key={u.id} user={u} code={code} />
            ))}
          </ul>
        )}
      </Card>
    </PageContainer>
  );
}

function UserRow({ user: u, code }: { user: AppUser; code: string }) {
  const { t } = useTranslation();
  return (
    <li className="group relative flex flex-col gap-2 px-4 py-3 transition-colors hover:bg-muted/50 sm:flex-row sm:items-center sm:gap-4">
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <Avatar name={u.name || u.phone} className="size-9" />
        <div className="min-w-0">
          <Link
            to={appUserPath(code, u.id)}
            className="block truncate text-[13px] font-medium text-foreground after:absolute after:inset-0 after:content-[''] group-hover:text-primary focus-visible:outline-none"
          >
            {u.name || t('workspaceApp.app.unnamed')}
          </Link>
          <p className="truncate text-xs tabular-nums text-muted-foreground">
            {formatPhone(u.phone)}
            {u.city ? ` · ${u.city}` : ''}
            {u.email ? ` · ${u.email}` : ''}
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 pl-12 sm:w-72 sm:pl-0">
        <RoleBadge role={u.role} />
        <AccessBadge user={u} />
        <UserStatusBadge status={u.status} />
      </div>
      <div className="flex items-center gap-3 pl-12 text-xs tabular-nums text-muted-foreground sm:w-56 sm:justify-end sm:pl-0">
        <span className="inline-flex items-center gap-1" title={t('workspaceApp.app.businessesCount', { count: u.businesses })}>
          <Store className="size-3.5" aria-hidden />
          {u.businesses}
        </span>
        <span className="inline-flex items-center gap-1" title={t('workspaceApp.app.cardsCount', { count: u.cards })}>
          <CreditCard className="size-3.5" aria-hidden />
          {u.cards}
        </span>
        {u.openTickets ? (
          <span className="inline-flex items-center gap-1 text-warning" title={t('workspaceApp.app.openTickets', { count: u.openTickets })}>
            <LifeBuoy className="size-3.5" aria-hidden />
            {u.openTickets}
          </span>
        ) : null}
        <span className="ml-auto sm:ml-0">{u.lastLoginAt ? relativeTime(u.lastLoginAt) : t('workspaceApp.app.neverSignedIn')}</span>
      </div>
    </li>
  );
}

export function RowsSkeleton() {
  return (
    <div aria-busy="true" className="divide-y">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3">
          <Skeleton className="size-9 rounded-full" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-64" />
          </div>
          <Skeleton className="h-5 w-24" />
        </div>
      ))}
    </div>
  );
}
