import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Bookmark, Store } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AppBusiness } from '@crm/api/types';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { useWorkspace } from '../workspace-context';
import { appAccess, appKeys, appUserPath, businessPath, BusinessStatusBadge, formatPhone, ListingBadge, VerificationBadge } from './app-utils';
import { RowsSkeleton, useListParams } from './app-users-page';

const FILTERS = ['all', 'listed', 'hidden', 'verified', 'review'] as const;
type Filter = (typeof FILTERS)[number];

/** /crm/w/:ws/businesses — the app's business directory (listings, badges, visibility). */
export function BusinessesPage() {
  const { context } = useWorkspace();
  if (!appAccess(context, 'app_business').read) return <RecordNoAccess />;
  return <BusinessesView />;
}

function BusinessesView() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  const canUsers = appAccess(context, 'app_user').read;
  useDocumentTitle(t('workspaceApp.app.bizTitle'));
  const { q, filter, patch } = useListParams(FILTERS, 'all');
  const onSearch = useCallback((v: string) => patch({ q: v }), [patch]);
  const params = { q: q || undefined, filter: filter === 'all' ? undefined : filter };
  const list = useQuery({
    queryKey: appKeys.businesses(code, params),
    queryFn: () => workspaceAppApi(code).businesses(params),
    placeholderData: keepPreviousData
  });
  if (isApiError(list.error) && list.error.status === 403) return <RecordNoAccess />;
  const counts = list.data?.counts;
  const rows = list.data?.data;

  return (
    <PageContainer wide>
      <PageHeader
        title={t('workspaceApp.app.bizTitle')}
        description={t('workspaceApp.app.bizSubtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <Store className="size-5" aria-hidden />
          </span>
        }
      />
      <Card className="min-w-0 overflow-hidden">
        <div className="flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center">
          <SearchInput value={q} onChange={onSearch} placeholder={t('workspaceApp.app.bizSearch')} className="lg:max-w-xs" />
          <SegmentedFilter<Filter>
            value={filter}
            onChange={(v) => patch({ filter: v === 'all' ? null : v })}
            options={FILTERS.map((f) => ({ value: f, label: t(`workspaceApp.app.bizFilter.${f}`), count: counts?.[f] }))}
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
            icon={Store}
            title={q || filter !== 'all' ? t('workspaceApp.app.noMatch') : t('workspaceApp.app.noBusinesses')}
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
            {rows.map((b) => (
              <BusinessRow key={b.id} biz={b} code={code} canUsers={canUsers} />
            ))}
          </ul>
        )}
      </Card>
    </PageContainer>
  );
}

function BusinessRow({ biz: b, code, canUsers }: { biz: AppBusiness; code: string; canUsers: boolean }) {
  const { t } = useTranslation();
  return (
    <li className="group relative flex flex-col gap-2 px-4 py-3 transition-colors hover:bg-muted/50 md:flex-row md:items-center md:gap-4">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
          <Store className="size-5" aria-hidden />
        </span>
        <div className="min-w-0">
          <Link
            to={businessPath(code, b.id)}
            className="block truncate text-[13px] font-semibold text-foreground after:absolute after:inset-0 after:content-[''] group-hover:text-primary focus-visible:outline-none"
          >
            {b.name}
          </Link>
          <p className="truncate text-xs uppercase tracking-wide text-muted-foreground">
            {[b.category, b.city && `${b.city} (${b.pincode})`].filter(Boolean).join(' · ')}
          </p>
          <p className="truncate text-xs text-muted-foreground">
            {t('workspaceApp.app.owner')}:{' '}
            {canUsers ? (
              <Link to={appUserPath(code, b.owner.id)} className="relative z-10 font-medium text-primary hover:underline">
                {b.owner.name || formatPhone(b.owner.phone)}
              </Link>
            ) : (
              b.owner.name
            )}{' '}
            <span className="tabular-nums">({formatPhone(b.owner.phone)})</span>
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 pl-[3.25rem] md:pl-0">
        <VerificationBadge value={b.verification} />
        <ListingBadge value={b.listing} />
        <BusinessStatusBadge value={b.status} />
        {b.savedBy ? (
          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground" title={t('workspaceApp.app.savedBy', { count: b.savedBy })}>
            <Bookmark className="size-3.5" aria-hidden /> {b.savedBy}
          </span>
        ) : null}
      </div>
    </li>
  );
}
