import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useInfiniteQuery } from '@tanstack/react-query';
import { Funnel, ScrollText, X } from 'lucide-react';
import { auditApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AuditEntry } from '@crm/api/types';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { PageContainer, PageHeader, SearchInput } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { absoluteTime, actionTone, actorLabel, humanizeAction, shortId, type ActionTone } from './audit-format';

const PAGE_SIZE = 50;

const dotClass: Record<ActionTone, string> = {
  success: 'bg-success',
  danger: 'bg-danger',
  warning: 'bg-warning',
  primary: 'bg-primary',
  neutral: 'bg-muted-foreground/50'
};

export function AuditPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('audit.title'));
  const [params, setParams] = useSearchParams();
  const entityId = (params.get('entityId') ?? '').trim();

  const setEntity = useCallback(
    (v: string) =>
      setParams(
        (prev) => {
          const sp = new URLSearchParams(prev);
          if (v.trim()) sp.set('entityId', v.trim());
          else sp.delete('entityId');
          return sp;
        },
        { replace: true }
      ),
    [setParams]
  );

  const q = useInfiniteQuery({
    queryKey: ['platform', 'audit', { entityId }],
    queryFn: ({ pageParam }) => auditApi.list({ limit: PAGE_SIZE, before: pageParam, entityId: entityId || undefined }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.nextBefore ?? undefined
  });

  const rows = q.data?.pages.flatMap((p) => p.data);

  return (
    <PageContainer>
      <PageHeader title={t('audit.title')} description={t('audit.description')} />

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <SearchInput value={entityId} onChange={setEntity} placeholder={t('audit.filterPlaceholder')} />
        {rows && rows.length > 0 ? (
          <p className="text-xs tabular-nums text-muted-foreground">
            {t('audit.count', { count: rows.length })}
            {q.hasNextPage ? '+' : ''}
          </p>
        ) : null}
      </div>

      <Card className="min-w-0 overflow-hidden">
        {q.isError && !rows ? (
          <ErrorState
            title={t('audit.errorTitle')}
            message={isApiError(q.error) ? q.error.message : undefined}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        ) : !rows ? (
          <AuditSkeleton />
        ) : rows.length === 0 ? (
          entityId ? (
            <EmptyState
              icon={ScrollText}
              title={t('audit.emptyFilteredTitle')}
              body={t('audit.emptyFilteredBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => setEntity('')}>
                  <X /> {t('audit.clearFilter')}
                </Button>
              }
            />
          ) : (
            <EmptyState icon={ScrollText} title={t('audit.emptyTitle')} body={t('audit.emptyBody')} />
          )
        ) : (
          <>
            <div className="hidden overflow-x-auto md:block">
              <table className="w-full text-left text-[13px]">
                <thead>
                  <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                    <th scope="col" className="w-32 px-4 py-2.5 font-medium">{t('audit.colTime')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('audit.colActor')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('audit.colAction')}</th>
                    <th scope="col" className="px-3 py-2.5 font-medium">{t('audit.colEntity')}</th>
                    <th scope="col" className="hidden px-3 py-2.5 font-medium lg:table-cell">{t('audit.colWorkspace')}</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">{t('audit.colIp')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((e) => (
                    <tr key={e.id} className="border-b align-top last:border-0 hover:bg-muted/40">
                      <td className="whitespace-nowrap px-4 py-2.5 text-muted-foreground">
                        <TimeCell iso={e.createdAt} />
                      </td>
                      <td className="max-w-[180px] px-3 py-2.5">
                        <ActorCell entry={e} />
                      </td>
                      <td className="max-w-[300px] px-3 py-2.5">
                        <ActionCell entry={e} />
                      </td>
                      <td className="px-3 py-2.5">
                        <EntityCell entry={e} active={entityId} onFilter={setEntity} />
                      </td>
                      <td className="hidden max-w-[180px] truncate px-3 py-2.5 text-muted-foreground lg:table-cell">
                        {e.workspaceName ?? <span className="text-muted-foreground/60">—</span>}
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-muted-foreground">
                        {e.ip ?? <span className="font-sans text-muted-foreground/60">—</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <ul className="divide-y md:hidden">
              {rows.map((e) => (
                <li key={e.id} className="space-y-1.5 px-4 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <ActionCell entry={e} />
                    <span className="shrink-0 text-xs text-muted-foreground">
                      <TimeCell iso={e.createdAt} />
                    </span>
                  </div>
                  <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{actorLabel(t, e)}</span>
                    {e.workspaceName ? (
                      <>
                        <span aria-hidden>·</span>
                        <span>{e.workspaceName}</span>
                      </>
                    ) : null}
                    {e.ip ? (
                      <>
                        <span aria-hidden>·</span>
                        <span className="font-mono text-[11px]">{e.ip}</span>
                      </>
                    ) : null}
                  </p>
                  {e.entityType || e.entityId ? <EntityCell entry={e} active={entityId} onFilter={setEntity} /> : null}
                </li>
              ))}
            </ul>

            <div className="flex flex-col items-center gap-2 border-t px-4 py-3">
              {q.isFetchNextPageError ? <p className="text-xs text-danger">{t('audit.loadMoreError')}</p> : null}
              {q.hasNextPage ? (
                <Button variant="outline" size="sm" onClick={() => void q.fetchNextPage()} loading={q.isFetchingNextPage}>
                  {t('audit.loadMore')}
                </Button>
              ) : (
                <p className="text-xs text-muted-foreground">{t('audit.end')}</p>
              )}
            </div>
          </>
        )}
      </Card>
    </PageContainer>
  );
}

function TimeCell({ iso }: { iso: string }) {
  return (
    <Tooltip content={absoluteTime(iso)} side="top">
      <time dateTime={iso} tabIndex={0} className="rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        {relativeTime(iso)}
      </time>
    </Tooltip>
  );
}

function ActorCell({ entry: e }: { entry: AuditEntry }) {
  const { t } = useTranslation();
  const system = !e.actorName?.trim();
  return (
    <div className="min-w-0">
      <p className={cn('truncate', system ? 'text-muted-foreground' : 'font-medium text-foreground')}>{actorLabel(t, e)}</p>
      {!system && e.actorKind && e.actorKind !== 'identity' ? <p className="text-[11px] text-muted-foreground">{e.actorKind}</p> : null}
    </div>
  );
}

function ActionCell({ entry: e }: { entry: AuditEntry }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-w-0 items-start gap-2">
      <span className={cn('mt-[7px] size-1.5 shrink-0 rounded-full', dotClass[actionTone(e.action)])} aria-hidden />
      <div className="min-w-0">
        <p className="text-[13px] font-medium text-foreground">{humanizeAction(t, e.action)}</p>
        <p className="truncate font-mono text-[11px] text-muted-foreground">{e.action}</p>
      </div>
    </div>
  );
}

function EntityCell({ entry: e, active, onFilter }: { entry: AuditEntry; active: string; onFilter: (id: string) => void }) {
  const { t } = useTranslation();
  if (!e.entityType && !e.entityId) return <span className="text-muted-foreground/60">—</span>;
  const type = e.entityType ?? '';
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {type ? <Badge>{type}</Badge> : null}
      {e.entityId ? (
        e.entityId === active ? (
          <span className="font-mono text-xs text-foreground" title={e.entityId}>
            {shortId(e.entityId)}
          </span>
        ) : (
          <Tooltip content={t('audit.filterByEntity', { type: type || 'entity' })} side="top">
            <button
              type="button"
              onClick={() => onFilter(e.entityId ?? '')}
              className="group inline-flex items-center gap-1 rounded font-mono text-xs text-muted-foreground hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={`${t('audit.filterByEntity', { type: type || 'entity' })}: ${e.entityId}`}
            >
              {shortId(e.entityId)}
              <Funnel className="size-3 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100" aria-hidden />
            </button>
          </Tooltip>
        )
      ) : null}
    </div>
  );
}

function AuditSkeleton() {
  return (
    <ul className="divide-y" aria-hidden>
      {Array.from({ length: 10 }).map((_, i) => (
        <li key={i} className="flex items-center gap-4 px-4 py-3">
          <Skeleton className="h-3.5 w-16" />
          <Skeleton className="h-3.5 w-24" />
          <div className="flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-28" />
          </div>
          <Skeleton className="hidden h-4 w-20 md:block" />
          <Skeleton className="hidden h-3.5 w-24 md:block" />
        </li>
      ))}
    </ul>
  );
}
