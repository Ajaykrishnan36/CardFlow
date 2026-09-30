import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import type { SSOSettings } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Switch, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { useAdminOptions } from '@crm/features/workspace/admin/admin-utils';
import { CopyField, errorText, formatDateTime, hasCap, SettingRow, toolKeys, useTools } from './tool-utils';

/** /crm/w/:ws/settings/sso — SAML single sign-on for this workspace (access.manage). */
export function SsoPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('tools.sso.title'));
  const api = useTools();
  const key = toolKeys.one(code, 'sso');
  const q = useQuery({ queryKey: key, queryFn: () => api.sso(), enabled: hasCap('access.manage')(context) });
  if (!hasCap('access.manage')(context)) return <NoAccessPage />;
  return (
    <PageContainer>
      <PageHeader title={t('tools.sso.title')} description={t('tools.sso.subtitle')} />
      {q.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : q.isError ? (
        <Card>
          <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />
        </Card>
      ) : (
        <SsoForm key={q.data.updatedAt ?? 'new'} s={q.data} />
      )}
    </PageContainer>
  );
}

function SsoForm({ s }: { s: SSOSettings }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const roles = useAdminOptions(code);
  const [enabled, setEnabled] = useState(s.enabled || !s.configured);
  const [name, setName] = useState(s.name);
  const [source, setSource] = useState<'xml' | 'url'>('xml');
  const [xml, setXml] = useState(s.idpMetadataXml ?? '');
  const [metaUrl, setMetaUrl] = useState('');
  const [domains, setDomains] = useState(s.domains.join(', '));
  const [jit, setJit] = useState(s.jitProvisioning);
  const [role, setRole] = useState(s.defaultRoleKey);
  const [removing, setRemoving] = useState(false);
  const refresh = () => void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'sso') });
  const save = useMutation({
    mutationFn: () =>
      api.saveSso({
        enabled,
        name: name.trim(),
        idpMetadataXml: source === 'xml' ? xml : undefined,
        idpMetadataUrl: source === 'url' ? metaUrl.trim() : undefined,
        domains: domains
          .split(/[\s,;]+/)
          .map((d) => d.trim().toLowerCase())
          .filter(Boolean),
        jitProvisioning: jit,
        defaultRoleKey: role
      }),
    onSuccess: () => {
      toast.success(t('tools.sso.saved'));
      refresh();
    }
  });
  const del = useMutation({
    mutationFn: () => api.deleteSso(),
    onSuccess: () => {
      toast.success(t('tools.sso.removed'));
      setRemoving(false);
      refresh();
    }
  });
  const roleOptions = (roles.data?.roles ?? []).filter((r) => r.key !== 'SUPER_ADMIN').map((r) => ({ value: r.key, label: r.name }));
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_380px]">
      <Card>
        <CardHeader
          title={t('tools.sso.idp')}
          description={t('tools.sso.idpBody')}
          actions={s.configured ? <Badge tone={s.enabled ? 'success' : 'neutral'}>{s.enabled ? t('tools.sso.on') : t('tools.sso.off')}</Badge> : null}
        />
        <form
          noValidate
          className="space-y-4 px-5 py-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          {save.isError ? <Alert tone="danger">{errorText(save.error, t('common.genericError'))}</Alert> : null}
          {!s.loginMethodOn ? <Alert tone="warning" title={t('tools.sso.methodOffTitle')}>{t('tools.sso.methodOffBody')}</Alert> : null}
          <Field label={t('tools.sso.buttonName')} hint={t('tools.sso.buttonNameHint')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} />
          </Field>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">{t('tools.sso.metadata')}</p>
            <Tabs
              className="mb-3"
              value={source}
              onChange={setSource}
              items={[
                { value: 'xml', label: t('tools.sso.pasteXml') },
                { value: 'url', label: t('tools.sso.fromUrl') }
              ]}
            />
            {source === 'xml' ? (
              <Textarea value={xml} onChange={(e) => setXml(e.target.value)} rows={8} className="font-mono text-[11px]" placeholder='<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" …' spellCheck={false} />
            ) : (
              <Input value={metaUrl} onChange={(e) => setMetaUrl(e.target.value)} placeholder="https://login.example.com/app/metadata.xml" inputMode="url" />
            )}
            {s.idpEntityId ? <p className="mt-1.5 text-xs text-muted-foreground">{t('tools.sso.currentIdp', { id: s.idpEntityId })}</p> : null}
          </div>
          <Field label={t('tools.sso.domains')} hint={t('tools.sso.domainsHint')}>
            <Input value={domains} onChange={(e) => setDomains(e.target.value)} placeholder="example.com, example.in" />
          </Field>
          <div className="divide-y rounded-lg border px-4">
            <SettingRow title={t('tools.sso.enabled')} body={t('tools.sso.enabledHint')}>
              <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={t('tools.sso.enabled')} />
            </SettingRow>
            <SettingRow title={t('tools.sso.jit')} body={t('tools.sso.jitHint')}>
              <Switch checked={jit} onCheckedChange={setJit} aria-label={t('tools.sso.jit')} />
            </SettingRow>
            {jit ? (
              <SettingRow title={t('tools.sso.defaultRole')} body={t('tools.sso.defaultRoleHint')}>
                <Select value={role} onChange={(e) => setRole(e.target.value)} options={roleOptions.length ? roleOptions : [{ value: role, label: role }]} className="w-48" />
              </SettingRow>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-between gap-2">
            {s.configured ? (
              <Button type="button" variant="danger-outline" onClick={() => setRemoving(true)}>
                {t('tools.sso.remove')}
              </Button>
            ) : (
              <span />
            )}
            <Button type="submit" loading={save.isPending}>
              {t('tools.common.save')}
            </Button>
          </div>
          {s.configured && s.updatedAt ? <p className="text-right text-xs text-muted-foreground">{t('tools.common.updated', { when: formatDateTime(s.updatedAt) })}</p> : null}
        </form>
      </Card>
      <Card className="h-fit">
        <CardHeader title={t('tools.sso.sp')} description={t('tools.sso.spBody')} />
        <div className="space-y-3 px-5 py-4">
          <CopyField label={t('tools.sso.entityId')} value={s.spEntityId} />
          <CopyField label={t('tools.sso.acsUrl')} value={s.spAcsUrl} />
          <CopyField label={t('tools.sso.spMetadata')} value={s.spMetadataUrl} />
          <CopyField label={t('tools.sso.signInUrl')} value={s.signInUrl} hint={t('tools.sso.signInUrlHint')} />
          <p className="text-xs text-muted-foreground">{t('tools.sso.attributes')}</p>
        </div>
      </Card>
      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title={t('tools.sso.removeTitle')}
        body={t('tools.sso.removeBody')}
        confirmLabel={t('tools.sso.remove')}
        tone="danger"
        loading={del.isPending}
        onConfirm={() => del.mutate()}
      />
    </div>
  );
}
