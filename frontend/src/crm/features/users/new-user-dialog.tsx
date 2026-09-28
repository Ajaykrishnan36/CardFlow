import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { AtSign, Phone } from 'lucide-react';
import { usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { CreateUserResult, GiveLoginBody } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import {
  defaultGiveLoginBody,
  GiveLoginFields,
  isGiveLoginField,
  LoginCreatedSummary,
  useInvalidateAfterLogin,
  validateGiveLogin
} from '@crm/features/access/give-login-dialog';
import { userKey } from './use-user-mutations';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Owner creates a user directly: who they are + where they work + how they sign in. */
export function NewUserDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [run, setRun] = useState(0);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-2xl flex-col p-0">
        {open ? <NewUserForm key={run} onClose={() => onOpenChange(false)} onAnother={() => setRun((r) => r + 1)} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function NewUserForm({ onClose, onAnother }: { onClose: () => void; onAnother: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const invalidate = useInvalidateAfterLogin();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [login, setLogin] = useState<GiveLoginBody>(() => defaultGiveLoginBody());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [done, setDone] = useState<{ result: CreateUserResult; body: GiveLoginBody } | null>(null);

  const create = useMutation({
    mutationFn: (body: GiveLoginBody) =>
      usersApi.create({ ...body, displayName: name.trim(), email: email.trim(), phone: phone.trim() || undefined }),
    onSuccess: (result, body) => {
      qc.setQueryData(userKey(result.identityId), result.user);
      invalidate();
      setDone({ result, body });
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
        const fe = e.fieldErrors;
        setErrors(fe);
        const shown = ['displayName', 'email', 'phone'].some((k) => fe[k]) || Object.keys(fe).some(isGiveLoginField);
        setFormError(shown ? t('access.login.fixErrors') : e.message);
        return;
      }
      setFormError(isApiError(e) ? e.message : t('common.genericError'));
    }
  });

  if (done) {
    const displayName = done.result.user?.displayName ?? name.trim();
    return (
      <LoginCreatedSummary
        personName={displayName}
        email={email.trim()}
        body={done.body}
        result={done.result}
        actions={
          <>
            <Button variant="outline" onClick={onAnother}>
              {t('users.create.another')}
            </Button>
            <Button
              onClick={() => {
                onClose();
                navigate(`/crm/owner/users/${done.result.identityId}`);
              }}
            >
              {t('users.create.openUser', { name: displayName })}
            </Button>
          </>
        }
      />
    );
  }

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    const e: Record<string, string> = { ...validateGiveLogin(login, t) };
    if (!name.trim()) e.displayName = t('users.create.nameRequired');
    if (!EMAIL_RE.test(email.trim())) e.email = t('users.create.emailInvalid');
    setErrors(e);
    setFormError(null);
    if (Object.keys(e).length) return;
    create.mutate(login);
  };

  const { displayName: nameError, email: emailError, phone: phoneError, ...loginErrors } = errors;

  return (
    <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{t('users.create.title')}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('users.create.description')}</DialogDescription>
      </div>
      <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-4">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        <fieldset disabled={create.isPending} className="grid gap-4 sm:grid-cols-2">
          <legend className="sr-only">{t('users.create.person')}</legend>
          <Field label={t('users.create.name')} error={nameError} className="sm:col-span-2">
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus autoComplete="off" />
          </Field>
          <Field label={t('users.create.email')} error={emailError}>
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" autoCapitalize="none" leading={<AtSign />} />
          </Field>
          <Field label={t('users.create.phone')} error={phoneError}>
            <Input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder={t('users.create.phonePlaceholder')} autoComplete="off" leading={<Phone />} />
          </Field>
        </fieldset>
        <div className="border-t pt-5">
          <GiveLoginFields
            value={login}
            onChange={setLogin}
            personName={name.trim() || t('users.create.person')}
            // The sign-in email is the one typed above; keep the fields usable before it's valid.
            email={email.trim() || '…'}
            errors={loginErrors}
            disabled={create.isPending}
          />
        </div>
      </div>
      <div className="flex justify-end gap-2 border-t px-5 py-3">
        <Button type="button" variant="outline" onClick={onClose} disabled={create.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={create.isPending}>
          {login.method === 'password' ? t('access.login.submitPassword') : t('access.login.submitInvite')}
        </Button>
      </div>
    </form>
  );
}
