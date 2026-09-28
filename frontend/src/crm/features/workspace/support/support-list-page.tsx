import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useSearchParams } from 'react-router-dom';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { CornerDownRight, LifeBuoy, Phone, RefreshCw } from 'lucide-react';
import { workspaceSupportApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { SupportTicket, TicketStatus } from '@crm/api/types';
import { Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader, SearchInput, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { scopedLookupHref, useRecordScope } from '@crm/features/records/record-scope';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { ticketAccess, useWorkspace } from '../workspace-context';
import { CategoryChip, SUPPORT_REFRESH_MS, supportKeys, supportPath, TICKET_STATUSES, ticketNumber, TicketStatusBadge } from './support-utils';

type Filter = 'all' | TicketStatus;

/** /crm/w/:ws/support — tickets raised in the connected app. Refreshes on its own. */
export function SupportListPage() {
  const { context } = useWorkspace();
  if (!ticketAccess(context).read) return <RecordNoAccess />;
  return <SupportListView />;
}

function SupportListView() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  useDocumentTitle(t('workspaceApp.support.title'));
  const [sp, setSp] = useSearchParams();
  const q = sp.get('q') ?? '';
  const raw = sp.get('status');
  const status: Filter = raw && (TICKET_STATUSES as string[]).includes(raw) ? (raw as TicketStatus) : 'all';

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
  const onSearch = useCallback((v: string) => patch({ q: v }), [patch]);

  const params = { q: q || undefined, status: status === 'all' ? undefined : status };
  const list = useQuery({
    queryKey: supportKeys.list(code, params),
    queryFn: () => workspaceSupportApi(code).list(params),
    placeholderData: keepPreviousData,
    refetchInterval: SUPPORT_REFRESH_MS
  });

  if (isApiError(list.error) && list.error.status === 403) return <RecordNoAccess />;

  const counts = list.data?.counts;
  const rows = list.data?.data;
  const filtered = Boolean(q) || status !== 'all';

  return (
    <PageContainer wide>
      <PageHeader
        title={t('workspaceApp.support.title')}
        description={t('workspaceApp.support.subtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <LifeBuoy className="size-5" aria-hidden />
          </span>
        }
        actions={
          <Button variant="outline" size="sm" onClick={() => void list.refetch()} loading={list.isFetching && !list.isPending}>
            {!list.isFetching || list.isPending ? <RefreshCw /> : null} {t('workspaceApp.support.refresh')}
          </Button>
        }
      />

      <Card className="min-w-0 overflow-hidden">
        <div className="flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center">
          <SearchInput value={q} onChange={onSearch} placeholder={t('workspaceApp.support.search')} className="lg:max-w-xs" />
          <SegmentedFilter<Filter>
            value={status}
            onChange={(v) => patch({ status: v === 'all' ? null : v })}
            options={[
              { value: 'all', label: t('workspaceApp.support.filterAll'), count: counts?.all },
              ...TICKET_STATUSES.map((s) => ({ value: s, label: t(`workspaceApp.support.status.${s}`), count: counts?.[s] }))
            ]}
          />
          <p className="text-xs text-muted-foreground lg:ml-auto" aria-live="polite">
            {t('workspaceApp.support.autoRefresh')}
          </p>
        </div>

        {list.isError ? (
          <ErrorState
            title={t('workspaceApp.support.errorTitle')}
            message={isApiError(list.error) ? list.error.message : undefined}
            requestId={isApiError(list.error) ? list.error.requestId : undefined}
            onRetry={() => void list.refetch()}
          />
        ) : !rows ? (
          <ListSkeleton />
        ) : rows.length === 0 ? (
          filtered ? (
            <EmptyState
              icon={LifeBuoy}
              title={t('workspaceApp.support.noMatchTitle')}
              body={t('workspaceApp.support.noMatchBody')}
              action={
                <Button variant="outline" size="sm" onClick={() => patch({ q: null, status: null })}>
                  {t('workspaceApp.support.clearFilters')}
                </Button>
              }
            />
          ) : (
            <EmptyState icon={LifeBuoy} title={t('workspaceApp.support.emptyTitle')} body={t('workspaceApp.support.emptyBody')} />
          )
        ) : (
          <div className={cn('transition-opacity', list.isPlaceholderData && 'opacity-60')}>
            <div className="hidden grid-cols-[minmax(0,2.2fr)_minmax(0,1.3fr)_7.5rem_6.5rem] gap-4 border-b bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground md:grid">
              <span>{t('workspaceApp.support.colTicket')}</span>
              <span>{t('workspaceApp.support.colFrom')}</span>
              <span>{t('workspaceApp.support.colStatus')}</span>
              <span className="text-right">{t('workspaceApp.support.colCreated')}</span>
            </div>
            <ul className="divide-y">
              {rows.map((tk) => (
                <TicketRow key={tk.id} ticket={tk} code={code} />
              ))}
            </ul>
          </div>
        )}
      </Card>
    </PageContainer>
  );
}

function TicketRow({ ticket: tk, code }: { ticket: SupportTicket; code: string }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const href = supportPath(code, tk.id);
  const accountHref = tk.account ? scopedLookupHref(scope, 'accounts', tk.account.id) : null;
  return (
    // Stretched main link; the person link sits above it (z-10), so links never nest.
    <li className="group relative grid gap-x-4 gap-y-1.5 px-4 py-3 transition-colors hover:bg-muted/50 md:grid-cols-[minmax(0,2.2fr)_minmax(0,1.3fr)_7.5rem_6.5rem] md:items-start">
      <div className="min-w-0">
        <div className="flex items-start justify-between gap-2 md:block">
          <Link
            to={href}
            className="block min-w-0 truncate text-[13px] font-medium text-foreground after:absolute after:inset-0 after:content-[''] group-hover:text-primary focus-visible:outline-none focus-visible:after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-inset focus-visible:after:ring-ring"
          >
            {tk.subject || t('workspaceApp.support.noSubject')}
          </Link>
          <span className="shrink-0 md:hidden">
            <TicketStatusBadge status={tk.status} />
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          <CategoryChip category={tk.category} />
          <span className="font-mono text-[11px] text-muted-foreground">{ticketNumber(tk.id)}</span>
          <span className="text-[11px] text-muted-foreground md:hidden">· {relativeTime(tk.createdAt)}</span>
        </div>
        <p className="mt-1 flex min-w-0 items-start gap-1 text-xs text-muted-foreground">
          <CornerDownRight className="mt-0.5 size-3 shrink-0" aria-hidden />
          <span className="line-clamp-1">{tk.reply ? tk.reply : <span className="italic">{t('workspaceApp.support.noReply')}</span>}</span>
        </p>
      </div>
      <div className="min-w-0 text-xs">
        <p className="flex min-w-0 items-center gap-1.5">
          {accountHref ? (
            <Link to={accountHref} className="relative z-10 truncate font-medium text-primary hover:underline">
              {tk.user.name || tk.account?.label}
            </Link>
          ) : (
            <span className="truncate font-medium text-foreground">{tk.user.name || '—'}</span>
          )}
          {tk.user.role ? <span className="shrink-0 rounded bg-muted px-1.5 text-[11px] text-muted-foreground">{tk.user.role}</span> : null}
        </p>
        {tk.user.phone ? (
          <p className="mt-0.5 flex items-center gap-1 tabular-nums text-muted-foreground">
            <Phone className="size-3" aria-hidden />
            {tk.user.phone}
          </p>
        ) : null}
      </div>
      <div className="hidden md:block">
        <TicketStatusBadge status={tk.status} />
      </div>
      <p className="hidden text-right text-xs text-muted-foreground md:block">
        <time dateTime={tk.createdAt} title={new Date(tk.createdAt).toLocaleString('en-IN')}>
          {relativeTime(tk.createdAt)}
        </time>
      </p>
    </li>
  );
}

function ListSkeleton() {
  return (
    <div aria-busy="true" className="divide-y">
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} className="grid gap-3 px-4 py-3 md:grid-cols-[2.2fr_1.3fr_7.5rem_6.5rem]">
          <div className="space-y-1.5">
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3 w-1/3" />
          </div>
          <Skeleton className="h-3.5 w-2/3" />
          <Skeleton className="hidden h-5 w-16 md:block" />
          <Skeleton className="hidden h-3 w-12 justify-self-end md:block" />
        </div>
      ))}
    </div>
  );
}
