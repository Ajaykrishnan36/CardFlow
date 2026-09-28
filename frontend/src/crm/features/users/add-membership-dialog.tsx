import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessWorkspaceOption, RoleKey, UserDetail } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Checkbox, Select } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { RoleField } from '@crm/features/access/role-permissions';
import { accessKeys, ROLE_KEYS, workspaceLabel } from '@crm/features/access/use-access';
import { useApplyUser } from './use-user-mutations';

export function AddMembershipDialog({
  open,
  onOpenChange,
  user,
  available
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  user: UserDetail;
  /** Active workspaces the user is not in yet. */
  available: AccessWorkspaceOption[];
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[6vh] flex max-h-[88vh] max-w-lg flex-col p-0">
        {open ? <AddMembershipForm user={user} available={available} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function AddMembershipForm({ user, available, onDone }: { user: UserDetail; available: AccessWorkspaceOption[]; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const apply = useApplyUser(user.id);
  const first = available[0];
  const [workspaceId, setWorkspaceId] = useState(first?.id ?? '');
  const [roleKey, setRoleKey] = useState<RoleKey>('STAFF');
  const [productIds, setProductIds] = useState<string[]>(first?.products.map((p) => p.id) ?? []);
  const [setIds, setSetIds] = useState<string[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  const ws = available.find((w) => w.id === workspaceId);
  const platformLabel = t('access.workspace.platformLabel');

  const add = useMutation({
    mutationFn: () =>
      usersApi.addMembership(user.id, {
        workspaceId,
        roleKey,
        productIds: ws?.isPlatform ? undefined : productIds,
        permissionSetIds: setIds
      }),
    onSuccess: (u) => {
      apply(u);
      void qc.invalidateQueries({ queryKey: accessKeys.all });
      void qc.invalidateQueries({ queryKey: ['workspace', workspaceId] });
      void qc.invalidateQueries({ queryKey: ['workspaces'] });
      toast.success(t('users.access.added', { workspace: ws ? workspaceLabel(ws, platformLabel) : '' }));
      onDone();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        setErrors(e.fieldErrors);
        const known = ['workspaceId', 'roleKey', 'productIds', 'permissionSetIds'].some((k) => e.fieldErrors[k]);
        setFormError(known ? null : e.message);
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const pickWorkspace = (id: string) => {
    const next = available.find((w) => w.id === id);
    setWorkspaceId(id);
    if (!(ROLE_KEYS as string[]).includes(roleKey)) setRoleKey('STAFF');
    setProductIds(next?.products.map((p) => p.id) ?? []);
    setSetIds([]);
  };

  const toggle = (list: string[], id: string, on: boolean) => (on ? Array.from(new Set([...list, id])) : list.filter((x) => x !== id));

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    setFormError(null);
    if (!workspaceId) {
      setErrors({ workspaceId: t('users.access.workspaceRequired') });
      return;
    }
    setErrors({});
    add.mutate();
  };

  return (
    <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{t('users.access.addTitle', { name: user.displayName })}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('users.access.addDescription')}</DialogDescription>
      </div>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        {available.length === 0 ? <Alert tone="info">{t('users.access.addNone')}</Alert> : null}
        <Field label={t('users.access.workspace')} error={errors.workspaceId}>
          <Select
            value={workspaceId}
            onChange={(e) => pickWorkspace(e.target.value)}
            placeholder={workspaceId ? undefined : t('users.access.chooseWorkspace')}
            options={available.map((w) => ({ value: w.id, label: w.isPlatform ? platformLabel : `${w.name} · ${w.code}` }))}
          />
        </Field>
        <RoleField workspaceId={workspaceId || undefined} label={t('users.access.role')} value={roleKey} onChange={setRoleKey} error={errors.roleKey} />

        {ws && !ws.isPlatform ? (
          <fieldset>
            <legend className="mb-2 text-[13px] font-medium text-foreground">{t('users.access.products')}</legend>
            {ws.products.length === 0 ? (
              <p className="text-xs text-muted-foreground">{t('users.access.noProducts')}</p>
            ) : (
              <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
                {ws.products.map((p) => (
                  <Checkbox key={p.id} label={p.name} checked={productIds.includes(p.id)} onCheckedChange={(on) => setProductIds((ids) => toggle(ids, p.id, on))} />
                ))}
              </div>
            )}
            {errors.productIds ? <p className="mt-1.5 text-[13px] text-danger">{errors.productIds}</p> : <p className="mt-1.5 text-xs text-muted-foreground">{t('users.access.productsHint')}</p>}
          </fieldset>
        ) : null}

        {ws ? (
          <fieldset>
            <legend className="mb-2 text-[13px] font-medium text-foreground">{t('users.access.permissionSets')}</legend>
            {ws.permissionSets.length === 0 ? (
              <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">{t('users.access.noSets')}</p>
            ) : (
              <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
                {ws.permissionSets.map((s) => (
                  <Checkbox key={s.id} label={s.name} checked={setIds.includes(s.id)} onCheckedChange={(on) => setSetIds((ids) => toggle(ids, s.id, on))} />
                ))}
              </div>
            )}
            {errors.permissionSetIds ? <p className="mt-1.5 text-[13px] text-danger">{errors.permissionSetIds}</p> : null}
            {roleKey === 'END_USER' && setIds.length === 0 ? <p className="mt-1.5 text-xs font-medium text-warning">{t('access.login.endUserWarning')}</p> : null}
          </fieldset>
        ) : null}
      </div>
      <div className="flex justify-end gap-2 border-t px-5 py-3">
        <Button type="button" variant="outline" onClick={onDone} disabled={add.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={add.isPending} disabled={available.length === 0}>
          {t('users.access.addSubmit')}
        </Button>
      </div>
    </form>
  );
}
