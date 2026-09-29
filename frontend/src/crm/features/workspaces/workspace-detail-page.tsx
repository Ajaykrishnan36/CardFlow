import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Ban, Boxes, Building2, CirclePlay, ExternalLink, MailPlus, Plug, UsersRound } from 'lucide-react';
import { workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { WorkspaceDetail } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { ConfirmDialog, DetailItem, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { isOpenInvite, WorkspaceStatusBadge, WorkspaceTile } from './workspace-ui';
import { WorkspaceProductsTab } from './workspace-products-tab';
import { WorkspacePeopleTab } from './workspace-people-tab';
import { WorkspaceSettingsTab } from './workspace-settings-tab';
import { ApiSharing } from '@crm/features/access/api-sharing';
import { PermissionSetsPanel } from '@crm/features/access/permission-sets-panel';
import { useIntegrations } from '@crm/features/integrations/use-integrations';

type Tab = 'overview' | 'products' | 'people' | 'permissions' | 'api' | 'settings';
const TABS: Tab[] = ['overview', 'products', 'people', 'permissions', 'api', 'settings'];

export function WorkspaceDetailPage() {
  const { t } = useTranslation();
  const { id = '' } = useParams();
  const q = useQuery({ queryKey: ['workspace', id], queryFn: () => workspacesApi.get(id), enabled: Boolean(id) });
  useDocumentTitle(q.data?.name ?? t('workspaces.list.title'));

  if (q.isError) {
    const notFound = isApiError(q.error) && q.error.status === 404;
    return (
      <PageContainer>
        <PageHeader title={t('workspaces.list.title')} crumbs={[{ label: t('workspaces.list.title'), to: '/crm/owner/workspaces' }]} />
        <Card>
          {notFound ? (
            <EmptyState
              icon={Building2}
              title={t('workspaces.detail.notFoundTitle')}
              body={t('workspaces.detail.notFoundBody')}
              action={
                <Button asChild variant="outline">
                  <Link to="/crm/owner/workspaces">{t('workspaces.detail.backToList')}</Link>
                </Button>
              }
            />
          ) : (
            <ErrorState
              title={t('workspaces.detail.errorTitle')}
              message={isApiError(q.error) ? q.error.message : undefined}
              requestId={isApiError(q.error) ? q.error.requestId : undefined}
              onRetry={() => void q.refetch()}
            />
          )}
        </Card>
      </PageContainer>
    );
  }
  if (!q.data) return <DetailSkeleton />;
  return <WorkspaceDetailView workspace={q.data} />;
}

function WorkspaceDetailView({ workspace }: { workspace: WorkspaceDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const tabParam = params.get('tab') as Tab | null;
  const tab: Tab = tabParam && TABS.includes(tabParam) ? tabParam : 'overview';
  const [confirmOpen, setConfirmOpen] = useState(false);

  const setTab = (v: Tab) =>
    setParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        sp.set('tab', v);
        return sp;
      },
      { replace: true }
    );

  const suspended = workspace.status === 'suspended';
  const canToggle = workspace.status === 'active' || suspended;

  const setStatus = useMutation({
    mutationFn: (status: 'active' | 'suspended') => workspacesApi.update(workspace.id, { status }),
    onSuccess: (w) => {
      qc.setQueryData(['workspace', w.id], w);
      void qc.invalidateQueries({ queryKey: ['workspaces'] });
      setConfirmOpen(false);
      toast.success(w.status === 'suspended' ? t('workspaces.detail.suspended') : t('workspaces.detail.reactivated'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const openInvites = workspace.invitations.filter(isOpenInvite).length;
  const integrations = useIntegrations();
  const connected = (integrations.data ?? []).filter((i) => i.workspace?.id === workspace.id);

  return (
    <PageContainer>
      <PageHeader
        crumbs={[{ label: t('workspaces.list.title'), to: '/crm/owner/workspaces' }, { label: workspace.name }]}
        icon={<WorkspaceTile name={workspace.name} size="lg" />}
        title={workspace.name}
        description={<span className="font-mono text-xs">{workspace.code}</span>}
        badges={
          <>
            <WorkspaceStatusBadge status={workspace.status} />
            {connected.map((i) => (
              <Link
                key={i.key}
                to="/crm/owner/integrations"
                className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Badge tone={i.status === 'error' ? 'danger' : 'primary'} className="hover:underline">
                  <Plug className="size-3" aria-hidden /> {t('integrations.connected', { name: i.name })}
                </Badge>
              </Link>
            ))}
          </>
        }
        actions={
          <>
            <Button asChild variant="outline" className="border-primary/40 text-primary hover:bg-primary-soft">
              <Link to={`/crm/w/${workspace.code}/home`}>
                <ExternalLink /> {t('workspaces.detail.openCrm')}
              </Link>
            </Button>
            {canToggle ? (
              suspended ? (
                <Button onClick={() => setConfirmOpen(true)}>
                  <CirclePlay /> {t('workspaces.detail.reactivate')}
                </Button>
              ) : (
                <Button variant="danger-outline" onClick={() => setConfirmOpen(true)}>
                  <Ban /> {t('workspaces.detail.suspend')}
                </Button>
              )
            ) : null}
          </>
        }
      />

      {suspended ? (
        <div className="mb-4 rounded-lg border border-warning/30 bg-warning-soft px-4 py-3 text-[13px] text-foreground">{t('workspaces.detail.suspendedBanner')}</div>
      ) : null}

      <Tabs<Tab>
        className="mb-6"
        value={tab}
        onChange={setTab}
        items={[
          { value: 'overview', label: t('workspaces.tabs.overview') },
          { value: 'products', label: <TabLabel label={t('workspaces.tabs.products')} count={workspace.productList.length} /> },
          { value: 'people', label: <TabLabel label={t('workspaces.tabs.people')} count={workspace.memberList.length} /> },
          { value: 'permissions', label: t('workspaces.tabs.permissions') },
          { value: 'api', label: t('workspaces.tabs.api') },
          { value: 'settings', label: t('workspaces.tabs.settings') }
        ]}
      />

      {tab === 'overview' ? <OverviewTab workspace={workspace} openInvites={openInvites} onTab={setTab} /> : null}
      {tab === 'products' ? <WorkspaceProductsTab workspace={workspace} /> : null}
      {tab === 'people' ? <WorkspacePeopleTab workspace={workspace} /> : null}
      {tab === 'permissions' ? <PermissionSetsPanel lockedWorkspaceId={workspace.id} /> : null}
      {tab === 'api' ? <ApiSharing code={workspace.code} source={{ kind: 'owner', workspaceId: workspace.id }} /> : null}
      {tab === 'settings' ? <WorkspaceSettingsTab workspace={workspace} /> : null}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        tone={suspended ? 'primary' : 'danger'}
        title={suspended ? t('workspaces.detail.reactivateTitle', { name: workspace.name }) : t('workspaces.detail.suspendTitle', { name: workspace.name })}
        body={suspended ? t('workspaces.detail.reactivateBody') : t('workspaces.detail.suspendBody')}
        confirmLabel={suspended ? t('workspaces.detail.reactivate') : t('workspaces.detail.suspend')}
        cancelLabel={t('common.cancel')}
        loading={setStatus.isPending}
        onConfirm={() => setStatus.mutate(suspended ? 'active' : 'suspended')}
      />
    </PageContainer>
  );
}

function TabLabel({ label, count }: { label: string; count: number }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {label}
      <span className="rounded-full bg-muted px-1.5 text-[11px] tabular-nums text-muted-foreground">{count}</span>
    </span>
  );
}

function MiniKpi({ icon: Icon, label, value, onClick }: { icon: typeof Boxes; label: string; value: number; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded-lg border bg-card p-4 text-left text-card-foreground shadow-card transition-shadow hover:shadow-pop focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <span className="flex items-center justify-between gap-2">
        <span className="text-[13px] font-medium text-muted-foreground">{label}</span>
        <span className="grid size-7 place-items-center rounded-md bg-primary-soft text-primary">
          <Icon className="size-3.5" aria-hidden />
        </span>
      </span>
      <span className="mt-2 block text-2xl font-semibold leading-none tracking-tight tabular-nums">{value}</span>
    </button>
  );
}

function OverviewTab({ workspace, openInvites, onTab }: { workspace: WorkspaceDetail; openInvites: number; onTab: (t: Tab) => void }) {
  const { t } = useTranslation();
  const row = (label: string, value: ReactNode) => <DetailItem label={label}>{value}</DetailItem>;
  return (
    <div className="space-y-6">
      <section className="grid grid-cols-1 gap-3 min-[420px]:grid-cols-3" aria-label={t('workspaces.overview.summary')}>
        <MiniKpi icon={Boxes} label={t('workspaces.overview.products')} value={workspace.productList.length} onClick={() => onTab('products')} />
        <MiniKpi icon={UsersRound} label={t('workspaces.overview.members')} value={workspace.memberList.length} onClick={() => onTab('people')} />
        <MiniKpi icon={MailPlus} label={t('workspaces.overview.pendingInvites')} value={openInvites} onClick={() => onTab('people')} />
      </section>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="min-w-0">
          <CardHeader title={t('workspaces.overview.details')} />
          <dl className="grid gap-4 px-5 py-5 sm:grid-cols-2">
            {row(t('workspaces.provision.code'), <span className="font-mono text-[13px]">{workspace.code}</span>)}
            {row(t('workspaces.fields.timezone'), workspace.timezone.replace(/_/g, ' '))}
            {row(t('workspaces.fields.locale'), t(`workspaces.locales.${workspace.locale}`, { defaultValue: workspace.locale }))}
            {row(t('workspaces.fields.currency'), workspace.currency)}
            {row(t('workspaces.overview.created'), `${relativeTime(workspace.createdAt)} · ${new Date(workspace.createdAt).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' })}`)}
          </dl>
        </Card>

        <Card className="min-w-0">
          <CardHeader title={t('workspaces.overview.account')} description={t('workspaces.overview.accountBody')} />
          <div className="px-5 py-5">
            {workspace.account ? (
              <Link
                to={`/crm/owner/accounts/${workspace.account.id}`}
                className="flex items-center gap-3 rounded-lg border px-3 py-2.5 transition-colors hover:bg-muted/60"
              >
                <WorkspaceTile name={workspace.account.name} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-foreground">{workspace.account.name}</span>
                  <span className="block font-mono text-xs text-muted-foreground">{workspace.account.code}</span>
                </span>
                <ExternalLink className="size-4 text-muted-foreground" aria-hidden />
              </Link>
            ) : (
              <p className="text-[13px] text-muted-foreground">{t('workspaces.overview.noAccount')}</p>
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}

function DetailSkeleton() {
  return (
    <PageContainer>
      <Skeleton className="mb-3 h-3.5 w-40" />
      <div className="mb-6 flex items-center gap-3">
        <Skeleton className="size-12 rounded-xl" />
        <div className="space-y-2">
          <Skeleton className="h-6 w-56" />
          <Skeleton className="h-3 w-24" />
        </div>
      </div>
      <Skeleton className="mb-6 h-9 w-full max-w-md" />
      <div className="grid grid-cols-3 gap-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-20 rounded-lg" />
        ))}
      </div>
      <Skeleton className="mt-6 h-56 rounded-lg" />
    </PageContainer>
  );
}
