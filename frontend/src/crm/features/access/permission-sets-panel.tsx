import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Ellipsis, Info, Layers, Pencil, Plus, Trash2 } from 'lucide-react';
import { accessApi } from '@crm/api/endpoints';
import { RoleHierarchy } from './role-hierarchy';
import { isApiError } from '@crm/api/client';
import type { PermissionSet } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '@crm/components/ui/menu';
import { ConfirmDialog } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { PermissionSetDialog } from './permission-set-dialog';
import { summarizeRules, useAccessCatalog, useAccessWorkspaces, useInvalidateAccess, usePermissionSets, useWorkspaceRoles, workspaceLabel } from './use-access';

export interface PermissionSetsPanelProps {
  /** Fixed workspace (workspace detail page): no picker. */
  lockedWorkspaceId?: string;
  /** Controlled picker value (e.g. from the URL); defaults to the platform workspace. */
  selectedWorkspaceId?: string;
  onSelectWorkspace?: (id: string) => void;
  /** Show the workspace's roles above the permission sets. */
  showRoles?: boolean;
}

type DialogState = { mode: 'create' } | { mode: 'edit'; set: PermissionSet } | null;

/** "Roles & permission sets" for one workspace: its roles first, then its permission sets. */
function OwnerRoleHierarchy({ workspaceId, workspaceName }: { workspaceId: string; workspaceName?: string }) {
  const roles = useWorkspaceRoles(workspaceId);
  const invalidate = useInvalidateAccess();
  return (
    <RoleHierarchy
      roles={roles.data}
      loading={roles.isPending}
      error={roles.error}
      onRetry={() => void roles.refetch()}
      canEdit
      context={workspaceName}
      onChanged={invalidate}
      api={{
        create: (b) => accessApi.createRole(workspaceId, b),
        update: (id, b) => accessApi.updateRole(id, b),
        remove: (id) => accessApi.deleteRole(id)
      }}
    />
  );
}

