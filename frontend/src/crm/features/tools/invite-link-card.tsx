import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Link2, RefreshCw } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { InviteLinkSettings } from '@crm/api/types-features';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Switch } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog } from '@crm/components/page';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { useAdminOptions } from '@crm/features/workspace/admin/admin-utils';
import { CopyField, errorText, hasCap, SettingRow, toolKeys, useTools } from './tool-utils';

/** Users & access: a shareable link that lets people from the company's email domains join (D-83). */
export function InviteLinkCard() {
  const { code, context } = useWorkspace();
  const api = useTools();
  const allowed = hasCap('access.manage')(context);
  const q = useQuery({ queryKey: toolKeys.one(code, 'invite-link'), queryFn: () => api.inviteLink(), enabled: allowed });
  if (!allowed) return null;
  if (!q.data) return <Skeleton className="h-40 w-full" />;
  return <InviteLinkForm key={q.data.updatedAt ?? 'new'} s={q.data} />;
}

function InviteLinkForm({ s }: { s: InviteLinkSettings }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const roles = useAdminOptions(code);
  const [enabled, setEnabled] = useState(s.configured ? s.enabled : true);
  const [domains, setDomains] = useState(s.domains.join(', '));
  const [role, setRole] = useState(s.roleKey);
  const [confirmNew, setConfirmNew] = useState(false);
  const save = useMutation({
    mutationFn: (regenerate: boolean) =>
      api.saveInviteLink({
        enabled,
        roleKey: role,
        regenerate,
        domains: domains
          .split(/[\s,;]+/)
          .map((d) => d.trim().toLowerCase())
          .filter(Boolean)
      }),
    onSuccess: (_, regenerate) => {
      toast.success(regenerate ? t('inviteLink.regenerated') : t('inviteLink.saved'));
      setConfirmNew(false);
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'invite-link') });
    }
  });
  const fieldError = (k: string) => (save.error && isApiError(save.error) ? save.error.fieldErrors[k] : undefined);
  const roleOptions = (roles.data?.roles ?? []).filter((r) => r.key !== 'SUPER_ADMIN').map((r) => ({ value: r.key, label: r.name }));
  return (
    <Card>
      <CardHeader
        title={
          <span className="inline-flex items-center gap-2">
            <Link2 className="size-4 text-primary" /> {t('inviteLink.title')}
          </span>
        }
        description={t('inviteLink.body')}
        actions={s.configured ? <Badge tone={s.enabled ? 'success' : 'neutral'}>{s.enabled ? t('inviteLink.on') : t('inviteLink.off')}</Badge> : null}
      />
      <form
        noValidate
        className="space-y-4 px-5 py-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(false);
        }}
      >
        {save.isError && !Object.keys(isApiError(save.error) ? save.error.fieldErrors : {}).length ? (
          <Alert tone="danger">{errorText(save.error, t('common.genericError'))}</Alert>
        ) : null}
        {s.configured && s.url ? (
          <CopyField label={t('inviteLink.link')} value={s.url} hint={t('inviteLink.uses', { count: s.uses })} />
        ) : null}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('inviteLink.domains')} hint={t('inviteLink.domainsHint')} error={fieldError('domains')}>
            <Input value={domains} onChange={(e) => setDomains(e.target.value)} placeholder="acme.com, acme.in" />
          </Field>
          <Field label={t('inviteLink.role')} error={fieldError('roleKey')}>
            <Select value={role} onChange={(e) => setRole(e.target.value)} options={roleOptions.length ? roleOptions : [{ value: role, label: role }]} />
          </Field>
        </div>
        <div className="rounded-lg border px-4">
          <SettingRow title={t('inviteLink.enabled')} body={t('inviteLink.enabledHint')}>
            <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={t('inviteLink.enabled')} />
          </SettingRow>
        </div>
        <div className="flex flex-wrap justify-between gap-2">
          {s.configured ? (
            <Button type="button" variant="outline" onClick={() => setConfirmNew(true)}>
              <RefreshCw /> {t('inviteLink.regenerate')}
            </Button>
          ) : (
            <span />
          )}
          <Button type="submit" loading={save.isPending && !confirmNew}>
            {s.configured ? t('inviteLink.save') : t('inviteLink.create')}
          </Button>
        </div>
      </form>
      <ConfirmDialog
        open={confirmNew}
        onOpenChange={setConfirmNew}
        title={t('inviteLink.regenerateTitle')}
        body={t('inviteLink.regenerateBody')}
        confirmLabel={t('inviteLink.regenerate')}
        loading={save.isPending}
        onConfirm={() => save.mutate(true)}
      />
    </Card>
  );
}
