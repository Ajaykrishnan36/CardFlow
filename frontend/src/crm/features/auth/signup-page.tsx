import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, MailCheck } from 'lucide-react';
import { authApi, signInApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { PasswordInput } from '@crm/components/ui/password-input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { OtpInput } from '@crm/components/ui/otp-input';
import { Spinner } from '@crm/components/ui/spinner';
import { useAfterAuth } from '@crm/auth/session';
import { AuthHeading, AuthLayout } from './auth-layout';
import { useDocumentTitle } from './login-pages';

/**
 * /crm/signup?product=<code> — self sign-up, only for products whose setup allows it (D-69).
 * /crm/join/<token> — a product's invite link, for people on its company email domains (D-83).
 */
export function SignupPage() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const { token } = useParams();
  const invite = token?.trim() || undefined;
  const link = useQuery({ queryKey: ['auth', 'invite-link', invite], queryFn: () => authApi.inviteLink(invite!), enabled: Boolean(invite), retry: false });
  useDocumentTitle(link.data ? t('auth.signup.joinTitle', { name: link.data.name }) : t('auth.signup.title'));
  const product = invite ? link.data?.product ?? '' : (params.get('product') ?? '').trim().toLowerCase();
  const afterAuth = useAfterAuth();
  const methods = useQuery({ queryKey: ['auth', 'methods', product], queryFn: () => signInApi.methods(product), enabled: Boolean(product) && !invite, retry: false });
  const [step, setStep] = useState<'details' | 'code'>('details');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [devCode, setDevCode] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const loginHref = `/crm/login?product=${encodeURIComponent(product)}`;

  const fail = (e: unknown) => {
    if (isApiError(e)) {
      setFieldErrors(e.fieldErrors);
      setError(Object.keys(e.fieldErrors).length ? null : e.message);
    } else setError(t('common.genericError'));
  };
  const request = async (e?: FormEvent) => {
    e?.preventDefault();
    setError(null);
    setFieldErrors({});
    if (!name.trim() || !email.trim()) return setError(t('auth.signup.fillIn'));
    setBusy(true);
    try {
      const r = await authApi.requestSignup({ product, name: name.trim(), email: email.trim(), invite });
      setDevCode(r.devCode);
      setStep('code');
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };
  const complete = async (e?: FormEvent, value = code) => {
    e?.preventDefault();
    if (value.length !== 6) return;
    setError(null);
    setFieldErrors({});
    setBusy(true);
    try {
      const step = await authApi.completeSignup({ product, name: name.trim(), email: email.trim(), code: value, password: password || undefined, invite });
      await afterAuth(step.next, null);
    } catch (err) {
      fail(err);
      setBusy(false);
    }
  };

  const closed = invite ? link.isError : !product || (methods.data && !methods.data.signup);
  const domains = (link.data?.domains ?? []).map((d) => '@' + d).join(', ');
  return (
    <AuthLayout>
      {(invite && link.isPending) || (!invite && product && methods.isPending) ? (
        <Spinner className="size-5 text-muted-foreground" label={t('common.loading')} />
      ) : closed ? (
        <>
          <AuthHeading
            title={invite ? t('auth.signup.linkInvalidTitle') : t('auth.signup.closedTitle')}
            subtitle={invite ? (isApiError(link.error) ? link.error.message : t('auth.signup.linkInvalidBody')) : t('auth.signup.closedBody')}
          />
          <Button asChild size="lg" className="w-full">
            <Link to={product ? loginHref : '/crm/login'}>{t('auth.signup.goSignIn')}</Link>
          </Button>
        </>
      ) : step === 'details' ? (
        <>
          <AuthHeading
            title={invite ? t('auth.signup.joinTitle', { name: link.data?.name }) : t('auth.signup.title')}
            subtitle={invite ? t('auth.signup.joinSubtitle', { domains }) : t('auth.signup.subtitle')}
          />
          <form onSubmit={request} noValidate className="space-y-5">
            {error ? (
              <Alert tone="danger">{error}</Alert>
            ) : null}
            <Field label={t('auth.signup.name')} error={fieldErrors.name}>
              <Input value={name} onChange={(e) => setName(e.target.value)} inputSize="lg" autoComplete="name" autoFocus maxLength={120} />
            </Field>
            <Field label={t('auth.signup.email')} error={fieldErrors.email}>
              <Input value={email} onChange={(e) => setEmail(e.target.value)} inputSize="lg" type="email" autoComplete="email" placeholder={link.data?.domains[0] ? `you@${link.data.domains[0]}` : 'you@company.com'} />
            </Field>
            <Button type="submit" size="lg" className="w-full" loading={busy}>
              {t('auth.signup.sendCode')} <ArrowRight />
            </Button>
          </form>
          <p className="mt-6 text-center text-[13px] text-muted-foreground">
            {t('auth.signup.haveAccount')}{' '}
            <Link to={loginHref} className="font-medium text-primary hover:underline">
              {t('auth.signup.signIn')}
            </Link>
          </p>
        </>
      ) : (
        <>
          <AuthHeading icon={<MailCheck className="size-8 text-primary" aria-hidden />} title={t('auth.login.codeSentTitle')} subtitle={t('auth.login.codeSentBody', { email })} />
          <form onSubmit={(e) => void complete(e)} noValidate className="space-y-5">
            {devCode ? <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">{t('auth.login.devCode', { code: devCode })}</p> : null}
            {error ? <Alert tone="danger">{error}</Alert> : null}
            <OtpInput label={t('auth.login.codeLabel')} value={code} onChange={setCode} invalid={Boolean(error || fieldErrors.code)} disabled={busy} autoFocus />
            <Field label={t('auth.signup.password')} hint={invite ? t('auth.signup.passwordHintJoin') : t('auth.signup.passwordHint')} error={fieldErrors.password}>
              <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} inputSize="lg" autoComplete="new-password" />
            </Field>
            <Button type="submit" size="lg" className="w-full" loading={busy} disabled={code.length !== 6}>
              {invite ? t('auth.signup.join') : t('auth.signup.create')}
            </Button>
            <Button type="button" variant="link" onClick={() => setStep('details')}>
              {t('auth.signup.changeEmail')}
            </Button>
          </form>
        </>
      )}
    </AuthLayout>
  );
}
