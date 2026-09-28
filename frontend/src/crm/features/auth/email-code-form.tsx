import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { ArrowLeft, ArrowRight, AtSign, MailCheck } from 'lucide-react';
import { authApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { OtpInput } from '@crm/components/ui/otp-input';
import { useAfterAuth } from '@crm/auth/session';
import { safeReturnTo } from '@crm/lib/utils';

const RESEND_SECONDS = 30;

function errorMessage(e: unknown, fallback: string, tooMany: (minutes: number) => string) {
  if (!isApiError(e)) return fallback;
  if (e.status === 429 && e.retryAfter) return tooMany(Math.max(1, Math.ceil(e.retryAfter / 60)));
  return e.fieldErrors.identifier ?? e.fieldErrors.code ?? e.message;
}

/** Passwordless sign-in: email → 6-digit code (PRD AUTH-02). MFA still applies after it. */
export function EmailCodeForm({ audience }: { audience: 'owner' | 'workspace' }) {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const afterAuth = useAfterAuth();
  const returnTo = safeReturnTo(params.get('returnTo'));

  const linkEmail = params.get('email') ?? '';
  const linkCode = (params.get('code') ?? '').replace(/\D/g, '').slice(0, 6);
  const [email, setEmail] = useState(linkEmail);
  const [step, setStep] = useState<'email' | 'code'>(linkEmail && linkCode.length === 6 ? 'code' : 'email');
  const [code, setCode] = useState(linkCode);
  const [devCode, setDevCode] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [cooldown, setCooldown] = useState(0);

  useEffect(() => {
    if (cooldown <= 0) return;
    const id = setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => clearTimeout(id);
  }, [cooldown]);

  const tooMany = (count: number) => t('common.tooManyAttempts', { count });

  // Opened from the magic link: sign in straight away (once).
  const [autoTried, setAutoTried] = useState(false);
  useEffect(() => {
    if (autoTried || step !== 'code' || linkCode.length !== 6) return;
    setAutoTried(true);
    void verify(linkCode);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoTried, step]);

  const send = async () => {
    setError(null);
    if (!email.trim()) {
      setError(t('common.required'));
      return;
    }
    setBusy(true);
    try {
      const res = await authApi.requestOtp({ identifier: email.trim(), audience });
      setDevCode(res.devCode);
      setStep('code');
      setCode('');
      setCooldown(RESEND_SECONDS);
    } catch (e) {
      setError(errorMessage(e, t('common.genericError'), tooMany));
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value = code) => {
    setError(null);
    if (value.length !== 6) {
      setError(t('auth.login.codeLength'));
      return;
    }
    setBusy(true);
    try {
      const next = await authApi.verifyOtp({ identifier: email.trim(), code: value, audience });
      await afterAuth(next.next, returnTo);
    } catch (e) {
      setError(errorMessage(e, t('common.genericError'), tooMany));
      setCode('');
      setBusy(false);
    }
  };

  if (step === 'email') {
    return (
      <form
        noValidate
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <Field label={t('auth.login.emailLabel')}>
          <Input
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            inputSize="lg"
            type="email"
            inputMode="email"
            autoComplete="email"
            autoCapitalize="none"
            spellCheck={false}
            placeholder={t('auth.login.identifierPlaceholder')}
            leading={<AtSign />}
          />
        </Field>
        <Button type="submit" size="lg" className="group w-full" loading={busy}>
          {busy ? t('auth.login.sendingCode') : t('auth.login.sendCode')}
          {!busy ? <ArrowRight className="transition-transform group-hover:translate-x-0.5" /> : null}
        </Button>
      </form>
    );
  }

  return (
    <form
      noValidate
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault();
        void verify();
      }}
    >
      <Alert tone="info" title={t('auth.login.codeSentTitle')}>
        <span className="flex items-start gap-2">
          <MailCheck className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden />
          {t('auth.login.codeSentBody', { email: email.trim() })}
        </span>
      </Alert>
      {devCode ? (
        <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">
          {t('auth.login.devCode', { code: devCode })}
        </p>
      ) : null}
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <OtpInput
        label={t('auth.login.codeLabel')}
        value={code}
        onChange={setCode}
        onComplete={(v) => void verify(v)}
        invalid={Boolean(error)}
        disabled={busy}
        autoFocus
      />
      <Button type="submit" size="lg" className="w-full" loading={busy}>
        {busy ? t('auth.login.verifyingCode') : t('auth.login.verifyCode')}
      </Button>
      <div className="flex items-center justify-between text-[13px]">
        <button
          type="button"
          className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
          onClick={() => {
            setStep('email');
            setError(null);
          }}
        >
          <ArrowLeft className="size-3.5" aria-hidden /> {t('auth.login.changeEmail')}
        </button>
        <button
          type="button"
          className="font-medium text-primary hover:underline disabled:cursor-not-allowed disabled:text-muted-foreground disabled:no-underline"
          disabled={cooldown > 0 || busy}
          onClick={() => void send()}
        >
          {cooldown > 0 ? t('auth.login.resendIn', { seconds: cooldown }) : t('auth.login.resend')}
        </button>
      </div>
    </form>
  );
}
