import { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Link, useSearchParams } from 'react-router-dom';
import { ArrowRight, AtSign } from 'lucide-react';
import { authApi } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { PasswordInput } from '@crm/components/ui/password-input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { useAfterAuth } from '@crm/auth/session';
import { safeReturnTo } from '@crm/lib/utils';
import { useFormError } from './use-form-error';
import { EmailCodeForm } from './email-code-form';

interface Values {
  identifier: string;
  password: string;
}

const isDesktop = () => window.matchMedia?.('(min-width: 1024px)').matches ?? false;

export function LoginForm({ audience }: { audience: 'owner' | 'workspace' }) {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  // Magic link from the sign-in email: ?method=code&email=…&code=… opens the code tab.
  const [method, setMethod] = useState<'password' | 'code'>(searchParams.get('method') === 'code' ? 'code' : 'password');
  return (
    <div className="space-y-5">
      <div role="tablist" aria-label={t('auth.login.submit')} className="grid grid-cols-2 rounded-lg border bg-muted/60 p-1">
        {(['password', 'code'] as const).map((m) => (
          <button
            key={m}
            type="button"
            role="tab"
            aria-selected={method === m}
            onClick={() => setMethod(m)}
            className={
              'rounded-md py-1.5 text-[13px] font-medium transition-colors ' +
              (method === m ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')
            }
          >
            {m === 'password' ? t('auth.login.methodPassword') : t('auth.login.methodCode')}
          </button>
        ))}
      </div>
      {method === 'password' ? <PasswordLoginForm audience={audience} /> : <EmailCodeForm audience={audience} />}
    </div>
  );
}

function PasswordLoginForm({ audience }: { audience: 'owner' | 'workspace' }) {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const afterAuth = useAfterAuth();
  const returnTo = safeReturnTo(params.get('returnTo'));

  const schema = useMemo(
    () =>
      z.object({
        identifier: z.string().trim().min(1, t('common.required')),
        password: z.string().min(1, t('common.required'))
      }),
    [t]
  );

  const {
    register,
    handleSubmit,
    setError,
    setFocus,
    formState: { errors, isSubmitting }
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { identifier: '', password: '' } });
  const { alert, setAlert, handle } = useFormError(setError, ['identifier', 'password']);

  useEffect(() => {
    // Autofocus only where it helps: on phones it would pop the keyboard over the page.
    if (isDesktop()) setFocus('identifier');
  }, [setFocus]);

  const notice = params.get('expired')
    ? { tone: 'info' as const, text: t('auth.login.sessionExpired') }
    : params.get('signedOut')
      ? { tone: 'success' as const, text: t('auth.login.signedOut') }
      : params.get('reset')
        ? { tone: 'success' as const, text: t('auth.login.passwordUpdated') }
        : null;

  const onSubmit = handleSubmit(async (values) => {
    setAlert(null);
    try {
      const step = await authApi.login({ identifier: values.identifier.trim(), password: values.password, audience });
      await afterAuth(step.next, returnTo);
    } catch (e) {
      handle(e);
      setFocus('password');
    }
  });

  return (
    <form onSubmit={onSubmit} noValidate className="space-y-5">
      {alert ? (
        <Alert tone="danger">
          {alert.message}
          {alert.requestId ? <span className="mt-1 block font-mono text-[11px]">{t('common.requestId', { id: alert.requestId })}</span> : null}
        </Alert>
      ) : notice ? (
        <Alert tone={notice.tone}>{notice.text}</Alert>
      ) : null}

      <Field label={t('auth.login.identifier')} error={errors.identifier?.message}>
        <Input
          {...register('identifier')}
          inputSize="lg"
          type="text"
          inputMode="email"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          placeholder={t('auth.login.identifierPlaceholder')}
          leading={<AtSign />}
        />
      </Field>

      <Field
        label={t('auth.login.password')}
        error={errors.password?.message}
        labelAside={
          <Link to="/crm/forgot-password" className="text-[13px] font-medium text-primary hover:underline" tabIndex={0}>
            {t('auth.login.forgot')}
          </Link>
        }
      >
        <PasswordInput {...register('password')} inputSize="lg" autoComplete="current-password" placeholder={t('auth.login.passwordPlaceholder')} />
      </Field>

      <Button type="submit" size="lg" className="group w-full" loading={isSubmitting}>
        {isSubmitting ? t('auth.login.submitting') : t('auth.login.submit')}
        {!isSubmitting ? <ArrowRight className="transition-transform group-hover:translate-x-0.5" /> : null}
      </Button>
    </form>
  );
}
