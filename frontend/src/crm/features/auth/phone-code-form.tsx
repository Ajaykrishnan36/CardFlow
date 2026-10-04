import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { ArrowLeft, ArrowRight, Clock, MessageSquareText, Phone, RotateCw } from 'lucide-react';
import { phoneAuthApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { OtpInput } from '@crm/components/ui/otp-input';
import { useAfterAuth } from '@crm/auth/session';
import { cn, safeReturnTo } from '@crm/lib/utils';
import { useShake } from './use-shake';

const RESEND_SECONDS = 60;
const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

function useCountdown(until: number | null) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!until) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [until]);
  return until ? Math.max(0, Math.ceil((until - now) / 1000)) : 0;
}

/**
 * Sign in with a mobile number and a code sent by text (D-93). The same number signs in
 * to the mobile app and to the CRM. A number that isn't known yet creates the account;
 * the person is then asked to create their business.
 */
export function PhoneCodeForm() {
  const [params] = useSearchParams();
  const afterAuth = useAfterAuth();
  const returnTo = safeReturnTo(params.get('returnTo'));
  const [phone, setPhone] = useState('');
  const [step, setStep] = useState<'phone' | 'code'>('phone');
  const [code, setCode] = useState('');
  const [devCode, setDevCode] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [phoneShakeRef, shakePhone] = useShake();
  const [codeShakeRef, shakeCode] = useShake();
  const [resendAt, setResendAt] = useState<number | null>(null);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const resendIn = useCountdown(resendAt);
  const expiresIn = useCountdown(expiresAt);
  const expired = expiresAt !== null && expiresIn === 0;

  const explain = (e: unknown): { message: string; field: boolean } => {
    if (!isApiError(e)) return { message: 'Something went wrong. Please try again.', field: false };
    if (e.status === 429) {
      const minutes = Math.max(1, Math.ceil((e.retryAfter ?? 60) / 60));
      return { message: `Too many attempts. Try again in ${minutes} minute${minutes === 1 ? '' : 's'}.`, field: false };
    }
    if (e.fieldErrors.phone) return { message: e.fieldErrors.phone, field: true };
    return { message: e.fieldErrors.code ?? e.message, field: false };
  };

  const fail = (message: string, onField: boolean) => {
    setFieldError(onField ? message : null);
    setError(onField ? null : message);
    requestAnimationFrame(onField ? shakePhone : shakeCode);
  };

  const send = async () => {
    setError(null);
    setFieldError(null);
    const value = phone.trim();
    if (value.replace(/\D/g, '').length < 10) {
      fail('Enter your 10-digit mobile number.', true);
      return;
    }
    setBusy(true);
    try {
      const res = await phoneAuthApi.request(value);
      setDevCode(res.devCode);
      setStep('code');
      setCode('');
      setResendAt(Date.now() + RESEND_SECONDS * 1000);
      setExpiresAt(Date.now() + res.expiresIn * 1000);
      toast.success('Code sent');
    } catch (e) {
      const { message, field } = explain(e);
      if (step === 'code' && field) setStep('phone');
      fail(message, field);
    } finally {
      setBusy(false);
    }
  };

  const verify = async (value = code) => {
    setError(null);
    if (value.length !== 6) {
      fail('Enter all 6 digits.', false);
      return;
    }
    setBusy(true);
    try {
      const step = await phoneAuthApi.verify({ phone: phone.trim(), code: value });
      toast.success('Signed in');
      // No business yet: the next screen is "create your business".
      await afterAuth(step.hasBusiness ? step.next : '/crm/businesses?new=1', step.hasBusiness ? returnTo : null);
    } catch (e) {
      fail(explain(e).message, false);
      setCode('');
      setBusy(false);
    }
  };

  if (step === 'phone') {
    return (
      <form
        key="phone"
        noValidate
        className="space-y-5 animate-pop-in"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        {error ? <Alert tone="danger">{error}</Alert> : null}
        <div ref={phoneShakeRef}>
          <Field label="Mobile number" error={fieldError ?? undefined} hint="Use the same number as in the mobile app. New here? This creates your account.">
            <Input
              value={phone}
              onChange={(e) => {
                setPhone(e.target.value);
                if (fieldError) setFieldError(null);
              }}
              inputSize="lg"
              type="tel"
              inputMode="tel"
              autoComplete="tel"
              autoFocus
              placeholder="98765 43210"
              leading={<Phone />}
            />
          </Field>
        </div>
        <Button type="submit" size="lg" className="group w-full" loading={busy}>
          {busy ? 'Sending…' : 'Text me a code'}
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
      <Alert tone="success" title="Check your messages">
        <span className="flex items-start gap-2">
          <MessageSquareText className="mt-0.5 size-4 shrink-0 text-success" aria-hidden />
          We sent a 6-digit code to {phone.trim()}.
        </span>
      </Alert>
      {devCode ? (
        <p className="rounded-md border border-dashed border-warning/50 bg-warning-soft px-3 py-2 text-[13px] font-medium">
          No SMS provider is set up on this server — your code is {devCode}
        </p>
      ) : null}
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div ref={codeShakeRef}>
        <OtpInput label="Sign-in code" value={code} onChange={setCode} onComplete={(v) => void verify(v)} invalid={Boolean(error)} disabled={busy || expired} autoFocus />
      </div>
      {expiresAt ? (
        <p className={cn('flex items-center gap-1.5 text-[13px]', expired ? 'text-danger' : 'text-muted-foreground')} aria-live="polite">
          <Clock className="size-3.5" aria-hidden />
          {expired ? 'This code has expired. Send a new one.' : `Code expires in ${mmss(expiresIn)}`}
        </p>
      ) : null}
      <Button type="submit" size="lg" className="w-full" loading={busy} disabled={expired}>
        {busy ? 'Signing in…' : 'Sign in'}
      </Button>
      <div className="flex items-center justify-between text-[13px]">
        <button
          type="button"
          className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
          onClick={() => {
            setStep('phone');
            setError(null);
            setExpiresAt(null);
          }}
        >
          <ArrowLeft className="size-3.5" aria-hidden /> Use a different number
        </button>
        <button
          type="button"
          className="flex items-center gap-1.5 font-medium text-primary hover:underline disabled:cursor-not-allowed disabled:text-muted-foreground disabled:no-underline"
          disabled={resendIn > 0 || busy}
          onClick={() => void send()}
        >
          <RotateCw className={cn('size-3.5', busy && 'animate-spin')} aria-hidden />
          {resendIn > 0 ? `Resend in ${mmss(resendIn)}` : 'Resend code'}
        </button>
      </div>
    </form>
  );
}
