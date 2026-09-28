import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { ArrowLeft, ArrowRight, AtSign, Clock, MailCheck, RotateCw } from 'lucide-react';
import { authApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { OtpInput } from '@crm/components/ui/otp-input';
import { useAfterAuth } from '@crm/auth/session';
import { cn, safeReturnTo } from '@crm/lib/utils';
import { useShake } from './use-shake';

const RESEND_SECONDS = 120;

const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

/** Seconds left until `until` (epoch ms), ticking every second. */
function useCountdown(until: number | null) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!until) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [until]);
  return until ? Math.max(0, Math.ceil((until - now) / 1000)) : 0;
}

/** Passwordless sign-in: email → 6-digit code (PRD AUTH-02). MFA still applies after it. */
export function EmailCodeForm({ audience }: { audience: 'owner' | 'workspace' }) {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const afterAuth = useAfterAuth();
  const returnTo = safeReturnTo(params.get('returnTo'));

  const linkEmail = params.get('email') ?? '';
  const linkCode = (params.get('code') ?? '').replace(/\D/g, '').slice(0, 6);
  const fromLink = Boolean(linkEmail && linkCode.length === 6);

  const [email, setEmail] = useState(linkEmail);
  const [step, setStep] = useState<'email' | 'code'>(fromLink ? 'code' : 'email');
  const [code, setCode] = useState(linkCode);
  const [devCode, setDevCode] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [emailShakeRef, shakeEmail] = useShake();
  const [codeShakeRef, shakeCode] = useShake();
  const [resendAt, setResendAt] = useState<number | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const resendIn = useCountdown(resendAt);
  const expiresIn = useCountdown(expiresAt);
  const expired = expiresAt !== null && expiresIn === 0;

  const fail = (message: string, onField = false) => {
    if (onField) {
      setFieldError(message);
      setError(null);
    } else {
      setError(message);
      setFieldError(null);
    }
    requestAnimationFrame(onField ? shakeEmail : shakeCode);
  };

  const explain = (e: unknown): { message: string; field: boolean } => {
    if (!isApiError(e)) return { message: t('common.genericError'), field: false };
    if (e.status === 429 && e.retryAfter) {
      return {
        message: t('common.tooManyAttempts', {
          count: Math.max(1, Math.ceil(e.retryAfter / 60))
        }),
        field: false
      };
    }
    if (e.fieldErrors.identifier) return { message: e.fieldErrors.identifier, field: true };
    return { message: e.fieldErrors.code ?? e.message, field: false };
  };

  const send = async () => {
    setError(null);
    setFieldError(null);
    const value = email.trim();
    if (!value) {
      fail(t('common.required'), true);
      return;
    }
    setBusy(true);
    try {
      const res = await authApi.requestOtp({ identifier: value, audience });
      setDevCode(res.devCode);
      setStep('code');
      setCode('');
      setResendAt(Date.now() + RESEND_SECONDS * 1000);
      setExpiresAt(Date.now() + res.expiresIn * 1000);
      toast.success(t('auth.login.codeSentToast', { email: value }));
    } catch (e) {
      const { message, field } = explain(e);
      if (step === 'code' && field) {
        setStep('email'); // the address itself is the problem: go back to it
      }
      fail(message, field);
      toast.error(message);
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value = code) => {
    setError(null);
    if (value.length !== 6) {
      fail(t('auth.login.codeLength'));
      return;
    }
    if (expired) {
      fail(t('auth.login.codeExpired'));
      return;
    }
    setBusy(true);
    try {
      const next = await authApi.verifyOtp({
        identifier: email.trim(),
        code: value,
        audience
      });
      toast.success(t('auth.login.signedInToast'));
      await afterAuth(next.next, returnTo);
    } catch (e) {
      const { message } = explain(e);
      fail(message);
      toast.error(message);
      setCode('');
      setBusy(false);
    }
  };

  // Opened from the magic link in the email: sign in straight away (once).
  const [autoTried, setAutoTried] = useState(false);
  useEffect(() => {
    if (!fromLink || autoTried) return;
    setAutoTried(true);
    void verify(linkCode);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fromLink, autoTried]);

  if (step === 'email') {
    return (
      <form
        key="email"
        noValidate
        className="space-y-5 animate-pop-in"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <div ref={emailShakeRef}>
          <Field label={t('auth.login.emailLabel')} error={fieldError ?? undefined}>
            <Input
              value={email}
              onChange={(e) => {
                setEmail(e.target.value);
                if (fieldError) setFieldError(null);
              }}
              inputSize="lg"
              type="email"
              inputMode="email"
              autoComplete="email"
              autoCapitalize="none"
              spellCheck={false}
              autoFocus
              placeholder={t('auth.login.identifierPlaceholder')}
              leading={<AtSign />}
            />
          </Field>
        </div>
        <Button type="submit" size="lg" className="group w-full" loading={busy}>
          {busy ? t('auth.login.sendingCode') : t('auth.login.sendCode')}
          {!busy ? <ArrowRight className="transition-transform group-hover:translate-x-0.5" /> : null}
        </Button>
      </form>
    );
  }

  return (
    <form
      key="code"
      noValidate
      className="space-y-5 animate-pop-in"
      onSubmit={(e) => {
        e.preventDefault();
        void verify();
      }}
    >
      <Alert tone="success" title={t('auth.login.codeSentTitle')}>
        <span className="flex items-start gap-2">
          <MailCheck className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
          {t('auth.login.codeSentBody', { email: email.trim() })}
        </span>
      </Alert>
      {devCode ? (
        <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">
          {t('auth.login.devCode', { code: devCode })}
        </p>
      ) : null}
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div ref={codeShakeRef}>
        <OtpInput
          label={t('auth.login.codeLabel')}
          value={code}
          onChange={setCode}
          onComplete={(v) => void verify(v)}
          invalid={Boolean(error)}
          disabled={busy || expired}
          autoFocus
        />
      </div>
      {expiresAt ? (
        <p
          className={cn('flex items-center gap-1.5 text-[13px]', expired ? 'text-danger' : 'text-muted-foreground')}
          aria-live="polite"
        >
          <Clock className="size-3.5" aria-hidden />
          {expired ? t('auth.login.codeExpired') : t('auth.login.codeExpiresIn', { time: mmss(expiresIn) })}
        </p>
      ) : null}
      <Button type="submit" size="lg" className="w-full" loading={busy} disabled={expired}>
        {busy ? t('auth.login.verifyingCode') : t('auth.login.verifyCode')}
      </Button>
      <div className="flex items-center justify-between text-[13px]">
        <button
          type="button"
          className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
          onClick={() => {
            setStep('email');
            setError(null);
            setExpiresAt(null);
          }}
        >
          <ArrowLeft className="size-3.5" aria-hidden /> {t('auth.login.changeEmail')}
        </button>
        <button
          type="button"
          className="flex items-center gap-1.5 font-medium text-primary hover:underline disabled:cursor-not-allowed disabled:text-muted-foreground disabled:no-underline"
          disabled={resendIn > 0 || busy}
          onClick={() => void send()}
        >
          <RotateCw className={cn('size-3.5', busy && 'animate-spin')} aria-hidden />
          {resendIn > 0 ? t('auth.login.resendIn', { time: mmss(resendIn) }) : t('auth.login.resend')}
        </button>
      </div>
    </form>
  );
}
