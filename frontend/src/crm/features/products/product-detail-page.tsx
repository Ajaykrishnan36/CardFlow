import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Archive, ArchiveRestore, Boxes, Building2, Ellipsis, GitBranch, KeyRound, LayoutGrid, Rocket, TriangleAlert, UsersRound } from 'lucide-react';
import { productsApi, workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { ProductConfig, ProductDetail } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '@crm/components/ui/menu';
import { EmptyState, ErrorState } from '@crm/components/states';
import { ConfirmDialog, DetailItem, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { ProductIcon } from './product-icon';
import { canPublish, LOGIN_METHODS, nextVersion, productStatusTone, SETUP_STEPS, type SetupStep } from './product-utils';
import { draftFromProduct, useProductDraft } from './use-product-draft';
import { ProductSetupWizard } from './product-setup-wizard';

type Tab = 'overview' | 'setup' | 'versions' | 'workspaces';
const TABS: Tab[] = ['overview', 'setup', 'versions', 'workspaces'];

export function ProductDetailPage() {
  const { t } = useTranslation();
  const { id = '' } = useParams();
  const q = useQuery({ queryKey: ['product', id], queryFn: () => productsApi.get(id), enabled: Boolean(id) });
  useDocumentTitle(q.data?.name ?? t('products.list.title'));

  if (q.isError) {
    const notFound = isApiError(q.error) && q.error.status === 404;
    return (
      <PageContainer>
        <PageHeader title={t('products.list.title')} crumbs={[{ label: t('products.list.title'), to: '/crm/owner/products' }]} />
        <Card>
          {notFound ? (
            <EmptyState
              icon={Boxes}
              title={t('products.detail.notFoundTitle')}
              body={t('products.detail.notFoundBody')}
              action={
                <Button asChild variant="outline">
                  <Link to="/crm/owner/products">{t('products.detail.backToList')}</Link>
                </Button>
              }
            />
          ) : (
            <ErrorState
              title={t('products.detail.errorTitle')}
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
  return <ProductDetailView product={q.data} />;
}

function ProductDetailView({ product }: { product: ProductDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const tabParam = params.get('tab') as Tab | null;
  const tab: Tab = tabParam && TABS.includes(tabParam) ? tabParam : 'overview';
  const stepParam = params.get('step') as SetupStep | null;
  const step: SetupStep = stepParam && SETUP_STEPS.includes(stepParam) ? stepParam : 'general';

  const { draft, setDraft, updateConfig, dirty } = useProductDraft(product);
  // Opened from a project's Setup tab: breadcrumbs lead back to that project.
  const projectParam = params.get('project');
  const fromProjectQ = useQuery({ queryKey: ['workspace', projectParam], queryFn: () => workspacesApi.get(projectParam!), enabled: Boolean(projectParam) });
  const fromProject = fromProjectQ.data ? { id: fromProjectQ.data.id, name: fromProjectQ.data.name } : null;
  // Created from a workspace (workspace first): add it there as soon as it's published.
  const assignTo = params.get('assignTo');
  const assignWs = useQuery({ queryKey: ['workspace', assignTo], queryFn: () => workspacesApi.get(assignTo!), enabled: Boolean(assignTo) });
  const [publishOpen, setPublishOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [publishErrors, setPublishErrors] = useState<Record<string, string> | null>(null);

  const go = (next: { tab?: Tab; step?: SetupStep }) => {
    setParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        const nextTab = next.tab ?? tab;
        sp.set('tab', nextTab);
        if (nextTab === 'setup') sp.set('step', next.step ?? step);
        else sp.delete('step');
        return sp;
      },
      { replace: true }
    );
  };

  const applyServer = (p: ProductDetail) => {
    qc.setQueryData(['product', p.id], p);
    setDraft(draftFromProduct(p));
    void qc.invalidateQueries({ queryKey: ['products'] });
  };

  const saveBody = () => ({ name: draft.name.trim(), description: draft.description.trim(), icon: draft.icon, draftConfig: draft.config });

  const save = useMutation({
    mutationFn: () => productsApi.update(product.id, saveBody()),
    onSuccess: (p) => {
      applyServer(p);
      toast.success(t('products.setup.draftSaved'));
    }
  });

  // After publishing, go back to the product this setup belongs to (its Product setup tab).
  const backToProduct = (p: ProductDetail) => {
    const backTo = assignTo ?? projectParam ?? (p.assignedWorkspaces.length === 1 ? p.assignedWorkspaces[0]!.id : null);
    if (backTo) navigate(`/crm/owner/workspaces/${encodeURIComponent(backTo)}?tab=products`);
  };

  const publish = useMutation({
    mutationFn: async () => {
      if (dirty) applyServer(await productsApi.update(product.id, saveBody()));
      return productsApi.publish(product.id);
    },
    onSuccess: (p) => {
      applyServer(p);
      setPublishErrors(null);
      setPublishOpen(false);
      void qc.invalidateQueries({ queryKey: ['workspace'] });
      toast.success(t('products.publish.done', { version: p.currentVersion ?? nextVersion(product) }));
      const goBack = () => backToProduct(p);
      if (assignTo && !p.assignedWorkspaces.some((w) => w.id === assignTo)) {
        void workspacesApi
          .assignProduct(assignTo, p.id)
          .then(() => {
            toast.success(t('products.assignTo.done', { workspace: assignWs.data?.name ?? '' }));
            void qc.invalidateQueries({ queryKey: ['workspace'] });
            void qc.invalidateQueries({ queryKey: ['product', p.id] });
            goBack();
          })
          .catch((e: unknown) => toast.error(isApiError(e) ? e.message : t('common.genericError')));
        return;
      }
      goBack();
    },
    onError: (e) => {
      setPublishOpen(false);
      if (isApiError(e) && e.code === 'nothing_to_publish') {
        // Only the name or description changed: that's saved, there's no new version to publish.
        toast.success(t('products.setup.draftSaved'));
        backToProduct(product);
        return;
      }
      if (isApiError(e) && e.status === 422 && Object.keys(e.fieldErrors).length > 0) {
        setPublishErrors(e.fieldErrors);
        go({ tab: 'setup', step: 'review' });
        toast.error(t('products.publish.fixFirst'));
        return;
      }
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const archive = useMutation({
    mutationFn: () => productsApi.archive(product.id),
    onSuccess: (p) => {
      applyServer(p);
      setArchiveOpen(false);
      toast.success(t('products.detail.archived'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const restore = useMutation({
    mutationFn: () => productsApi.restore(product.id),
    onSuccess: (p) => {
      applyServer(p);
      toast.success(t('products.detail.restored'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const version = nextVersion(product);
  const publishable = canPublish(product, dirty);
  const archived = product.status === 'archived';

  return (
    <PageContainer>
      <PageHeader
        crumbs={
          fromProject
            ? [
                { label: t('workspaces.list.title'), to: '/crm/owner/workspaces' },
                { label: fromProject.name, to: `/crm/owner/workspaces/${fromProject.id}?tab=products` },
                { label: t('products.setupCrumb') }
              ]
            : [{ label: t('products.list.title'), to: '/crm/owner/products' }, { label: product.name }]
        }
        icon={<ProductIcon icon={draft.icon} accent={draft.config.accentColor} size="lg" />}
        title={product.name}
        description={<span className="font-mono text-xs">{product.key}</span>}
        badges={
          <>
            <Badge tone={productStatusTone[product.status]}>{t(`status.${product.status}`)}</Badge>
            {product.currentVersion ? <Badge tone="primary">v{product.currentVersion}</Badge> : null}
            {(product.hasUnpublishedChanges || dirty) && !archived ? (
              <span className="inline-flex items-center gap-1 text-[11px] font-medium text-warning">
                <span className="size-1.5 rounded-full bg-warning" aria-hidden />
                {dirty ? t('products.setup.unsaved') : t('products.list.unpublished')}
              </span>
            ) : null}
          </>
        }
        actions={
          <>
            <Button onClick={() => setPublishOpen(true)} disabled={!publishable} loading={publish.isPending}>
              <Rocket /> {t('products.publish.button', { version })}
            </Button>
            <Menu>
              <MenuTrigger asChild>
                <Button variant="outline" size="icon" aria-label={t('products.detail.more')}>
                  <Ellipsis />
                </Button>
              </MenuTrigger>
              <MenuContent align="end" className="min-w-[12rem]">
                {archived ? (
                  <MenuItem onSelect={() => restore.mutate()} disabled={restore.isPending}>
                    <ArchiveRestore /> {t('products.detail.restore')}
                  </MenuItem>
                ) : (
                  <MenuItem danger onSelect={() => setArchiveOpen(true)}>
                    <Archive /> {t('products.detail.archive')}
                  </MenuItem>
                )}
              </MenuContent>
            </Menu>
          </>
        }
      />

      {archived ? (
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-warning/30 bg-warning-soft px-4 py-3 text-[13px]">
          <span className="flex items-center gap-2 text-foreground">
            <TriangleAlert className="size-4 text-warning" aria-hidden /> {t('products.detail.archivedBanner')}
          </span>
          <Button size="sm" variant="outline" onClick={() => restore.mutate()} loading={restore.isPending}>
            <ArchiveRestore /> {t('products.detail.restore')}
          </Button>
        </div>
      ) : null}

      {assignTo ? (
        <Alert tone="info" className="mb-4">
          {t('products.assignTo.banner', { workspace: assignWs.data?.name ?? t('products.assignTo.thisWorkspace') })}{' '}
          <Link to={`/crm/owner/workspaces/${encodeURIComponent(assignTo)}?tab=products`} className="font-medium text-primary hover:underline">
            {t('products.assignTo.back')}
          </Link>
        </Alert>
      ) : null}
      <Tabs<Tab>
        className="mb-6"
        value={tab}
        onChange={(v) => go({ tab: v })}
        items={[
          { value: 'overview', label: t('products.tabs.overview') },
          {
            value: 'setup',
            label: (
              <span className="inline-flex items-center gap-1.5">
                {t('products.tabs.setup')}
                {dirty ? <span className="size-1.5 rounded-full bg-warning" aria-label={t('products.setup.unsaved')} /> : null}
              </span>
            )
          },
          { value: 'versions', label: <TabLabel label={t('products.tabs.versions')} count={product.versions.length} /> },
          { value: 'workspaces', label: <TabLabel label={t('products.tabs.workspaces')} count={product.assignedWorkspaces.length} /> }
        ]}
      />

      {tab === 'overview' ? <OverviewTab product={product} onEdit={() => go({ tab: 'setup', step: 'general' })} /> : null}
      {tab === 'setup' ? (
        <ProductSetupWizard
          product={product}
          draft={draft}
          setDraft={setDraft}
          updateConfig={updateConfig}
          dirty={dirty}
          step={step}
          onStepChange={(s) => go({ tab: 'setup', step: s })}
          onSave={async () => {
            try {
              await save.mutateAsync();
              return { ok: true as const };
            } catch (error) {
              return { ok: false as const, error };
            }
          }}
          saving={save.isPending}
          publishErrors={publishErrors}
          publishable={publishable}
          publishing={publish.isPending}
          onPublish={() => setPublishOpen(true)}
        />
      ) : null}
      {tab === 'versions' ? <VersionsTab product={product} /> : null}
      {tab === 'workspaces' ? <WorkspacesTab product={product} /> : null}

      <ConfirmDialog
        open={publishOpen}
        onOpenChange={setPublishOpen}
        title={t('products.publish.confirmTitle', { name: product.name, version })}
        body={
          <>
            <span className="block">
              {product.currentVersion ? t('products.publish.impactUpgrade') : t('products.publish.impactFirst', { name: product.name })}
            </span>
            {product.assignedWorkspaces.length > 0 ? (
              <span className="mt-2 block font-medium text-foreground">
                {t('products.publish.impactCount', { count: product.assignedWorkspaces.length })}
              </span>
            ) : null}
            {dirty ? <span className="mt-2 block">{t('products.publish.savesFirst')}</span> : null}
          </>
        }
        confirmLabel={t('products.publish.button', { version })}
        cancelLabel={t('common.cancel')}
        loading={publish.isPending}
        onConfirm={() => publish.mutate()}
      />
      <ConfirmDialog
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
        tone="danger"
        title={t('products.detail.archiveTitle', { name: product.name })}
        body={t('products.detail.archiveBody')}
        confirmLabel={t('products.detail.archive')}
        cancelLabel={t('common.cancel')}
        loading={archive.isPending}
        onConfirm={() => archive.mutate()}
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

// ---------------------------------------------------------------- overview
function StatCard({ icon: Icon, label, value }: { icon: typeof Boxes; label: string; value: ReactNode }) {
  return (
    <Card className="p-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-[13px] font-medium text-muted-foreground">{label}</p>
        <span className="grid size-7 place-items-center rounded-md bg-primary-soft text-primary">
          <Icon className="size-3.5" aria-hidden />
        </span>
      </div>
      <p className="mt-2 text-2xl font-semibold leading-none tracking-tight tabular-nums">{value}</p>
    </Card>
  );
}

function enabledLoginMethods(c: ProductConfig): string[] {
  return LOGIN_METHODS.filter((k) => c.loginMethods[k]);
}

function OverviewTab({ product, onEdit }: { product: ProductDetail; onEdit: () => void }) {
  const { t } = useTranslation();
  const config = product.publishedConfig ?? product.draftConfig;
  const live = Boolean(product.publishedConfig);
  const moduleLabel = (key: string) => product.moduleCatalog.find((m) => m.key === key)?.label ?? key;
  const logins = enabledLoginMethods(config);

  return (
    <div className="space-y-6">
      <section className="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-label={t('products.overview.summary')}>
        <StatCard icon={LayoutGrid} label={t('products.overview.modules')} value={config.modules.length} />
        <StatCard icon={UsersRound} label={t('products.overview.userTypes')} value={config.userTypes.length} />
        <StatCard icon={KeyRound} label={t('products.overview.loginMethods')} value={logins.length} />
        <StatCard icon={Building2} label={t('products.overview.workspaces')} value={product.workspaces} />
      </section>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <Card className="min-w-0">
          <CardHeader
            title={live ? t('products.overview.liveConfig', { version: product.currentVersion }) : t('products.overview.draftConfig')}
            description={product.hasUnpublishedChanges && live ? t('products.overview.pendingNote') : undefined}
            actions={
              <Button variant="outline" size="sm" onClick={onEdit}>
                {t('products.overview.edit')}
              </Button>
            }
          />
          <div className="space-y-5 px-5 py-5">
            <ChipSection title={t('products.overview.modules')} empty={t('products.overview.noneYet')} items={config.modules.map(moduleLabel)} />
            <ChipSection title={t('products.overview.userTypes')} empty={t('products.overview.noneYet')} items={config.userTypes.map((u) => u.label)} />
            <ChipSection
              title={t('products.overview.roles')}
              empty={t('products.overview.noneYet')}
              items={config.roles.filter((r) => r.enabled).map((r) => r.label)}
            />
            <ChipSection title={t('products.overview.loginMethods')} empty={t('products.overview.noneYet')} items={logins.map((k) => t(`products.setup.login.${k}`))} />
          </div>
        </Card>

        <Card className="min-w-0">
          <CardHeader title={t('products.overview.details')} />
          <dl className="grid gap-4 px-5 py-5">
            <DetailItem label={t('products.create.descriptionLabel')}>{product.description || null}</DetailItem>
            <DetailItem label={t('products.create.key')}>
              <span className="font-mono text-[13px]">{product.key}</span>
            </DetailItem>
            <DetailItem label={t('products.overview.currentVersion')}>{product.currentVersion ? `v${product.currentVersion}` : null}</DetailItem>
            <div className="grid grid-cols-2 gap-4">
              <DetailItem label={t('products.overview.created')}>{relativeTime(product.createdAt)}</DetailItem>
              <DetailItem label={t('products.overview.updated')}>{relativeTime(product.updatedAt)}</DetailItem>
            </div>
          </dl>
        </Card>
      </div>
    </div>
  );
}

function ChipSection({ title, items, empty }: { title: string; items: string[]; empty: string }) {
  return (
    <div>
      <p className="mb-2 text-xs font-medium text-muted-foreground">
        {title} <span className="tabular-nums">· {items.length}</span>
      </p>
      {items.length === 0 ? (
        <p className="text-[13px] text-muted-foreground/70">{empty}</p>
      ) : (
        <div className="flex flex-wrap gap-1.5">
          {items.map((label, i) => (
            <span key={`${label}-${i}`} className="rounded-md border bg-muted/50 px-2 py-0.5 text-xs font-medium text-foreground">
              {label}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- versions
function VersionsTab({ product }: { product: ProductDetail }) {
  const { t } = useTranslation();
  const versions = [...product.versions].sort((a, b) => b.version - a.version);
  return (
    <Card>
      <CardHeader title={t('products.versions.title')} description={t('products.versions.description')} />
      {versions.length === 0 ? (
        <EmptyState icon={GitBranch} title={t('products.versions.emptyTitle')} body={t('products.versions.emptyBody')} />
      ) : (
        <ol className="px-5 py-4">
          {versions.map((v, i) => {
            const current = v.version === product.currentVersion;
            return (
              <li key={v.version} className="relative flex gap-4 pb-6 last:pb-0">
                {i < versions.length - 1 ? <span className="absolute left-[15px] top-8 h-[calc(100%-2rem)] w-px bg-border" aria-hidden /> : null}
                <span
                  className={cn(
                    'relative grid size-8 shrink-0 place-items-center rounded-full border font-mono text-[11px] font-semibold',
                    current ? 'border-primary bg-primary text-primary-foreground' : 'bg-card text-muted-foreground'
                  )}
                >
                  v{v.version}
                </span>
                <div className="min-w-0 flex-1 pt-1">
                  <p className="flex flex-wrap items-center gap-2 text-[13px] font-medium text-foreground">
                    {t('products.versions.version', { version: v.version })}
                    {current ? <Badge tone="primary">{t('products.versions.current')}</Badge> : null}
                  </p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {v.publishedBy
                      ? t('products.versions.publishedBy', { time: relativeTime(v.publishedAt), who: v.publishedBy })
                      : t('products.versions.published', { time: relativeTime(v.publishedAt) })}
                    {' · '}
                    {t('products.versions.pinned', { count: v.workspaces })}
                  </p>
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- workspaces
function WorkspacesTab({ product }: { product: ProductDetail }) {
  const { t } = useTranslation();
  const rows = product.assignedWorkspaces;
  const latest = product.currentVersion ?? 0;
  const statusTone = (s: string) => (s === 'active' ? 'success' : s === 'suspended' ? 'warning' : s === 'failed' ? 'danger' : 'neutral');

  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader title={t('products.workspaces.title')} description={t('products.workspaces.description')} />
      {rows.length === 0 ? (
        <EmptyState
          icon={Building2}
          title={t('products.workspaces.emptyTitle')}
          body={product.status === 'active' ? t('products.workspaces.emptyBody') : t('products.workspaces.emptyBodyDraft')}
          action={
            product.status === 'active' ? (
              <Button asChild variant="outline">
                <Link to="/crm/owner/workspaces/new">{t('products.workspaces.provision')}</Link>
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          <div className="hidden overflow-x-auto sm:block">
            <table className="w-full text-left text-[13px]">
              <thead>
                <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">{t('products.workspaces.colWorkspace')}</th>
                  <th className="px-3 py-2.5 font-medium">{t('products.workspaces.colStatus')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('products.workspaces.colVersion')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((w) => (
                  <tr key={w.id} className="border-b last:border-0 hover:bg-muted/50">
                    <td className="px-5 py-3">
                      <Link to={`/crm/owner/workspaces/${w.id}`} className="font-medium text-foreground hover:underline">
                        {w.name}
                      </Link>
                      <p className="font-mono text-xs text-muted-foreground">{w.code}</p>
                    </td>
                    <td className="px-3 py-3">
                      <span className="inline-flex flex-wrap gap-1.5">
                        <Badge tone={statusTone(w.status)}>{t(`status.${w.status}`, { defaultValue: w.status })}</Badge>
                        {w.assignmentStatus === 'suspended' ? <Badge tone="warning">{t('products.workspaces.accessSuspended')}</Badge> : null}
                      </span>
                    </td>
                    <td className="px-5 py-3">
                      <VersionCell version={w.configVersion} latest={latest} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <ul className="divide-y sm:hidden">
            {rows.map((w) => (
              <li key={w.id}>
                <Link to={`/crm/owner/workspaces/${w.id}`} className="flex items-center justify-between gap-3 px-4 py-3 hover:bg-muted/50">
                  <div className="min-w-0">
                    <p className="truncate font-medium text-foreground">{w.name}</p>
                    <p className="font-mono text-xs text-muted-foreground">{w.code}</p>
                  </div>
                  <div className="flex shrink-0 flex-col items-end gap-1">
                    <Badge tone={statusTone(w.status)}>{t(`status.${w.status}`, { defaultValue: w.status })}</Badge>
                    <VersionCell version={w.configVersion} latest={latest} />
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </Card>
  );
}

function VersionCell({ version, latest }: { version: number; latest: number }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="font-mono text-xs tabular-nums">v{version}</span>
      {version < latest ? <Badge tone="warning">{t('products.workspaces.behind')}</Badge> : null}
    </span>
  );
}

function DetailSkeleton() {
  return (
    <PageContainer>
      <Skeleton className="mb-3 h-3.5 w-32" />
      <div className="mb-6 flex items-center gap-3">
        <Skeleton className="size-12 rounded-xl" />
        <div className="space-y-2">
          <Skeleton className="h-6 w-56" />
          <Skeleton className="h-3 w-24" />
        </div>
      </div>
      <Skeleton className="mb-6 h-9 w-full max-w-md" />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-20 rounded-lg" />
        ))}
      </div>
      <Skeleton className="mt-6 h-64 rounded-lg" />
    </PageContainer>
  );
}
