import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ChevronRight, Ellipsis, Pencil, Plus, RotateCcw, ShieldCheck, Trash2, Lock } from 'lucide-react';
import { accessApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { WorkspaceRole } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '@crm/components/ui/menu';
import { ConfirmDialog } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { RoleDialog } from './role-dialog';
import { RoleBadge, RolePermissions } from './role-permissions';
import { summarizeRules, useAccessCatalog, useInvalidateAccess, useWorkspaceRoles } from './use-access';

type Confirm = { kind: 'reset' | 'delete'; role: WorkspaceRole } | null;

/** A workspace's roles: expand to see permissions, edit, reset built-in ones, add or delete custom ones. */
export function RolesCard({ workspaceId, workspaceName }: { workspaceId: string | undefined; workspaceName?: string }) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const roles = useWorkspaceRoles(workspaceId);
  const invalidate = useInvalidateAccess();
  const [expanded, setExpanded] = useState<string | null>(null);
  const [editing, setEditing] = useState<WorkspaceRole | 'new' | null>(null);
  const [confirm, setConfirm] = useState<Confirm>(null);

  const act = useMutation({
    mutationFn: ({ kind, role }: NonNullable<Confirm>) => (kind === 'reset' ? accessApi.resetRole(role.id).then(() => undefined) : accessApi.deleteRole(role.id)),
    onSuccess: (_r, { kind, role }) => {
      invalidate();
      setConfirm(null);
      toast.success(kind === 'reset' ? t('access.roles.resetDone', { name: role.name }) : t('access.roles.deleted', { name: role.name }));
    },
    onError: (e) => {
      // 409 role_in_use: "Assigned to N users — move them to another role first."
      setConfirm(null);
      toast.error(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const list = roles.data ?? [];

  let body;
  if (roles.isError) {
    body = <ErrorState title={t('access.roles.errorTitle')} message={isApiError(roles.error) ? roles.error.message : undefined} onRetry={() => void roles.refetch()} />;
  } else if (!roles.data || !catalog.data) {
    body = (
      <ul className="divide-y" aria-busy>
        {Array.from({ length: 4 }).map((_, i) => (
          <li key={i} className="flex items-center gap-3 px-5 py-3.5">
            <Skeleton className="size-8 rounded-lg" />
            <div className="flex-1 space-y-1.5">
              <Skeleton className="h-3.5 w-36" />
              <Skeleton className="h-3 w-72 max-w-full" />
            </div>
            <Skeleton className="h-5 w-14" />
          </li>
        ))}
      </ul>
    );
  } else if (list.length === 0) {
    body = <EmptyState icon={ShieldCheck} title={t('access.roles.emptyTitle')} className="py-10" />;
  } else {
    const cat = catalog.data;
    body = (
      <ul className="divide-y">
        {list.map((role) => {
          const open = expanded === role.id;
          const panelId = `role-panel-${role.id}`;
          const summary = summarizeRules(role.rules, cat);
          return (
            <li key={role.id}>
              <div className="flex items-start gap-2 px-3 py-3 transition-colors hover:bg-muted/40 sm:px-5">
                <button
                  type="button"
                  aria-expanded={open}
                  aria-controls={panelId}
                  onClick={() => setExpanded(open ? null : role.id)}
                  className="flex min-w-0 flex-1 items-start gap-3 rounded-md text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <ChevronRight className={cn('mt-2 size-4 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden />
                  <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                    <ShieldCheck className="size-4" aria-hidden />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                      <span className="text-[13px] font-medium text-foreground">{role.name}</span>
                      <RoleBadge role={role} showBuiltIn />
                      <Badge tone={role.assignedCount > 0 ? 'primary' : 'neutral'}>{t('access.roles.users', { count: role.assignedCount })}</Badge>
                    </span>
                    {role.description || role.isSystem ? (
                      <span className="mt-0.5 block text-xs text-muted-foreground">
                        {role.isSystem && !role.customized ? t(`access.roles.${role.key}.hint`, { defaultValue: role.description ?? '' }) : role.description}
                      </span>
                    ) : null}
                    <span className={cn('mt-1 block text-xs', summary ? 'text-foreground/80' : 'italic text-muted-foreground')}>{summary || t('access.roles.noGrants')}</span>
                  </span>
                </button>
                <Menu modal={false}>
                  <MenuTrigger asChild>
                    <Button variant="ghost" size="icon-sm" aria-label={t('access.roles.actionsFor', { name: role.name })}>
                      <Ellipsis />
                    </Button>
                  </MenuTrigger>
                  <MenuContent align="end" className="min-w-[12rem]">
                    {role.key === 'SUPER_ADMIN' ? (
                      <MenuItem disabled>
                        <Lock /> {t('access.roles.superAdminLocked')}
                      </MenuItem>
                    ) : (
                      <MenuItem onSelect={() => setEditing(role)}>
                        <Pencil /> {role.isSystem ? t('access.roles.editPermissions') : t('access.roles.edit')}
                      </MenuItem>
                    )}
                    {role.isSystem ? (
                      <MenuItem disabled={!role.customized || role.key === 'SUPER_ADMIN'} onSelect={() => setConfirm({ kind: 'reset', role })}>
                        <RotateCcw /> {t('access.roles.reset')}
                      </MenuItem>
                    ) : (
                      <>
                        <MenuSeparator />
                        <MenuItem danger onSelect={() => setConfirm({ kind: 'delete', role })}>
                          <Trash2 /> {t('access.roles.delete')}
                        </MenuItem>
                      </>
                    )}
                  </MenuContent>
                </Menu>
              </div>
              {open ? (
                <div id={panelId} className="border-t bg-muted/20 px-5 py-4 animate-fade-in">
                  <RolePermissions roleKey={role.key} rules={role.rules} />
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    );
  }

  const c = confirm;
  return (
    <Card className="min-w-0">
      <CardHeader
        title={t('access.roles.sectionTitle')}
        description={t('access.roles.sectionDescription')}
        actions={
          <Button size="sm" onClick={() => setEditing('new')} disabled={!workspaceId || !roles.data}>
            <Plus /> {t('access.roles.new')}
          </Button>
        }
      />
      {body}

      {workspaceId && editing ? (
        <RoleDialog
          open
          onOpenChange={(o) => !o && setEditing(null)}
          workspaceId={workspaceId}
          workspaceName={workspaceName}
          role={editing === 'new' ? null : editing}
          roles={list}
        />
      ) : null}

      <ConfirmDialog
        open={c !== null}
        onOpenChange={(o) => !o && !act.isPending && setConfirm(null)}
        title={c?.kind === 'reset' ? t('access.roles.resetTitle', { name: c.role.name }) : t('access.roles.deleteTitle', { name: c?.role.name ?? '' })}
        body={
          c?.kind === 'reset'
            ? t('access.roles.resetBody', { count: c.role.assignedCount })
            : t('access.roles.deleteBody')
        }
        confirmLabel={c?.kind === 'reset' ? t('access.roles.resetConfirm') : t('access.roles.deleteConfirm')}
        cancelLabel={t('common.cancel')}
        tone={c?.kind === 'reset' ? 'primary' : 'danger'}
        loading={act.isPending}
        onConfirm={() => c && act.mutate(c)}
      />
    </Card>
  );
}
