import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Boxes, Plus } from 'lucide-react';
import { productsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ProductStatus, ProductSummary } from '@crm/api/types';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { CreateProductDialog } from './create-product-dialog';
import { ProductIcon } from './product-icon';
import { productStatusTone } from './product-utils';

type StatusFilter = '' | ProductStatus;

export function ProductsPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('products.list.title'));
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const q = params.get('q') ?? '';
  const status = (params.get('status') ?? '') as StatusFilter;
  const [createOpen, setCreateOpen] = useState(false);

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
    queryKey: ['products', { q, status }],
    queryFn: () => productsApi.list({ q, status }),
    placeholderData: keepPreviousData
  });
  const rows = list.data?.data;
  const filtered = Boolean(q || status);
  const open = (p: ProductSummary) => navigate(`/crm/owner/products/${p.id}`);

  return (
    <PageContainer>
      <PageHeader
        title={t('products.list.title')}
        description={t('products.list.description')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus /> {t('products.list.newProduct')}
          </Button>
        }
      />

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={q} onChange={onSearch} placeholder={t('products.list.search')} />
        <SegmentedFilter<StatusFilter>
          value={status}
          onChange={(v) => setParam('status', v)}
          options={[
            { value: '', label: t('products.list.filterAll') },
            { value: 'active', label: t('status.active') },
            { value: 'draft', label: t('status.draft') },
            { value: 'archived', label: t('status.archived') }
          ]}
        />
      </div>

      <Card className="min-w-0 overflow-hidden">
        {list.isError ? (
          <ErrorState
            title={t('products.list.errorTitle')}
            message={isApiError(list.error) ? list.error.message : undefined}
            requestId={isApiError(list.error) ? list.error.requestId : undefined}
            onRetry={() => void list.refetch()}
          />
        ) : !rows ? (
          <ListSkeleton />
        ) : rows.length === 0 ? (
          filtered ? (
            <EmptyState
              icon={Boxes}
              title={t('products.list.noMatchTitle')}
              body={t('products.list.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => setParams(new URLSearchParams(), { replace: true })}>
                  {t('products.list.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={Boxes}
              title={t('products.list.emptyTitle')}
              body={t('products.list.emptyBody')}
              action={
                <Button onClick={() => setCreateOpen(true)}>
                  <Plus /> {t('products.list.newProduct')}
                </Button>
              }
            />
          )
        ) : (
          <>
            <div className="hidden overflow-x-auto sm:block">
              <table className="w-full text-left text-[13px]">
                <thead>
                  <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                    <th className="px-5 py-2.5 font-medium">{t('products.list.colProduct')}</th>
                    <th className="px-3 py-2.5 font-medium">{t('products.list.colStatus')}</th>
                    <th className="px-3 py-2.5 font-medium">{t('products.list.colVersion')}</th>
                    <th className="px-3 py-2.5 text-right font-medium">{t('products.list.colWorkspaces')}</th>
                    <th className="px-5 py-2.5 text-right font-medium">{t('products.list.colUpdated')}</th>
                  </tr>
                </thead>
                <tbody className={list.isPlaceholderData ? 'opacity-60 transition-opacity' : undefined}>
                  {rows.map((p) => (
                    <tr key={p.id} className="cursor-pointer border-b last:border-0 hover:bg-muted/50" onClick={() => open(p)}>
                      <td className="px-5 py-3">
                        <div className="flex items-center gap-3">
                          <ProductIcon icon={p.icon} size="sm" />
                          <div className="min-w-0">
                            <Link
                              to={`/crm/owner/products/${p.id}`}
                              onClick={(e) => e.stopPropagation()}
                              className="block truncate font-medium text-foreground hover:underline"
                            >
                              {p.name}
                            </Link>
                            <p className="truncate font-mono text-xs text-muted-foreground">{p.key}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-3 py-3">
                        <StatusCell p={p} />
                      </td>
                      <td className="px-3 py-3 font-mono text-xs tabular-nums text-muted-foreground">{p.currentVersion ? `v${p.currentVersion}` : '—'}</td>
                      <td className="px-3 py-3 text-right tabular-nums">{p.workspaces}</td>
                      <td className="px-5 py-3 text-right text-muted-foreground">{relativeTime(p.updatedAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <ul className="divide-y sm:hidden">
              {rows.map((p) => (
                <li key={p.id}>
                  <Link to={`/crm/owner/products/${p.id}`} className="flex items-start gap-3 px-4 py-3 hover:bg-muted/50">
                    <ProductIcon icon={p.icon} size="sm" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-2">
                        <p className="truncate font-medium text-foreground">{p.name}</p>
                        <span className="shrink-0 font-mono text-xs text-muted-foreground">{p.currentVersion ? `v${p.currentVersion}` : '—'}</span>
                      </div>
                      <p className="truncate font-mono text-xs text-muted-foreground">{p.key}</p>
                      <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                        <StatusCell p={p} />
                        <span>{t('products.list.workspacesCount', { count: p.workspaces })}</span>
                        <span>· {relativeTime(p.updatedAt)}</span>
                      </div>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>
      {rows && rows.length > 0 && list.data && list.data.total > rows.length ? (
        <p className="mt-3 text-xs text-muted-foreground">{t('products.list.showing', { shown: rows.length, total: list.data.total })}</p>
      ) : null}

      <CreateProductDialog open={createOpen} onOpenChange={setCreateOpen} />
    </PageContainer>
  );
}

function StatusCell({ p }: { p: ProductSummary }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      <Badge tone={productStatusTone[p.status]}>{t(`status.${p.status}`)}</Badge>
      {p.hasUnpublishedChanges && p.status !== 'archived' ? (
        <span className="inline-flex items-center gap-1 text-[11px] font-medium text-warning">
          <span className="size-1.5 rounded-full bg-warning" aria-hidden />
          {t('products.list.unpublished')}
        </span>
      ) : null}
    </span>
  );
}

function ListSkeleton() {
  return (
    <div className="divide-y">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-5 py-3.5">
          <Skeleton className="size-8 rounded-md" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-24" />
          </div>
          <Skeleton className="hidden h-5 w-16 rounded-full sm:block" />
          <Skeleton className="hidden h-3.5 w-10 sm:block" />
        </div>
      ))}
    </div>
  );
}
