import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { BarChart3, ChevronRight, Plus } from 'lucide-react';
import { reportsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';

export const reportKeys = {
  all: (code: string) => ['workspace', code, 'reports'] as const,
  list: (code: string) => ['workspace', code, 'reports', 'list'] as const,
  one: (code: string, id: string) => ['workspace', code, 'reports', 'one', id] as const,
  objects: (code: string) => ['workspace', code, 'reports', 'objects'] as const,
  dashboards: (code: string) => ['workspace', code, 'reports', 'dashboards'] as const,
  dashboard: (code: string, id: string) => ['workspace', code, 'reports', 'dashboard', id] as const
};

const chartLabel: Record<string, string> = { bar: 'Bar', line: 'Line', donut: 'Donut', number: 'Number', table: 'Table' };

/** /crm/w/:ws/reports — saved reports of this project (each runs as whoever opens it). */
export function ReportsPage() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  useDocumentTitle(t('reports.title'));
  const q = useQuery({ queryKey: reportKeys.list(code), queryFn: () => reportsApi(code).list() });
  const base = workspaceBase(code);
  return (
    <PageContainer>
      <PageHeader
        title={t('reports.title')}
        description={t('reports.subtitle')}
        icon={
          <span className="grid size-10 place-items-center rounded-lg bg-primary-soft text-primary">
            <BarChart3 className="size-5" aria-hidden />
          </span>
        }
        actions={
          <Button asChild>
            <Link to={`${base}/reports/new`}>
              <Plus /> {t('reports.new')}
            </Link>
          </Button>
        }
      />
      <Card className="overflow-hidden">
        {q.isError ? (
          <ErrorState title={t('reports.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />
        ) : !q.data ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-12" />
            ))}
          </div>
        ) : q.data.length === 0 ? (
          <EmptyState
            icon={BarChart3}
            title={t('reports.emptyTitle')}
            body={t('reports.emptyBody')}
            action={
              <Button asChild size="sm">
                <Link to={`${base}/reports/new`}>
                  <Plus /> {t('reports.new')}
                </Link>
              </Button>
            }
          />
        ) : (
          <ul className="divide-y">
            {q.data.map((r) => (
              <li key={r.id}>
                <Link to={`${base}/reports/${r.id}`} className="group flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
                  <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
                    <BarChart3 className="size-4" aria-hidden />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-semibold group-hover:text-primary">{r.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {r.objectLabel} · {t('reports.by', { name: r.ownerName || '—' })} · {relativeTime(r.updatedAt)}
                    </p>
                  </div>
                  <Badge tone="neutral">{chartLabel[r.definition.chart] ?? r.definition.chart}</Badge>
                  <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </PageContainer>
  );
}