export function PermissionSetsPanel({ lockedWorkspaceId, selectedWorkspaceId, onSelectWorkspace, showRoles = true }: PermissionSetsPanelProps) {
  const { t } = useTranslation();
  const workspaces = useAccessWorkspaces();
  const catalog = useAccessCatalog();
  const invalidate = useInvalidateAccess();
  const [localWs, setLocalWs] = useState<string | undefined>(undefined);
  const [dialog, setDialog] = useState<DialogState>(null);
  const [toDelete, setToDelete] = useState<PermissionSet | null>(null);

  const list = workspaces.data ?? [];
  const picked = selectedWorkspaceId ?? localWs;
  const wsId = lockedWorkspaceId ?? (picked && list.some((w) => w.id === picked) ? picked : list[0]?.id);
  const ws = list.find((w) => w.id === wsId);
  const wsName = ws ? workspaceLabel(ws, t('access.workspace.platformLabel')) : undefined;
  const sets = usePermissionSets(wsId);

  const pick = (id: string) => {
    setLocalWs(id);
    onSelectWorkspace?.(id);
  };

  const remove = useMutation({
    mutationFn: (id: string) => accessApi.deletePermissionSet(id),
    onSuccess: () => {
      invalidate();
      setToDelete(null);
      toast.success(t('access.sets.deleted'));
    },
    onError: (e) => {
      setToDelete(null);
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const picker = !lockedWorkspaceId ? (
    <Select
      className="w-full shrink-0 sm:w-64"
      value={wsId ?? ''}
      onChange={(e) => pick(e.target.value)}
      aria-label={t('access.sets.workspace')}
      disabled={!workspaces.data}
      options={list.map((w) => ({ value: w.id, label: workspaceLabel(w, t('access.workspace.platformLabel')) }))}
      placeholder={workspaces.data ? undefined : t('common.loading')}
    />
  ) : null;

  const newButton = (
    <Button size="sm" onClick={() => setDialog({ mode: 'create' })} disabled={!wsId}>
      <Plus /> {t('access.sets.new')}
    </Button>
  );

  let body;
  if (!lockedWorkspaceId && workspaces.isError) {
    body = (
      <ErrorState
        title={t('access.sets.workspacesError')}
        message={isApiError(workspaces.error) ? workspaces.error.message : undefined}
        onRetry={() => void workspaces.refetch()}
      />
    );
  } else if (sets.isError) {
    body = <ErrorState title={t('access.sets.errorTitle')} message={isApiError(sets.error) ? sets.error.message : undefined} onRetry={() => void sets.refetch()} />;
  } else if (!sets.data || !catalog.data) {
    body = (
      <ul className="divide-y" aria-busy>
        {Array.from({ length: 3 }).map((_, i) => (
          <li key={i} className="flex items-center gap-3 px-5 py-3.5">
            <Skeleton className="size-8 rounded-lg" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-3.5 w-40" />
              <Skeleton className="h-3 w-72 max-w-full" />
            </div>
            <Skeleton className="h-5 w-16" />
          </li>
        ))}
      </ul>
    );
  } else if (sets.data.length === 0) {
    body = <EmptyState icon={Layers} title={t('access.sets.emptyTitle')} body={t('access.sets.emptyBody')} action={newButton} className="py-10" />;
  } else {
    const cat = catalog.data;
    body = (
      <ul className="divide-y">
        {sets.data.map((s) => {
          const summary = summarizeRules(s.rules, cat);
          return (
            <li key={s.id} className="flex items-start gap-3 px-5 py-3 transition-colors hover:bg-muted/40">
              <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-primary-soft text-primary">
                <Layers className="size-4" aria-hidden />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <button
                    type="button"
                    onClick={() => setDialog({ mode: 'edit', set: s })}
                    className="truncate text-left text-[13px] font-medium text-foreground hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {s.name}
                  </button>
                  <Badge tone={s.assignedCount > 0 ? 'primary' : 'neutral'}>
                    {s.assignedCount > 0 ? t('access.sets.assigned', { count: s.assignedCount }) : t('access.sets.unassigned')}
                  </Badge>
                </div>
                {s.description ? <p className="mt-0.5 text-xs text-muted-foreground">{s.description}</p> : null}
                <p className={cn('mt-1 text-xs', summary ? 'text-foreground/80' : 'italic text-muted-foreground')}>{summary || t('access.sets.noGrants')}</p>
              </div>
              <Menu modal={false}>
                <MenuTrigger asChild>
                  <Button variant="ghost" size="icon-sm" aria-label={t('access.sets.actionsFor', { name: s.name })}>
                    <Ellipsis />
                  </Button>
                </MenuTrigger>
                <MenuContent align="end" className="min-w-[10rem]">
                  <MenuItem onSelect={() => setDialog({ mode: 'edit', set: s })}>
                    <Pencil /> {t('access.sets.edit')}
                  </MenuItem>
                  <MenuItem danger onSelect={() => setToDelete(s)}>
                    <Trash2 /> {t('access.sets.delete')}
                  </MenuItem>
                </MenuContent>
              </Menu>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 className="text-[15px] font-semibold text-foreground">{t('access.panel.title')}</h2>
          <p className="mt-1 flex max-w-2xl items-start gap-1.5 text-[13px] text-muted-foreground">
            <Info className="mt-0.5 size-3.5 shrink-0 text-primary" aria-hidden />
            <span>{t('access.panel.explainer')}</span>
          </p>
        </div>
        {picker}
      </div>

      {showRoles && wsId ? <OwnerRoleHierarchy workspaceId={wsId} workspaceName={wsName} /> : null}

      <Card className="min-w-0">
        <CardHeader title={t('access.sets.title')} description={t('access.sets.description')} actions={sets.data && sets.data.length > 0 ? newButton : null} />
        {body}
      </Card>

      {wsId && dialog ? (
        <PermissionSetDialog
          open
          onOpenChange={(o) => !o && setDialog(null)}
          workspaceId={wsId}
          workspaceName={wsName}
          permissionSet={dialog.mode === 'edit' ? dialog.set : null}
        />
      ) : null}

      <ConfirmDialog
        open={toDelete !== null}
        onOpenChange={(o) => !o && !remove.isPending && setToDelete(null)}
        title={t('access.sets.deleteTitle', { name: toDelete?.name ?? '' })}
        body={t('access.sets.deleteBody')}
        confirmLabel={t('access.sets.deleteConfirm')}
        cancelLabel={t('common.cancel')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => toDelete && remove.mutate(toDelete.id)}
      />
    </div>
  );
}
