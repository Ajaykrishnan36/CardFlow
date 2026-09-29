import { useEffect, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ChevronRight, Link2 } from 'lucide-react';
import { usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { UserDetail, UserUpdateBody } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select } from '@crm/components/ui/form-controls';
import { DetailItem } from '@crm/components/page';
import { EmptyState } from '@crm/components/states';
import { LOCALES, localeLabel, timezoneOptions, userStatusTone } from './user-format';
import { useApplyUser } from './use-user-mutations';

interface FormState {
  displayName: string;
  email: string;
  phone: string;
  timezone: string;
  locale: string;
}

type FieldKey = keyof FormState;
const FIELD_KEYS: FieldKey[] = ['displayName', 'email', 'phone', 'timezone', 'locale'];

function toForm(u: UserDetail): FormState {
  return { displayName: u.displayName, email: u.email ?? '', phone: u.phone ?? '', timezone: u.timezone, locale: u.locale };
}

export function UserDetailsTab({ user, editing, onEditingChange }: { user: UserDetail; editing: boolean; onEditingChange: (v: boolean) => void }) {
  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
      <InfoCard user={user} editing={editing} onEditingChange={onEditingChange} />
      <LinkedRecordsCard user={user} />
    </div>
  );
}

function InfoCard({ user, editing, onEditingChange }: { user: UserDetail; editing: boolean; onEditingChange: (v: boolean) => void }) {
  const { t } = useTranslation();
  const apply = useApplyUser(user.id);
  const [form, setForm] = useState<FormState>(() => toForm(user));
  const [errors, setErrors] = useState<Partial<Record<FieldKey, string>>>({});
  // The platform owner's sign-in email is not editable from here.
  const emailLocked = user.isPlatformOwner;

  // Fresh form every time edit mode opens.
  useEffect(() => {
    if (editing) {
      setForm(toForm(user));
      setErrors({});
    }
  }, [editing]); // eslint-disable-line react-hooks/exhaustive-deps -- only on open

  const diff = (): UserUpdateBody => {
    const body: UserUpdateBody = {};
    const name = form.displayName.trim();
    if (name !== user.displayName) body.displayName = name;
    if (!emailLocked && form.email.trim() !== (user.email ?? '')) body.email = form.email.trim();
    if (form.phone.trim() !== (user.phone ?? '')) body.phone = form.phone.trim();
    if (form.timezone !== user.timezone) body.timezone = form.timezone;
    if (form.locale !== user.locale) body.locale = form.locale;
    return body;
  };

  const save = useMutation({
    mutationFn: (body: UserUpdateBody) => usersApi.update(user.id, body),
    onSuccess: (u) => {
      apply(u);
      onEditingChange(false);
      toast.success(t('users.info.saved'));
    },
    onError: (e) => {
      if (isApiError(e)) {
        const fe: Partial<Record<FieldKey, string>> = {};
        for (const k of FIELD_KEYS) if (e.fieldErrors[k]) fe[k] = e.fieldErrors[k];
        if (e.code === 'email_taken' && !fe.email) fe.email = e.message;
        if (e.code === 'phone_taken' && !fe.phone) fe.phone = e.message;
        if (Object.keys(fe).length > 0) {
          setErrors(fe);
          return;
        }
        toast.error(e.message);
        return;
      }
      toast.error(t('common.genericError'));
    }
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!form.displayName.trim()) {
      setErrors({ displayName: t('users.info.required') });
      return;
    }
    const body = diff();
    if (Object.keys(body).length === 0) {
      toast(t('users.info.noChanges'));
      onEditingChange(false);
      return;
    }
    setErrors({});
    save.mutate(body);
  };

  const set = (k: FieldKey) => (v: string) => {
    setForm((f) => ({ ...f, [k]: v }));
    if (errors[k]) setErrors((er) => ({ ...er, [k]: undefined }));
  };

  const hasErrors = Object.values(errors).some(Boolean);
  const dirty = editing && Object.keys(diff()).length > 0;

  if (!editing) {
    return (
      <Card className="min-w-0 self-start">
        <CardHeader title={t('users.info.title')} />
        <dl className="grid gap-x-8 gap-y-5 px-5 py-5 sm:grid-cols-2">
          <DetailItem label={t('users.info.displayName')}>{user.displayName}</DetailItem>
          <DetailItem label={t('users.info.status')}>
            <Badge tone={userStatusTone[user.status] ?? 'neutral'}>{t(`users.status.${user.status}`, { defaultValue: user.status })}</Badge>
          </DetailItem>
          <DetailItem label={t('users.info.email')}>
            {user.email ? (
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="break-all">{user.email}</span>
                <VerifiedBadge verified={user.emailVerified} />
              </span>
            ) : null}
          </DetailItem>
          <DetailItem label={t('users.info.phone')}>
            {user.phone ? (
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="tabular-nums">{user.phone}</span>
                <VerifiedBadge verified={user.phoneVerified} />
              </span>
            ) : null}
          </DetailItem>
          <DetailItem label={t('users.info.timezone')}>{user.timezone.replace(/_/g, ' ')}</DetailItem>
          <DetailItem label={t('users.info.locale')}>{localeLabel(user.locale)}</DetailItem>
        </dl>
      </Card>
    );
  }

  return (
    <Card className="min-w-0 self-start">
      <form onSubmit={onSubmit} noValidate>
        <CardHeader title={t('users.info.title')} />
        <div className="space-y-4 px-5 py-5">
          {hasErrors ? <Alert tone="danger">{t('users.info.fixErrors')}</Alert> : null}
          <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
            <Field label={t('users.info.displayName')} error={errors.displayName}>
              <Input value={form.displayName} onChange={(e) => set('displayName')(e.target.value)} autoComplete="off" maxLength={120} autoFocus />
            </Field>
            <div className="space-y-1.5">
              <p className="text-[13px] font-medium text-foreground">{t('users.info.status')}</p>
              <div className="flex h-9 items-center">
                <Badge tone={userStatusTone[user.status] ?? 'neutral'}>{t(`users.status.${user.status}`, { defaultValue: user.status })}</Badge>
              </div>
              <p className="text-[13px] text-muted-foreground">{t('users.info.statusHint')}</p>
            </div>
            <Field label={t('users.info.email')} error={errors.email} hint={emailLocked ? t('users.info.ownerEmailHint') : undefined}>
              <Input type="email" value={form.email} onChange={(e) => set('email')(e.target.value)} autoComplete="off" disabled={emailLocked} inputMode="email" />
            </Field>
            <Field label={t('users.info.phone')} error={errors.phone}>
              <Input
                type="tel"
                value={form.phone}
                onChange={(e) => set('phone')(e.target.value)}
                autoComplete="off"
                inputMode="tel"
                placeholder={t('users.info.phonePlaceholder')}
              />
            </Field>
            <Field label={t('users.info.timezone')} error={errors.timezone}>
              <Select value={form.timezone} onChange={(e) => set('timezone')(e.target.value)} options={timezoneOptions(user.timezone)} />
            </Field>
            <Field label={t('users.info.locale')} error={errors.locale}>
              <Select
                value={form.locale}
                onChange={(e) => set('locale')(e.target.value)}
                options={LOCALES.some((l) => l.value === user.locale) ? LOCALES : [{ value: user.locale, label: user.locale }, ...LOCALES]}
              />
            </Field>
          </div>
        </div>
        <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-end gap-2 rounded-b-lg border-t bg-card/95 px-5 py-3 backdrop-blur supports-[backdrop-filter]:bg-card/80">
          {dirty ? <span className="mr-auto text-xs text-muted-foreground">{t('users.info.unsaved')}</span> : null}
          <Button type="button" variant="outline" size="sm" onClick={() => onEditingChange(false)} disabled={save.isPending}>
            {t('users.info.cancel')}
          </Button>
          <Button type="submit" size="sm" loading={save.isPending}>
            {t('users.info.save')}
          </Button>
        </div>
      </form>
    </Card>
  );
}

