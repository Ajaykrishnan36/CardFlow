import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowUpRight, Boxes, Plus } from 'lucide-react';
import { productsApi, workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { WorkspaceDetail, WorkspaceProduct } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Switch } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { EmptyState, ErrorState } from '@crm/components/states';
import { ConfirmDialog } from '@crm/components/page';
import { relativeTime } from '@crm/lib/utils';
import { ProductIcon } from '@crm/features/products/product-icon';

type Pending = { kind: 'upgrade'; product: WorkspaceProduct } | { kind: 'suspend'; product: WorkspaceProduct } | null;

export function WorkspaceProductsTab({ workspace }: { workspace: WorkspaceDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [assignOpen, setAssignOpen] = useState(false);
  const [pending, setPending] = useState<Pending>(null);

  const apply = (w: WorkspaceDetail) => {
    qc.setQueryData(['workspace', w.id], w);
    void qc.invalidateQueries({ queryKey: ['workspaces'] });
    void qc.invalidateQueries({ queryKey: ['product'] });
    void qc.invalidateQueries({ queryKey: ['products'] });
  };

  const update = useMutation({
    mutationFn: (v: { productId: string; body: { status?: 'active' | 'suspended'; configVersion?: number } }) =>
      workspacesApi.updateProduct(workspace.id, v.productId, v.body),
    onSuccess: (w, v) => {
      apply(w);
      setPending(null);
      if (v.body.configVersion) toast.success(t('workspaces.products.upgraded', { version: v.body.configVersion }));
      else toast.success(v.body.status === 'suspended' ? t('workspaces.products.suspendedToast') : t('workspaces.products.resumedToast'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const rows = workspace.productList;
  const busy = (productId: string) => update.isPending && update.variables?.productId === productId;

  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader
        title={t('workspaces.products.title')}
        description={t('workspaces.products.description')}
        actions={
          <Button size="sm" onClick={() => setAssignOpen(true)}>
            <Plus /> {t('workspaces.products.assign')}
          </Button>
        }
      />
      {rows.length === 0 ? (
        <EmptyState
          icon={Boxes}
          title={t('workspaces.products.emptyTitle')}
          body={t('workspaces.products.emptyBody')}
          action={
            <Button onClick={() => setAssignOpen(true)}>
              <Plus /> {t('workspaces.products.assign')}
            </Button>
          }
        />
      ) : (
        <ul className="divide-y">
          {rows.map((p) => {
            const behind = p.latestVersion !== undefined && p.latestVersion > p.configVersion;
            return (
              <li key={p.productId} className="flex flex-col gap-3 px-5 py-3.5 sm:flex-row sm:items-center">
                <div className="flex min-w-0 flex-1 items-center gap-3">
                  <ProductIcon size="sm" />
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <Link to={`/crm/owner/products/${p.productId}`} className="truncate text-[13px] font-medium text-foreground hover:underline">
                        {p.name}
                      </Link>
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">v{p.configVersion}</span>
                      {behind ? <Badge tone="warning">{t('workspaces.products.behind', { version: p.latestVersion })}</Badge> : null}
                      {p.productStatus === 'archived' ? <Badge>{t('workspaces.products.productArchived')}</Badge> : null}
                    </div>
                    <p className="truncate font-mono text-xs text-muted-foreground">
                      {p.key} <span className="font-sans">· {t('workspaces.products.assignedAt', { time: relativeTime(p.assignedAt) })}</span>
                    </p>
                  </div>
                </div>
                <div className="flex items-center justify-between gap-3 sm:justify-end">
                  {behind ? (
                    <Button variant="outline" size="sm" onClick={() => setPending({ kind: 'upgrade', product: p })} disabled={busy(p.productId)}>
                      <ArrowUpRight /> {t('workspaces.products.upgrade', { version: p.latestVersion })}
                    </Button>
                  ) : null}
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    {p.status === 'active' ? t('workspaces.products.statusActive') : t('workspaces.products.statusSuspended')}
                    <Switch
                      checked={p.status === 'active'}
                      disabled={busy(p.productId)}
                      aria-label={t('workspaces.products.toggle', { name: p.name })}
                      onCheckedChange={(on) => {
                        if (on) update.mutate({ productId: p.productId, body: { status: 'active' } });
                        else setPending({ kind: 'suspend', product: p });
                      }}
                    />
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <ConfirmDialog
        open={pending?.kind === 'upgrade'}
        onOpenChange={(o) => !o && setPending(null)}
        title={pending ? t('workspaces.products.upgradeTitle', { name: pending.product.name, version: pending.product.latestVersion }) : ''}
        body={pending ? t('workspaces.products.upgradeBody', { from: pending.product.configVersion, to: pending.product.latestVersion, workspace: workspace.name }) : undefined}
        confirmLabel={pending ? t('workspaces.products.upgrade', { version: pending.product.latestVersion }) : ''}
        cancelLabel={t('common.cancel')}
        loading={update.isPending}
        onConfirm={() => pending && update.mutate({ productId: pending.product.productId, body: { configVersion: pending.product.latestVersion } })}
      />
      <ConfirmDialog
        open={pending?.kind === 'suspend'}
        onOpenChange={(o) => !o && setPending(null)}
        tone="danger"
        title={pending ? t('workspaces.products.suspendTitle', { name: pending.product.name }) : ''}
        body={t('workspaces.products.suspendBody', { workspace: workspace.name })}
        confirmLabel={t('workspaces.products.suspend')}
        cancelLabel={t('common.cancel')}
        loading={update.isPending}
        onConfirm={() => pending && update.mutate({ productId: pending.product.productId, body: { status: 'suspended' } })}
      />
      <AssignProductDialog open={assignOpen} onOpenChange={setAssignOpen} workspace={workspace} onAssigned={apply} />
    </Card>
  );
}

function AssignProductDialog({
  open,
  onOpenChange,
  workspace,
  onAssigned
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  workspace: WorkspaceDetail;
  onAssigned: (w: WorkspaceDetail) => void;
}) {
  const { t } = useTranslation();
  const products = useQuery({
    queryKey: ['products', { q: '', status: 'active' }],
    queryFn: () => productsApi.list({ status: 'active' }),
    enabled: open
  });
  const assigned = new Set(workspace.productList.map((p) => p.productId));
  const available = (products.data?.data ?? []).filter((p) => !assigned.has(p.id));

  const assign = useMutation({
    mutationFn: (productId: string) => workspacesApi.assignProduct(workspace.id, productId),
    onSuccess: (w) => {
      onAssigned(w);
      onOpenChange(false);
      toast.success(t('workspaces.products.assigned'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="pr-8 text-base font-semibold">{t('workspaces.products.assignTitle')}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('workspaces.products.assignBody', { workspace: workspace.name })}</DialogDescription>
        </div>
        <div className="crm-scroll max-h-[55vh] overflow-y-auto">
          {products.isError ? (
            <ErrorState title={t('workspaces.provision.productsError')} onRetry={() => void products.refetch()} />
          ) : products.isPending ? (
            <div className="space-y-2 p-5">
              <Skeleton className="h-12 rounded-lg" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          ) : available.length === 0 ? (
            <EmptyState
              icon={Boxes}
              className="py-8"
              title={t('workspaces.products.noneAvailable')}
              body={t('workspaces.products.noneAvailableBody')}
              action={
                <Button asChild variant="outline" size="sm">
                  <Link to="/crm/owner/products">{t('workspaces.provision.goToProducts')}</Link>
                </Button>
              }
            />
          ) : (
            <ul className="divide-y">
              {available.map((p) => (
                <li key={p.id} className="flex items-center gap-3 px-5 py-3">
                  <ProductIcon icon={p.icon} size="sm" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-medium text-foreground">{p.name}</p>
                    <p className="truncate font-mono text-xs text-muted-foreground">
                      {p.key}
                      {p.currentVersion ? ` · v${p.currentVersion}` : ''}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    loading={assign.isPending && assign.variables === p.id}
                    disabled={assign.isPending}
                    onClick={() => assign.mutate(p.id)}
                  >
                    {t('workspaces.products.assignOne')}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
