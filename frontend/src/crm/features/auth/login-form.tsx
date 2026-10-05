import { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, AtSign, Building2 } from 'lucide-react';
import { authApi, signInApi } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { PasswordInput } from '@crm/components/ui/password-input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { useAfterAuth } from '@crm/auth/session';
import { safeReturnTo } from '@crm/lib/utils';
import { useFormError } from './use-form-error';
import { EmailCodeForm } from './email-code-form';
import { PhoneCodeForm } from './phone-code-form';

interface Values {
  identifier: string;
  password: string;
}

const isDesktop = () => window.matchMedia?.('(min-width: 1024px)').matches ?? false;

export function LoginForm({ audience }: { audience: 'owner' | 'workspace' }) {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  // ?product=<workspace code> shows only the sign-in methods that product allows (D-64).
  const product = audience === 'workspace' ? (searchParams.get('product') ?? '').trim().toLowerCase() : '';
  const oauthError = searchParams.get('oauthError');
  const methodsQ = useQuery({ queryKey: ['auth', 'methods', product], queryFn: () => signInApi.methods(product || undefined), staleTime: 60_000, retry: false });
  const providersQ = useQuery({ queryKey: ['auth', 'providers'], queryFn: () => signInApi.providers(), staleTime: 5 * 60_000, retry: false });
  const allowed = methodsQ.data?.methods ?? ['password', 'otp'];
  // Customers sign in with their mobile number first (the same one as in the app, D-93);
  // the owner console never uses phone sign-in.
  const tabs = (['phone', 'password', 'code'] as const).filter((m) =>
    m === 'phone' ? audience === 'workspace' && allowed.includes('phone') : allowed.includes(m === 'password' ? 'password' : 'otp')
  );
  // Magic link from the sign-in email: ?method=code&email=…&code=… opens the code tab.
  const asked = searchParams.get('method');
  const [picked, setMethod] = useState<'phone' | 'password' | 'code'>(
    asked === 'code' ? 'code' : asked === 'password' || audience === 'owner' ? 'password' : 'phone'
  );
  const method = tabs.includes(picked) ? picked : tabs[0];
  const providers = (['google', 'microsoft', 'linkedin'] as const).filter((p) => allowed.includes(p) && providersQ.data?.[p] && (audience === 'workspace' || p !== 'linkedin'));
  const sso = audience === 'workspace' && Boolean(product) && allowed.includes('sso');
  const buttons = providers.length > 0 || sso;
  return (
    <div className="space-y-5">
      {oauthError ? <Alert tone="danger">{oauthError}</Alert> : null}
      {tabs.length > 1 ? (
        <div role="tablist" aria-label={t('auth.login.submit')} className={'grid rounded-lg border bg-muted/60 p-1 ' + (tabs.length === 3 ? 'grid-cols-3' : 'grid-cols-2')}>
          {tabs.map((m) => (
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
              {m === 'phone' ? 'Mobile' : m === 'password' ? t('auth.login.methodPassword') : t('auth.login.methodCode')}
            </button>
          ))}
        </div>
      ) : null}
      {method === 'phone' ? <PhoneCodeForm /> : method === 'password' ? <PasswordLoginForm audience={audience} /> : method === 'code' ? <EmailCodeForm audience={audience} /> : null}
      {buttons ? (
        <>
          {tabs.length ? (
            <div className="flex items-center gap-3 text-xs text-muted-foreground" aria-hidden>
              <span className="h-px flex-1 bg-border" />
              {t('auth.login.or')}
              <span className="h-px flex-1 bg-border" />
            </div>
          ) : null}
          <div className="grid gap-2">
            {providers.map((p) => (
              <Button key={p} asChild variant="outline" size="lg" className="w-full">
                <a href={signInApi.oauthStartUrl(p, { product: product || undefined, audience })}>
                  <ProviderMark provider={p} /> {t(`auth.login.continueWith.${p}`)}
                </a>
              </Button>
            ))}
            {sso ? (
              <Button asChild variant="outline" size="lg" className="w-full">
                <a href={signInApi.ssoStartUrl(product)}>
                  <Building2 /> {t('auth.login.sso')}
                </a>
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
      {product && methodsQ.data && !tabs.length && !buttons ? <Alert tone="warning">{t('auth.login.noMethods')}</Alert> : null}
      {product && methodsQ.data?.signup ? (
        <p className="text-center text-[13px] text-muted-foreground">
          {t('auth.login.newHere')}{' '}
          <Link to={`/crm/signup?product=${encodeURIComponent(product)}`} className="font-medium text-primary hover:underline">
            {t('auth.login.createAccount')}
          </Link>
        </p>
      ) : null}
    </div>
  );
}

function ProviderMark({ provider }: { provider: 'google' | 'microsoft' | 'linkedin' }) {
  if (provider === 'google')
    return (
      <svg viewBox="0 0 24 24" aria-hidden className="size-4">
        <path fill="#EA4335" d="M12 10.2v3.9h5.4c-.2 1.3-1.6 3.8-5.4 3.8-3.2 0-5.9-2.7-5.9-6s2.7-6 5.9-6c1.9 0 3.1.8 3.8 1.5l2.6-2.5C16.8 3.4 14.6 2.4 12 2.4 6.7 2.4 2.4 6.7 2.4 12s4.3 9.6 9.6 9.6c5.5 0 9.2-3.9 9.2-9.4 0-.6-.1-1.1-.2-1.6H12z" />
      </svg>
    );
  if (provider === 'microsoft')
    return (
      <svg viewBox="0 0 24 24" aria-hidden className="size-4">
        <path fill="#F25022" d="M3 3h8.5v8.5H3z" />
        <path fill="#7FBA00" d="M12.5 3H21v8.5h-8.5z" />
        <path fill="#00A4EF" d="M3 12.5h8.5V21H3z" />
        <path fill="#FFB900" d="M12.5 12.5H21V21h-8.5z" />
      </svg>
    );
  return (
    <svg viewBox="0 0 24 24" aria-hidden className="size-4">
      <path fill="#0A66C2" d="M20.4 2H3.6C2.7 2 2 2.7 2 3.6v16.8c0 .9.7 1.6 1.6 1.6h16.8c.9 0 1.6-.7 1.6-1.6V3.6c0-.9-.7-1.6-1.6-1.6zM8 19H5V9h3v10zM6.5 7.7a1.7 1.7 0 1 1 0-3.5 1.7 1.7 0 0 1 0 3.5zM19 19h-3v-4.9c0-1.2 0-2.7-1.6-2.7s-1.9 1.3-1.9 2.6v5H9.5V9h2.9v1.4c.4-.8 1.4-1.6 2.9-1.6 3.1 0 3.7 2 3.7 4.7V19z" />
    </svg>
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
