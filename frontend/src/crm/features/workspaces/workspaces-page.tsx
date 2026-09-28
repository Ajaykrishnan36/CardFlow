import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Building2, Plus } from 'lucide-react';
import { workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { WorkspaceStatusBadge, WorkspaceTile } from './workspace-ui';

type StatusFilter = '' | 'active' | 'suspended';

export function WorkspacesPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('workspaces.list.title'));
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const q = params.get('q') ?? '';
  const status = (params.get('status') ?? '') as StatusFilter;

  const setParam = useCallback(
    (key: string, value: string) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set(key, value);
          else next.delete(key);
          return next;
        },
        { replace: true }
      );
    },
    [setParams]
  );
  const onSearch = useCallback((v: string) => setParam('q', v), [setParam]);

  const list = useQuery({
    queryKey: ['workspaces', { q, status }],
    queryFn: () => workspacesApi.list({ q, status }),
    placeholderData: keepPreviousData
  });
  const rows = list.data?.data;
  const filtered = Boolean(q || status);

  const provisionButton = (
    <Button asChild>
      <Link to="/crm/owner/workspaces/new">
        <Plus /> {t('workspaces.list.provision')}
      </Link>
    </Button>
  );

  return (
    <PageContainer>
      <PageHeader title={t('workspaces.list.title')} description={t('workspaces.list.description')} actions={provisionButton} />

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onChange={onSearch} placeholder={t('workspaces.list.search')} />
        <SegmentedFilter<StatusFilter>
          value={status}
          onChange={(v) => setParam('status', v)}
          options={[
            { value: '', label: t('workspaces.list.filterAll') },
            { value: 'active', label: t('status.active') },
            { value: 'suspended', label: t('status.suspended') }
          ]}
        />
      </div>

      <Card className="min-w-0 overflow-hidden">
        {list.isError ? (
          <ErrorState
            title={t('workspaces.list.errorTitle')}
            message={isApiError(list.error) ? list.error.message : undefined}
            requestId={isApiError(list.error) ? list.error.requestId : undefined}
            onRetry={() => void list.refetch()}
          />
        ) : !rows ? (
          <div className="divide-y">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-5 py-3.5">
                <Skeleton className="size-8 rounded-md" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-44" />
                  <Skeleton className="h-3 w-24" />
                </div>
                <Skeleton className="hidden h-5 w-16 rounded-full sm:block" />
              </div>
            ))}
          </div>
        ) : rows.length === 0 ? (
          filtered ? (
            <EmptyState
              icon={Building2}
              title={t('workspaces.list.noMatchTitle')}
              body={t('workspaces.list.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => setParams(new URLSearchParams(), { replace: true })}>
                  {t('workspaces.list.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState icon={Building2} title={t('workspaces.list.emptyTitle')} body={t('workspaces.list.emptyBody')} action={provisionButton} />
          )
        ) : (
          <>
            <div className="hidden overflow-x-auto sm:block">
              <table className="w-full text-left text-[13px]">
                <thead>
                  <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                    <th className="px-5 py-2.5 font-medium">{t('workspaces.list.colWorkspace')}</th>
                    <th className="px-3 py-2.5 font-medium">{t('workspaces.list.colStatus')}</th>
                    <th className="px-3 py-2.5 text-right font-medium">{t('workspaces.list.colProducts')}</th>
                    <th className="px-3 py-2.5 text-right font-medium">{t('workspaces.list.colMembers')}</th>
                    <th className="px-3 py-2.5 text-right font-medium">{t('workspaces.list.colInvites')}</th>
                    <th className="px-5 py-2.5 text-right font-medium">{t('workspaces.list.colCreated')}</th>
                  </tr>
                </thead>
                <tbody className={list.isPlaceholderData ? 'opacity-60 transition-opacity' : undefined}>
                  {rows.map((w) => (
                    <tr key={w.id} className="cursor-pointer border-b last:border-0 hover:bg-muted/50" onClick={() => navigate(`/crm/owner/workspaces/${w.id}`)}>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-3">
                          <WorkspaceTile name={w.name} size="sm" />
                          <div className="min-w-0">
                            <Link
                              to={`/crm/owner/workspaces/${w.id}`}
                              onClick={(e) => e.stopPropagation()}
                              className="block truncate font-medium text-foreground hover:underline"
                            >
                              {w.name}
                            </Link>
                            <p className="truncate font-mono text-xs text-muted-foreground">{w.code}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <WorkspaceStatusBadge status={w.status} />
                      </td>
                      <td className="px-3 py-3 text-right tabular-nums">{w.products}</td>
                      <td className="px-3 py-3 text-right tabular-nums">{w.members}</td>
                      <td className="px-3 py-3 text-right tabular-nums">
                        {w.pendingInvites > 0 ? <span className="font-medium text-primary">{w.pendingInvites}</span> : <span className="text-muted-foreground">0</span>}
                      </td>
                      <td className="px-5 py-3 text-right text-muted-foreground">{relativeTime(w.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <ul className="divide-y sm:hidden">
              {rows.map((w) => (
                <li key={w.id}>
                  <Link to={`/crm/owner/workspaces/${w.id}`} className="flex items-start gap-3 px-4 py-3 hover:bg-muted/50">
                    <WorkspaceTile name={w.name} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-2">
                        <p className="truncate font-medium text-foreground">{w.name}</p>
                        <WorkspaceStatusBadge status={w.status} />
                      </div>
                      <p className="truncate font-mono text-xs text-muted-foreground">{w.code}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {t('workspaces.list.productsCount', { count: w.products })} · {t('workspaces.list.membersCount', { count: w.members })}
                        {w.pendingInvites > 0 ? ` · ${t('workspaces.list.invitesCount', { count: w.pendingInvites })}` : ''}
                      </p>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>
    </PageContainer>
  );
}
