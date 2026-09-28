import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { WorkspaceDetail } from '@crm/api/types';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select } from '@crm/components/ui/form-controls';
import { CURRENCIES, LOCALES, TIMEZONES, withCurrent } from './workspace-ui';

interface SettingsForm {
  name: string;
  timezone: string;
  locale: string;
  currency: string;
}

const fromWorkspace = (w: WorkspaceDetail): SettingsForm => ({ name: w.name, timezone: w.timezone, locale: w.locale, currency: w.currency });
const same = (a: SettingsForm, b: SettingsForm) => a.name === b.name && a.timezone === b.timezone && a.locale === b.locale && a.currency === b.currency;

export function WorkspaceSettingsTab({ workspace }: { workspace: WorkspaceDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const server = useMemo(() => fromWorkspace(workspace), [workspace]);
  const [form, setForm] = useState<SettingsForm>(server);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  // Follow server changes only while the form has no local edits.
  const prev = useRef(server);
  useEffect(() => {
    const before = prev.current;
    prev.current = server;
    setForm((f) => (same(f, before) ? server : f));
  }, [server]);

  const dirty = !same(form, server);

  const save = useMutation({
    mutationFn: () => workspacesApi.update(workspace.id, { ...form, name: form.name.trim() }),
    onSuccess: (w) => {
      qc.setQueryData(['workspace', w.id], w);
      void qc.invalidateQueries({ queryKey: ['workspaces'] });
      setForm(fromWorkspace(w));
      toast.success(t('workspaces.settings.saved'));
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        setErrors(e.fieldErrors);
        const known = ['name', 'timezone', 'locale', 'currency'].some((k) => e.fieldErrors[k]);
        setFormError(known ? null : e.message);
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  const set = (key: keyof SettingsForm, value: string) => {
    setForm((f) => ({ ...f, [key]: value }));
    if (errors[key]) setErrors((e) => ({ ...e, [key]: '' }));
  };

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    setFormError(null);
    if (!form.name.trim()) {
      setErrors({ name: t('workspaces.provision.nameRequired') });
      return;
    }
    setErrors({});
    save.mutate();
  };

  return (
    <Card className="max-w-3xl">
      <CardHeader title={t('workspaces.settings.title')} description={t('workspaces.settings.description')} />
      <form onSubmit={onSubmit} noValidate>
        <div className="space-y-4 px-5 py-5">
          {formError ? <Alert tone="danger">{formError}</Alert> : null}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t('workspaces.provision.name')} error={errors.name} className="sm:col-span-2">
              <Input value={form.name} onChange={(e) => set('name', e.target.value)} maxLength={120} />
            </Field>
            <Field label={t('workspaces.provision.code')} hint={t('workspaces.settings.codeHint')} className="sm:col-span-2">
              <Input value={workspace.code} className="font-mono" disabled readOnly />
            </Field>
            <Field label={t('workspaces.fields.timezone')} error={errors.timezone} className="sm:col-span-2">
              <Select
                value={form.timezone}
                onChange={(e) => set('timezone', e.target.value)}
                options={withCurrent(TIMEZONES, form.timezone).map((z) => ({ value: z, label: z.replace(/_/g, ' ') }))}
              />
            </Field>
            <Field label={t('workspaces.fields.locale')} error={errors.locale}>
              <Select
                value={form.locale}
                onChange={(e) => set('locale', e.target.value)}
                options={withCurrent(LOCALES, form.locale).map((l) => ({ value: l, label: t(`workspaces.locales.${l}`, { defaultValue: l }) }))}
              />
            </Field>
            <Field label={t('workspaces.fields.currency')} error={errors.currency}>
              <Select
                value={form.currency}
                onChange={(e) => set('currency', e.target.value)}
                options={withCurrent(CURRENCIES, form.currency).map((c) => ({ value: c, label: `${c} — ${t(`workspaces.currencies.${c}`, { defaultValue: c })}` }))}
              />
            </Field>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 rounded-b-lg border-t bg-muted/30 px-5 py-3">
          {dirty ? (
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setForm(server);
                setErrors({});
                setFormError(null);
              }}
              disabled={save.isPending}
            >
              {t('workspaces.settings.discard')}
            </Button>
          ) : null}
          <Button type="submit" disabled={!dirty} loading={save.isPending}>
            {save.isPending ? t('workspaces.settings.saving') : t('workspaces.settings.save')}
          </Button>
        </div>
      </form>
    </Card>
  );
}