function VerifiedBadge({ verified }: { verified: boolean }) {
  const { t } = useTranslation();
  return verified ? <Badge tone="success">{t('users.badge.verified')}</Badge> : <Badge>{t('users.badge.unverified')}</Badge>;
}

const objectTone = { leads: 'primary', accounts: 'success', contacts: 'warning' } as const;

function LinkedRecordsCard({ user }: { user: UserDetail }) {
  const { t } = useTranslation();
  return (
    <Card className="min-w-0 self-start">
      <CardHeader title={t('users.linked.title')} description={t('users.linked.description')} />
      {user.linkedRecords.length === 0 ? (
        <EmptyState icon={Link2} title={t('users.linked.empty')} className="py-8" />
      ) : (
        <ul className="divide-y">
          {user.linkedRecords.map((r) => (
            <li key={`${r.object}:${r.id}`}>
              <Link
                to={`/crm/owner/${r.object}/${r.id}`}
                className="group flex items-center gap-3 px-5 py-3 transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none"
              >
                <Badge tone={(objectTone as Record<string, 'primary' | 'success' | 'warning'>)[r.object] ?? 'neutral'} className="w-16 justify-center">
                  {t(`users.objects.${r.object}`, { defaultValue: r.object })}
                </Badge>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-foreground">{r.name}</span>
                  <span className="block font-mono text-[11px] text-muted-foreground">{r.code}</span>
                </span>
                <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" aria-hidden />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
