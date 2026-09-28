import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight, ShieldCheck, ShieldOff, UserPlus, UsersRound } from 'lucide-react';
import { usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { UserSummary } from '@crm/api/types';
import { useMe } from '@crm/auth/session';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';
import { PermissionSetsPanel } from '@crm/features/access/permission-sets-panel';
import { useAccessWorkspaces } from '@crm/features/access/use-access';
import { shortDate, userStatusTone } from './user-format';
import { NewUserDialog } from './new-user-dialog';

const PAGE_SIZE = 50;
type StatusFilter = 'all' | 'active' | 'suspended';
type PageTab = 'users' | 'permission-sets';

export function UsersPage() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const tab: PageTab = params.get('tab') === 'permission-sets' ? 'permission-sets' : 'users';
  const [creating, setCreating] = useState(false);
  useDocumentTitle(tab === 'users' ? t('users.list.title') : t('users.list.tabPermissionSets'));

  const setTab = (next: PageTab) =>
    setParams(
      (prev) => {
        const sp = new URLSearchParams();
        // Each tab keeps only its own filters.
        if (next === 'permission-sets') {
          sp.set('tab', next);
          const ws = prev.get('ws');
          if (ws) sp.set('ws', ws);
        }
        return sp;
      },
      { replace: true }
    );

  return (
    <PageContainer>
      <PageHeader
        title={t('users.list.title')}
        description={t('users.list.description')}
        actions={
          <Button onClick={() => setCreating(true)}>
            <UserPlus /> {t('users.list.newUser')}
          </Button>
        }
        className="mb-4"
      />
      <Tabs<PageTab>
        className="mb-5"
        value={tab}
        onChange={setTab}
        items={[
          { value: 'users', label: t('users.list.tabUsers') },
          { value: 'permission-sets', label: t('users.list.tabPermissionSets') }
        ]}
      />
      <div role="tabpanel">
        {tab === 'users' ? (
          <UsersList />
        ) : (
          <PermissionSetsPanel
            selectedWorkspaceId={params.get('ws') ?? undefined}
            onSelectWorkspace={(id) =>
              setParams(
                (prev) => {
                  const sp = new URLSearchParams(prev);
                  sp.set('ws', id);
                  return sp;
                },
                { replace: true }
              )
            }
          />
        )}
      </div>
      <NewUserDialog open={creating} onOpenChange={setCreating} />
    </PageContainer>
  );
}

