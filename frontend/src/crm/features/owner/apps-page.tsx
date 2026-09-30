import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { LayoutGrid } from 'lucide-react';
import { platformApi } from '@crm/api/endpoints';
import { Badge, Card } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SearchInput } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { relativeTime } from '@crm/lib/utils';
import { OwnerScopeBar, ownerFilterParams, useOwnerFilter } from './owner-filter';

/** /crm/owner/apps — every app installed in every product (D-73). */
export function AppsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('owner.apps.title'));
  const filter = useOwnerFilter();
  const q = useQuery({ queryKey: ['platform', 'apps', filter.product, filter.app], queryFn: () => platformApi.apps(ownerFilterParams(filter)) });
  const [search, setSearch] = useState('');
  const rows = useMemo(() => {
    const s = search.trim().toLowerCase();
    return (q.data ?? []).filter((a) => !s || `${a.name} ${a.key} ${a.workspaceName} ${a.workspaceCode}`.toLowerCase().includes(s));
  }, [q.data, search]);
  const products = new Set((q.data ?? []).map((a) => a.workspaceId)).size;
  return (
    <PageContainer>
      <PageHeader title={t('owner.apps.title')} description={t('owner.apps.subtitle')} />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex w-full max-w-full shrink-0 flex-wrap items-center gap-2 sm:w-auto">
          <SearchInput value={search} onChange={setSearch} placeholder={t('owner.apps.search')} className="sm:w-64" />
          <OwnerScopeBar />
        </div>
        {q.data ? <p className="text-[13px] text-muted-foreground">{t('owner.apps.count', { count: q.data.length })} {t('owner.apps.inProducts', { count: products })}</p> : null}
      </div>
      <Card className="overflow-hidden">
        {q.isPending ? (
          <Skeleton className="m-5 h-40" />
        ) : q.isError ? (
          <ErrorState title={t('owner.apps.error')} onRetry={() => void q.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState icon={LayoutGrid} title={t('owner.apps.emptyTitle')} body={t('owner.apps.emptyBody')} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[13px]">
              <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 font-medium">{t('owner.apps.colApp')}</th>
                  <th className="px-4 py-2.5 font-medium">{t('owner.apps.colProduct')}</th>
                  <th className="px-4 py-2.5 font-medium">{t('owner.apps.colVersion')}</th>
                  <th className="px-4 py-2.5 text-right font-medium">{t('owner.apps.colObjects')}</th>
                  <th className="px-4 py-2.5 font-medium">{t('owner.apps.colStatus')}</th>
                  <th className="px-4 py-2.5 text-right font-medium">{t('owner.apps.colInstalled')}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((a) => (
                  <tr key={`${a.workspaceId}:${a.productId}`} className="hover:bg-muted/40">
                    <td className="px-4 py-3">
                      <Link
                        to={`/crm/owner/products/${encodeURIComponent(a.productId)}?tab=setup&project=${encodeURIComponent(a.workspaceId)}`}
                        className="font-medium text-primary hover:underline"
                      >
                        {a.name}
                      </Link>
                      <p className="font-mono text-xs text-muted-foreground">{a.key}</p>
                    </td>
                    <td className="px-4 py-3">
                      <Link to={`/crm/owner/workspaces/${encodeURIComponent(a.workspaceId)}`} className="font-medium text-foreground hover:text-primary hover:underline">
                        {a.workspaceName}
                      </Link>
                      <p className="font-mono text-xs text-muted-foreground">{a.workspaceCode}</p>
                    </td>
                    <td className="px-4 py-3">
                      v{a.version}
                      {a.latestVersion > a.version ? <Badge tone="warning" className="ml-2">{t('owner.apps.newer', { version: a.latestVersion })}</Badge> : null}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums">{a.modules}</td>
                    <td className="px-4 py-3">
                      <Badge tone={a.status === 'active' ? 'success' : 'neutral'}>{t(`owner.apps.status.${a.status}`)}</Badge>
                    </td>
                    <td className="px-4 py-3 text-right text-muted-foreground">{relativeTime(a.installedAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </PageContainer>
  );
}
