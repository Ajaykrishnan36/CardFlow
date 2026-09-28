import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Trash2 } from 'lucide-react';
import { accessApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessRules, PermissionSet } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { ConfirmDialog } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { PermissionSetEditor } from './permission-set-editor';
import { emptyRules, normalizeRules, rulesEqual, useAccessCatalog, useAccessWorkspaces, useInvalidateAccess, useWorkspaceRoles, workspaceLabel } from './use-access';

export interface PermissionSetDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  workspaceId: string;
  /** Shown in the description; looked up when omitted. */
  workspaceName?: string;
  /** Edit this set; omitted/null = create. */
  permissionSet?: PermissionSet | null;
  onSaved?: (set: PermissionSet) => void;
  onDeleted?: (id: string) => void;
}

/** Create or edit a permission set in one workspace (name, description, permissions). */
export function PermissionSetDialog(props: PermissionSetDialogProps) {
  const { open, onOpenChange } = props;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-3xl flex-col p-0">
        {open ? <PermissionSetForm {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function PermissionSetForm({ onOpenChange, workspaceId, workspaceName, permissionSet, onSaved, onDeleted }: PermissionSetDialogProps) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const workspaces = useAccessWorkspaces();
  const invalidate = useInvalidateAccess();
  const wsRoles = useWorkspaceRoles(workspaceId);
  const editing = Boolean(permissionSet);

  const [name, setName] = useState(permissionSet?.name ?? '');
  const [description, setDescription] = useState(permissionSet?.description ?? '');
  const [rules, setRules] = useState<AccessRules>(() => permissionSet?.rules ?? emptyRules());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const ws = workspaces.data?.find((w) => w.id === workspaceId);
  const wsName = workspaceName ?? (ws ? workspaceLabel(ws, t('access.workspace.platformLabel')) : '…');

  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), description: description.trim(), rules: normalizeRules(rules, catalog.data) };
      return permissionSet ? accessApi.updatePermissionSet(permissionSet.id, body) : accessApi.createPermissionSet(workspaceId, body);
    },
    onSuccess: (set) => {
      invalidate();
      toast.success(editing ? t('access.sets.updated') : t('access.sets.created'));
      onSaved?.(set);
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

  const remove = useMutation({
    mutationFn: () => accessApi.deletePermissionSet(permissionSet?.id ?? ''),
    onSuccess: () => {
      invalidate();
      toast.success(t('access.sets.deleted'));
      setConfirmDelete(false);
      if (permissionSet) onDeleted?.(permissionSet.id);
      onOpenChange(false);
    },
    onError: (e) => {
      // 409 permission_set_in_use carries a human message ("Assigned to 3 users…").
      setConfirmDelete(false);
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!name.trim()) e.name = t('access.sets.nameRequired');
    setErrors(e);
    setFormError(null);
    if (Object.keys(e).length) return;
    save.mutate();
  };

  const copyFromRole = (key: string) => {
    const role = (wsRoles.data ?? catalog.data?.roles)?.find((r) => r.key === key);
    if (role) setRules(normalizeRules(role.rules, catalog.data));
  };

  const unchanged =
    editing &&
    permissionSet &&
    name.trim() === permissionSet.name &&
    description.trim() === (permissionSet.description ?? '') &&
    catalog.data &&
    rulesEqual(rules, permissionSet.rules, catalog.data);

  return (
    <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{editing ? t('access.sets.editTitle') : t('access.sets.createTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('access.sets.dialogDescription', { workspace: wsName })}</DialogDescription>
      </div>

      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
          <Field label={t('access.sets.name')} error={errors.name}>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('access.sets.namePlaceholder')} maxLength={80} autoFocus autoComplete="off" />
          </Field>
          <Field label={t('access.sets.descriptionLabel')} error={errors.description}>
            <Textarea
              rows={1}
              className="min-h-9 py-1.5"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('access.sets.descriptionPlaceholder')}
              maxLength={300}
            />
          </Field>
        </div>

        <div>
          <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
            <h3 className="text-[13px] font-medium text-foreground">{t('access.sets.permissions')}</h3>
            {catalog.data ? (
              <Select
                className="w-full sm:w-60"
                value=""
                onChange={(e) => copyFromRole(e.target.value)}
                aria-label={t('access.sets.startFrom')}
                placeholder={t('access.sets.startFromPlaceholder')}
                options={(wsRoles.data ?? catalog.data.roles).map((r) => ({ value: r.key, label: r.name }))}
                disabled={save.isPending}
              />
            ) : null}
          </div>
          {catalog.data ? (
            <PermissionSetEditor value={rules} onChange={setRules} catalog={catalog.data} disabled={save.isPending || remove.isPending} />
          ) : catalog.isError ? (
            <ErrorState
              title={t('access.sets.errorTitle')}
              message={isApiError(catalog.error) ? catalog.error.message : undefined}
              onRetry={() => void catalog.refetch()}
            />
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

      <div className="flex flex-wrap items-center justify-between gap-2 border-t px-5 py-3">
        <div>
          {editing ? (
            <Button type="button" variant="danger-outline" size="sm" onClick={() => setConfirmDelete(true)} disabled={save.isPending || remove.isPending}>
              <Trash2 /> {t('access.sets.delete')}
            </Button>
          ) : null}
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>
            {t('common.cancel')}
          </Button>
          <Button type="submit" loading={save.isPending} disabled={!catalog.data || Boolean(unchanged)}>
            {editing ? t('access.sets.save') : t('access.sets.create')}
          </Button>
        </div>
      </div>

      {permissionSet ? (
        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={(o) => !remove.isPending && setConfirmDelete(o)}
          title={t('access.sets.deleteTitle', { name: permissionSet.name })}
          body={t('access.sets.deleteBody')}
          confirmLabel={t('access.sets.deleteConfirm')}
          cancelLabel={t('common.cancel')}
          tone="danger"
          loading={remove.isPending}
          onConfirm={() => remove.mutate()}
        />
      ) : null}
    </form>
  );
}