function UsersList() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data: me } = useMe();
  const [params, setParams] = useSearchParams();
  const workspaces = useAccessWorkspaces();
  const platformName = workspaces.data?.find((w) => w.isPlatform)?.name;
  const teamLabel = t('users.list.yourTeam');
  const wsNames = (names: string[]) =>
    names.map((n) => (n === platformName || n === t('access.workspace.platformLabel') ? teamLabel : n));

  const q = params.get('q') ?? '';
  const rawStatus = params.get('status');
  const status: StatusFilter = rawStatus === 'active' || rawStatus === 'suspended' ? rawStatus : 'all';
  const page = Math.max(1, Number.parseInt(params.get('page') ?? '1', 10) || 1);
  const offset = (page - 1) * PAGE_SIZE;

  const update = useCallback(
    (patch: Record<string, string | null>) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(patch)) {
            if (v === null || v === '') next.delete(k);
            else next.set(k, v);
          }
          return next;
        },
        { replace: true }
      );
    },
    [setParams]
  );
  const onSearch = useCallback((v: string) => update({ q: v, page: null }), [update]);

  const query = useQuery({
    queryKey: ['platform', 'users', { q, status, offset }],
    queryFn: () => usersApi.list({ q: q || undefined, status: status === 'all' ? undefined : status, limit: PAGE_SIZE, offset }),
    placeholderData: keepPreviousData
  });

  const rows = query.data?.data;
  const total = query.data?.total ?? 0;
  const filtered = q !== '' || status !== 'all';
  // A page past the end (e.g. after rows were filtered away) snaps back to page 1.
  useEffect(() => {
    if (rows && rows.length === 0 && page > 1 && !query.isPlaceholderData) update({ page: null });
  }, [rows, page, query.isPlaceholderData, update]);
  const open = (u: UserSummary) => navigate(`/crm/owner/users/${u.id}`);

  return (
    <>
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onChange={onSearch} placeholder={t('users.list.search')} />
        <SegmentedFilter<StatusFilter>
          value={status}
          onChange={(v) => update({ status: v === 'all' ? null : v, page: null })}
          options={[
            { value: 'all', label: t('users.list.filterAll') },
            { value: 'active', label: t('users.list.filterActive') },
            { value: 'suspended', label: t('users.list.filterSuspended') }
          ]}
        />
      </div>

      <Card className="min-w-0 overflow-hidden">
        {query.isError && !rows ? (
          <ErrorState
            title={t('users.list.errorTitle')}
            message={isApiError(query.error) ? query.error.message : undefined}
            requestId={isApiError(query.error) ? query.error.requestId : undefined}
            onRetry={() => void query.refetch()}
          />
        ) : !rows ? (
          <ListSkeleton />
        ) : rows.length === 0 ? (
          filtered ? (
            <EmptyState
              icon={UsersRound}
              title={t('users.list.noMatchTitle')}
              body={t('users.list.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => update({ q: null, status: null, page: null })}>
                  {t('users.list.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState icon={UsersRound} title={t('users.list.emptyTitle')} body={t('users.list.emptyBody')} />
          )
        ) : (
          <div className={cn('transition-opacity', query.isPlaceholderData && 'opacity-60')} aria-busy={query.isFetching || undefined}>
            {/* Table on ≥640px, card list below. */}
            <div className="hidden overflow-x-auto sm:block">
              <table className="w-full text-left text-[13px]">
                <thead>
                  <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                    <th scope="col" className="px-4 py-2.5 font-medium">{t('users.list.colUser')}</th>
                    <th scope="col" className="hidden px-3 py-2.5 font-medium lg:table-cell">{t('users.list.colPhone')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('users.list.colWorkspaces')}</th>
                    <th scope="col" className="hidden px-3 py-2.5 font-medium md:table-cell">{t('users.list.colSecurity')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('users.list.colStatus')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('users.list.colLastLogin')}</th>
                    <th scope="col" className="hidden px-4 py-2.5 text-right font-medium xl:table-cell">{t('users.list.colCreated')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((u) => (
                    <tr key={u.id} onClick={() => open(u)} className="cursor-pointer border-b transition-colors last:border-0 hover:bg-muted/50">
                      <td className="max-w-[320px] px-4 py-2.5">
                        <div className="flex items-center gap-3">
                          <Avatar name={u.displayName} />
                          <div className="min-w-0">
                            <div className="flex items-center gap-1.5">
                              <Link
                                to={`/crm/owner/users/${u.id}`}
                                onClick={(e) => e.stopPropagation()}
                                className="truncate font-medium text-foreground hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                              >
                                {u.displayName}
                              </Link>
                              {u.isPlatformOwner ? <Badge tone="warning">{t('users.badge.owner')}</Badge> : null}
                              {me?.identity.id === u.id ? <Badge>{t('users.badge.you')}</Badge> : null}
                            </div>
                            {u.email ? <p className="truncate text-xs text-muted-foreground">{u.email}</p> : null}
                          </div>
                        </div>
                      </td>
                      <td className="hidden whitespace-nowrap px-3 py-2.5 tabular-nums text-muted-foreground lg:table-cell">
                        {u.phone ?? <span className="text-muted-foreground/60">—</span>}
                      </td>
                      <td className="px-3 py-2.5">
                        <WorkspaceChips names={wsNames(u.workspaceNames)} />
                      </td>
                      <td className="hidden px-3 py-2.5 md:table-cell">
                        <MfaBadge on={u.mfaEnrolled} />
                      </td>
                      <td className="px-3 py-2.5">
                        <Badge tone={userStatusTone[u.status] ?? 'neutral'}>{t(`users.status.${u.status}`, { defaultValue: u.status })}</Badge>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 text-muted-foreground">
                        {u.lastLoginAt ? (
                          <Tooltip content={new Date(u.lastLoginAt).toLocaleString('en-IN')} side="top">
                            <span>{relativeTime(u.lastLoginAt)}</span>
                          </Tooltip>
                        ) : (
                          <span className="text-muted-foreground/70">{t('users.list.never')}</span>
                        )}
                      </td>
                      <td className="hidden whitespace-nowrap px-4 py-2.5 text-right text-muted-foreground xl:table-cell">{shortDate(u.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <ul className="divide-y sm:hidden">
              {rows.map((u) => (
                <li key={u.id}>
                  <Link
                    to={`/crm/owner/users/${u.id}`}
                    className="flex items-start gap-3 px-4 py-3 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
                  >
                    <Avatar name={u.displayName} className="mt-0.5" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-2">
                        <p className="truncate text-[13px] font-medium text-foreground">{u.displayName}</p>
                        <Badge tone={userStatusTone[u.status] ?? 'neutral'}>{t(`users.status.${u.status}`, { defaultValue: u.status })}</Badge>
                      </div>
                      {u.email || u.phone ? <p className="truncate text-xs text-muted-foreground">{u.email ?? u.phone}</p> : null}
                      <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                        {u.isPlatformOwner ? <Badge tone="warning">{t('users.badge.owner')}</Badge> : null}
                        <MfaBadge on={u.mfaEnrolled} />
                        <WorkspaceChips names={wsNames(u.workspaceNames)} max={1} />
                      </div>
                      <p className="mt-1.5 text-xs text-muted-foreground">
                        {u.lastLoginAt ? t('users.list.lastLogin', { time: relativeTime(u.lastLoginAt) }) : t('users.list.lastLoginNever')}
                      </p>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        )}

        {rows && total > 0 ? (
          <Pagination
            offset={offset}
            count={rows.length}
            total={total}
            onPrev={() => update({ page: page - 1 <= 1 ? null : String(page - 1) })}
            onNext={() => update({ page: String(page + 1) })}
          />
        ) : null}
      </Card>
    </>
  );
}

function WorkspaceChips({ names, max = 2 }: { names: string[]; max?: number }) {
  const { t } = useTranslation();
  if (names.length === 0) return <span className="text-xs text-muted-foreground/70">{t('users.list.noWorkspaces')}</span>;
  const shown = names.slice(0, max);
  const rest = names.slice(max);
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1">
      {shown.map((n) => (
        <span key={n} className="max-w-[160px] truncate rounded border bg-muted/50 px-1.5 py-px text-[11px] font-medium text-foreground">
          {n}
        </span>
      ))}
      {rest.length > 0 ? (
        <Tooltip content={rest.join(', ')} side="top">
          <span tabIndex={0} className="rounded px-1 text-[11px] font-medium text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {t('users.list.moreWorkspaces', { count: rest.length })}
          </span>
        </Tooltip>
      ) : null}
    </div>
  );
}

function MfaBadge({ on }: { on: boolean }) {
  const { t } = useTranslation();
  return on ? (
    <Badge tone="success">
      <ShieldCheck className="size-3" aria-hidden /> {t('users.badge.mfaOn')}
    </Badge>
  ) : (
    <Badge>
      <ShieldOff className="size-3" aria-hidden /> {t('users.badge.mfaOff')}
    </Badge>
  );
}

function Pagination({ offset, count, total, onPrev, onNext }: { offset: number; count: number; total: number; onPrev: () => void; onNext: () => void }) {
  const { t } = useTranslation();
  const from = count === 0 ? 0 : offset + 1;
  const to = offset + count;
  return (
    <div className="flex items-center justify-between gap-3 border-t px-4 py-2.5">
      <p className="text-xs tabular-nums text-muted-foreground">
        {t('users.list.range', { from: from.toLocaleString('en-IN'), to: to.toLocaleString('en-IN'), total: total.toLocaleString('en-IN') })}
      </p>
      <div className="flex items-center gap-1">
        <Button variant="outline" size="icon-sm" onClick={onPrev} disabled={offset === 0} aria-label={t('users.list.prev')}>
          <ChevronLeft />
        </Button>
        <Button variant="outline" size="icon-sm" onClick={onNext} disabled={to >= total} aria-label={t('users.list.next')}>
          <ChevronRight />
        </Button>
      </div>
    </div>
  );
}

function ListSkeleton() {
  return (
    <ul className="divide-y" aria-hidden>
      {Array.from({ length: 8 }).map((_, i) => (
        <li key={i} className="flex items-center gap-3 px-4 py-3">
          <Skeleton className="size-8 rounded-full" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-56 max-w-full" />
          </div>
          <Skeleton className="hidden h-5 w-24 sm:block" />
          <Skeleton className="hidden h-5 w-16 sm:block" />
          <Skeleton className="h-5 w-14" />
        </li>
      ))}
    </ul>
  );
}
