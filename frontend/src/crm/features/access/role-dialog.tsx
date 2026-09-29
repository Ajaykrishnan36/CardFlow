import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Lock } from 'lucide-react';
import { accessApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessRules, RoleBody, WorkspaceRole } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { ErrorState } from '@crm/components/states';
import { PermissionSetEditor } from './permission-set-editor';
import { RoleBadge } from './role-permissions';
import { emptyRules, normalizeRules, rulesEqual, useAccessCatalog, useInvalidateAccess } from './use-access';

export interface RoleDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspaceId: string;
  workspaceName?: string;
  /** Edit this role; null/omitted = new custom role. */
  role?: WorkspaceRole | null;
  /** The workspace's roles, for "Start from". */
  roles: WorkspaceRole[];
  onSaved?: (role: WorkspaceRole) => void;
}

/** Create a custom role or edit a role's permissions (built-in roles keep their name and description). */
export function RoleDialog(props: RoleDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-3xl flex-col p-0">{props.open ? <RoleForm {...props} /> : null}</DialogContent>
    </Dialog>
  );
}

function RoleForm({ onOpenChange, workspaceId, workspaceName, role, roles, onSaved }: RoleDialogProps) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const fieldCatalog = useQuery({ queryKey: ['access', 'fields', workspaceId], queryFn: () => accessApi.fieldCatalog(workspaceId), staleTime: 60_000 });
  const invalidate = useInvalidateAccess();
  const editing = Boolean(role);
  const builtIn = Boolean(role?.isSystem);

  const [name, setName] = useState(role?.name ?? '');
  const [description, setDescription] = useState(role?.description ?? '');
  const [rules, setRules] = useState<AccessRules>(() => role?.rules ?? emptyRules());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: () => {
      const normalized = normalizeRules(rules, catalog.data);
      if (role) {
        // Built-in roles: only permissions are editable.
        const body: Partial<RoleBody> = builtIn ? { rules: normalized } : { name: name.trim(), description: description.trim(), rules: normalized };
        return accessApi.updateRole(role.id, body);
      }
      return accessApi.createRole(workspaceId, { name: name.trim(), description: description.trim() || undefined, rules: normalized });
    },
    onSuccess: (saved) => {
      invalidate();
      toast.success(editing ? t('access.roles.updated', { name: saved.name }) : t('access.roles.created', { name: saved.name }));
      onSaved?.(saved);
      onOpenChange(false);
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        setErrors(e.fieldErrors);
        setFormError(e.fieldErrors.name || e.fieldErrors.description ? null : e.message);
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!builtIn && !name.trim()) e.name = t('access.roles.nameRequired');
    setErrors(e);
    setFormError(null);
    if (Object.keys(e).length) return;
    save.mutate();
  };

  const copyFrom = (id: string) => {
    const src = roles.find((r) => r.id === id);
    if (src) setRules(normalizeRules(src.rules, catalog.data));
  };

  const unchanged =
    role &&
    catalog.data &&
    rulesEqual(rules, role.rules, catalog.data) &&
    (builtIn || (name.trim() === role.name && description.trim() === (role.description ?? '')));

  const others = roles.filter((r) => r.id !== role?.id);

  return (
    <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-5 py-4">
        <div className="flex flex-wrap items-center gap-2 pr-8">
          <DialogTitle className="text-base font-semibold">
            {role ? t('access.roles.editTitle', { name: role.name }) : t('access.roles.createTitle')}
          </DialogTitle>
          {role ? <RoleBadge role={role} showBuiltIn /> : null}
        </div>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
          {t('access.roles.dialogIn', { workspace: workspaceName ?? '…' })} {editing ? t('access.roles.appliesNow') : null}
        </DialogDescription>
      </div>

      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        {role && role.assignedCount > 0 ? <Alert tone="warning">{t('access.roles.assignedWarning', { count: role.assignedCount })}</Alert> : null}

        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
          <Field
            label={t('access.roles.name')}
            error={errors.name}
            hint={builtIn ? t('access.roles.builtInLocked') : !editing ? t('access.roles.keyHint') : undefined}
            labelAside={builtIn ? <Lock className="size-3.5 text-muted-foreground" aria-label={t('access.roles.builtInLocked')} /> : undefined}
          >
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('access.roles.namePlaceholder')}
              maxLength={60}
              autoFocus={!builtIn}
              autoComplete="off"
              readOnly={builtIn}
              disabled={builtIn}
            />
          </Field>
          <Field label={t('access.roles.descriptionLabel')} error={errors.description}>
            <Textarea
              rows={1}
              className="min-h-9 py-1.5"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('access.roles.descriptionPlaceholder')}
              maxLength={300}
              readOnly={builtIn}
              disabled={builtIn}
            />
          </Field>
        </div>

        <div>
          <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
            <h3 className="text-[13px] font-medium text-foreground">{t('access.roles.permissions')}</h3>
            {catalog.data && others.length > 0 ? (
              <Select
                className="w-full sm:w-60"
                value=""
                onChange={(e) => copyFrom(e.target.value)}
                aria-label={t('access.roles.startFrom')}
                placeholder={t('access.roles.startFromPlaceholder')}
                options={others.map((r) => ({ value: r.id, label: r.name }))}
                disabled={save.isPending}
              />
            ) : null}
          </div>
          {catalog.data ? (
            <PermissionSetEditor
              value={rules}
              onChange={setRules}
              catalog={catalog.data}
              disabled={save.isPending}
              fieldCatalog={fieldCatalog.data}
              fieldCatalogLoading={fieldCatalog.isPending}
            />
          ) : catalog.isError ? (
            <ErrorState title={t('access.roles.errorTitle')} message={isApiError(catalog.error) ? catalog.error.message : undefined} onRetry={() => void catalog.refetch()} />
          ) : (
            <div className="space-y-2" aria-busy>
              <Skeleton className="h-9" />
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          )}
          {errors.rules ? <p className="mt-1.5 text-[13px] text-danger">{errors.rules}</p> : null}
        </div>
      </div>

      <div className="flex justify-end gap-2 border-t px-5 py-3">
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={save.isPending} disabled={!catalog.data || Boolean(unchanged)}>
          {editing ? t('access.roles.save') : t('access.roles.create')}
        </Button>
      </div>
    </form>
  );
}
