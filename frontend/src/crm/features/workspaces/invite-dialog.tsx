import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { AtSign, CircleCheck, Phone } from 'lucide-react';
import { workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { Invitation, RoleKey, WorkspaceDetail } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Checkbox } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { DevLink } from '@crm/components/page';
import { RoleField } from '@crm/features/access/role-permissions';
import { InviteStatusBadge, isOpenInvite } from './workspace-ui';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const PHONE_RE = /^\+?[\d\s()-]{8,20}$/;

export function InviteDialog({ open, onOpenChange, workspace }: { open: boolean; onOpenChange: (o: boolean) => void; workspace: WorkspaceDetail }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-0">{open ? <InviteForm workspace={workspace} onDone={() => onOpenChange(false)} /> : null}</DialogContent>
    </Dialog>
  );
}

/** Suggest Super Admin while the workspace has none (active, invited or pending). */
function defaultRole(w: WorkspaceDetail): RoleKey {
  const hasSuper =
    w.memberList.some((m) => m.roleKey === 'SUPER_ADMIN' && (m.status === 'active' || m.status === 'invited')) ||
    w.invitations.some((i) => i.roleKey === 'SUPER_ADMIN' && isOpenInvite(i));
  return hasSuper ? 'ADMIN' : 'SUPER_ADMIN';
}

function InviteForm({ workspace, onDone }: { workspace: WorkspaceDetail; onDone: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const activeProducts = workspace.productList.filter((p) => p.status === 'active');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [roleKey, setRoleKey] = useState<RoleKey>(() => defaultRole(workspace));
  const [productIds, setProductIds] = useState<string[]>(() => activeProducts.map((p) => p.productId));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [sent, setSent] = useState<Invitation | null>(null);

  const invite = useMutation({
    mutationFn: () => workspacesApi.invite(workspace.id, { name: name.trim(), email: email.trim(), phone: phone.trim() || undefined, roleKey, productIds }),
    onSuccess: (inv) => {
      void qc.invalidateQueries({ queryKey: ['workspace', workspace.id] });
      void qc.invalidateQueries({ queryKey: ['workspaces'] });
      setSent(inv);
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        const fe = e.fieldErrors;
        setErrors(fe);
        const known = ['name', 'email', 'phone', 'roleKey', 'productIds'].some((k) => fe[k]);
        setFormError(known ? null : e.message);
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    const e: Record<string, string> = {};
    if (!name.trim()) e.name = t('workspaces.invite.nameRequired');
    if (!EMAIL_RE.test(email.trim())) e.email = t('workspaces.provision.adminEmailInvalid');
    if (phone.trim() && !PHONE_RE.test(phone.trim())) e.phone = t('workspaces.invite.phoneInvalid');
    setErrors(e);
    setFormError(null);
    if (Object.keys(e).length > 0) return;
    invite.mutate();
  };

  const reset = () => {
    setSent(null);
    setName('');
    setEmail('');
    setPhone('');
    setErrors({});
    setFormError(null);
  };

  if (sent) {
    return (
      <div>
        <div className="flex flex-col items-center px-6 pb-5 pt-8 text-center">
          <span className="grid size-11 place-items-center rounded-full bg-success-soft text-success">
            <CircleCheck className="size-5" aria-hidden />
          </span>
          <DialogTitle className="mt-3 text-base font-semibold">{t('workspaces.invite.sentTitle')}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            {t('workspaces.invite.sentBody', { email: sent.email ?? sent.displayName, role: sent.roleName, workspace: workspace.name })}
          </DialogDescription>
          <div className="mt-2">
            <InviteStatusBadge status={sent.status} />
          </div>
        </div>
        {sent.devAcceptUrl ? (
          <div className="px-5 pb-5">
            <DevLink url={sent.devAcceptUrl} label={t('workspaces.devInviteLink')} />
          </div>
        ) : null}
        <div className="flex justify-end gap-2 border-t px-5 py-3.5">
          <Button variant="outline" onClick={reset}>
            {t('workspaces.invite.another')}
          </Button>
          <Button onClick={onDone}>{t('workspaces.invite.done')}</Button>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={onSubmit} noValidate>
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{t('workspaces.invite.title')}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('workspaces.invite.description', { workspace: workspace.name })}</DialogDescription>
      </div>
      <div className="space-y-4 px-5 py-5">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('workspaces.invite.name')} error={errors.name}>
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus autoComplete="off" />
          </Field>
          <Field label={t('workspaces.invite.email')} error={errors.email}>
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" autoCapitalize="none" leading={<AtSign />} />
          </Field>
        </div>
        <Field label={t('workspaces.invite.phone')} hint={t('workspaces.invite.phoneHint')} error={errors.phone}>
          <Input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} autoComplete="off" inputMode="tel" leading={<Phone />} />
        </Field>
        <RoleField workspaceId={workspace.id} label={t('workspaces.invite.role')} value={roleKey} onChange={setRoleKey} error={errors.roleKey} />
        <fieldset>
          <legend className="mb-2 text-[13px] font-medium text-foreground">{t('workspaces.invite.products')}</legend>
          {activeProducts.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('workspaces.invite.noProducts')}</p>
          ) : (
            <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
              {activeProducts.map((p) => (
                <Checkbox
                  key={p.productId}
                  label={p.name}
                  description={`v${p.configVersion}`}
                  checked={productIds.includes(p.productId)}
                  onCheckedChange={(on) => setProductIds((ids) => (on ? [...ids, p.productId] : ids.filter((x) => x !== p.productId)))}
                />
              ))}
            </div>
          )}
          {errors.productIds ? <p className="mt-1.5 text-[13px] text-danger">{errors.productIds}</p> : null}
        </fieldset>
      </div>
      <div className="flex justify-end gap-2 border-t px-5 py-3.5">
        <Button type="button" variant="outline" onClick={onDone} disabled={invite.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={invite.isPending}>
          {invite.isPending ? t('workspaces.invite.sending') : t('workspaces.invite.submit')}
        </Button>
      </div>
    </form>
  );
}
