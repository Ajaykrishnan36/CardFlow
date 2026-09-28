import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Check, Copy, Download, KeyRound, ShieldCheck, Smartphone } from 'lucide-react';
import { authApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { MfaConfirmation, MfaEnrollment } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { OtpInput } from '@crm/components/ui/otp-input';
import { Alert } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { homeFor, useAfterAuth, useMe, useSignOut } from '@crm/auth/session';
import { safeReturnTo } from '@crm/lib/utils';
import { AuthHeading, AuthLayout } from './auth-layout';
import { useDocumentTitle } from './login-pages';

function IconBadge({ children }: { children: React.ReactNode }) {
  return <div className="grid size-12 place-items-center rounded-xl bg-primary-soft text-primary [&_svg]:size-6">{children}</div>;
}

// ---------------------------------------------------------------- verify
export function MfaVerifyPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.mfa.verifyTitle'));
  const { data: me } = useMe();
  const [params] = useSearchParams();
  const returnTo = safeReturnTo(params.get('returnTo'));
  const afterAuth = useAfterAuth();
  const signOut = useSignOut();

  const [mode, setMode] = useState<'code' | 'recovery'>('code');
  const [code, setCode] = useState('');
  const [recovery, setRecovery] = useState('');
  const [fieldError, setFieldError] = useState<string>();
  const [alert, setAlert] = useState<string>();
  const [locked, setLocked] = useState(false);
  const [busy, setBusy] = useState(false);

  const submit = async (value: string) => {
    if (busy) return;
    if (mode === 'code' && value.length !== 6) {
      setFieldError(t('auth.mfa.codeLength'));
      return;
    }
    setBusy(true);
    setFieldError(undefined);
    setAlert(undefined);
    try {
      const res = await authApi.verifyMfa(value);
      await afterAuth(res.next, returnTo);
    } catch (e) {
      if (isApiError(e) && e.status === 422) {
        setFieldError(e.fieldErrors.code ?? e.message);
        setCode('');
      } else if (isApiError(e) && (e.status === 429 || e.status === 401)) {
        setLocked(true);
        setAlert(e.message);
      } else {
        setAlert(isApiError(e) ? e.message : t('common.genericError'));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <Smartphone />
          </IconBadge>
        }
        title={t('auth.mfa.verifyTitle')}
        subtitle={mode === 'code' ? t('auth.mfa.verifySubtitle') : t('auth.mfa.recoveryHint')}
      />
      {me?.identity.email ? (
        <p className="-mt-4 mb-6 text-[13px] text-muted-foreground">{t('auth.mfa.signingInAs', { who: me.identity.email })}</p>
      ) : null}

      <form
        noValidate
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault();
          void submit(mode === 'code' ? code : recovery.trim());
        }}
      >
        {alert ? <Alert tone="danger">{alert}</Alert> : null}

        {mode === 'code' ? (
          <Field label={t('auth.mfa.code')} error={fieldError}>
            <OtpInput label={t('auth.mfa.code')} value={code} onChange={setCode} onComplete={(v) => void submit(v)} autoFocus disabled={busy || locked} />
          </Field>
        ) : (
          <Field label={t('auth.mfa.recoveryLabel')} error={fieldError}>
            <Input
              value={recovery}
              onChange={(e) => setRecovery(e.target.value)}
              inputSize="lg"
              autoFocus
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              placeholder={t('auth.mfa.recoveryPlaceholder')}
              className="font-mono"
              leading={<KeyRound />}
              disabled={busy || locked}
            />
          </Field>
        )}

        {locked ? (
          <Button type="button" size="lg" className="w-full" onClick={() => void signOut()}>
            {t('auth.forgot.back')}
          </Button>
        ) : (
          <Button type="submit" size="lg" className="w-full" loading={busy}>
            {busy ? t('auth.mfa.verifying') : t('auth.mfa.verify')}
          </Button>
        )}
      </form>

      <div className="mt-6 flex items-center justify-between text-[13px]">
        <button
          type="button"
          className="font-medium text-primary hover:underline"
          onClick={() => {
            setMode((m) => (m === 'code' ? 'recovery' : 'code'));
            setFieldError(undefined);
          }}
        >
          {mode === 'code' ? t('auth.mfa.useRecovery') : t('auth.mfa.useCode')}
        </button>
        <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => void signOut()}>
          {t('common.signOut')}
        </button>
      </div>
    </AuthLayout>
  );
}

// ---------------------------------------------------------------- setup
function useCopy() {
  const [copied, setCopied] = useState<string | null>(null);
  const copy = async (key: string, text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      window.setTimeout(() => setCopied((c) => (c === key ? null : c)), 1800);
    } catch {
      /* clipboard blocked — user can still select the text */
    }
  };
  return { copied, copy };
}

export function MfaSetupPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.mfa.setupTitle'));
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const returnTo = safeReturnTo(params.get('returnTo'));
  const { data: me } = useMe();
  const afterAuth = useAfterAuth();
  const signOut = useSignOut();
  const { copied, copy } = useCopy();

  const required = Boolean(me?.session.mfaRequired && !me.session.mfaPassed);
  const [enrollment, setEnrollment] = useState<MfaEnrollment | null>(null);
  const [confirmation, setConfirmation] = useState<MfaConfirmation | null>(null);
  const [code, setCode] = useState('');
  const [fieldError, setFieldError] = useState<string>();
  const [alert, setAlert] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return; // one secret per visit (StrictMode double-mount safe)
    started.current = true;
    authApi
      .enrollMfa()
      .then(setEnrollment)
      .catch((e) => {
        if (isApiError(e) && e.code === 'mfa_already_enabled') navigate(me ? homeFor(me) : '/crm', { replace: true });
        else setAlert(isApiError(e) ? e.message : t('common.genericError'));
      });
  }, [navigate, me, t]);

  const confirm = async (value: string) => {
    if (busy) return;
    if (value.length !== 6) {
      setFieldError(t('auth.mfa.codeLength'));
      return;
    }
    setBusy(true);
    setFieldError(undefined);
    try {
      setConfirmation(await authApi.confirmMfa(value));
    } catch (e) {
      if (isApiError(e) && e.status === 422) {
        setFieldError(e.fieldErrors.code ?? e.message);
        setCode('');
      } else setAlert(isApiError(e) ? e.message : t('common.genericError'));
    } finally {
      setBusy(false);
    }
  };

  const downloadCodes = (codes: string[]) => {
    const text = `${t('auth.mfa.recoveryFileHeader', { date: new Date().toLocaleString() })}\n\n${codes.join('\n')}\n`;
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'crm-recovery-codes.txt';
    a.click();
    URL.revokeObjectURL(url);
  };

  if (confirmation) {
    return (
      <AuthLayout>
        <AuthHeading
          icon={
            <IconBadge>
              <ShieldCheck />
            </IconBadge>
          }
          title={t('auth.mfa.recoveryTitle')}
          subtitle={t('auth.mfa.recoveryBody')}
        />
        <ul className="grid grid-cols-2 gap-2 rounded-xl border bg-muted/40 p-4 font-mono text-[15px] tracking-wide" aria-label={t('auth.mfa.recoveryTitle')}>
          {confirmation.recoveryCodes.map((c) => (
            <li key={c} className="rounded-md bg-background px-3 py-2 text-center shadow-sm">
              {c}
            </li>
          ))}
        </ul>
        <div className="mt-3 flex gap-2">
          <Button variant="outline" size="sm" onClick={() => downloadCodes(confirmation.recoveryCodes)}>
            <Download /> {t('auth.mfa.download')}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void copy('codes', confirmation.recoveryCodes.join('\n'))}>
            {copied === 'codes' ? <Check /> : <Copy />} {copied === 'codes' ? t('common.copied') : t('auth.mfa.copyAll')}
          </Button>
        </div>
        <label className="mt-6 flex cursor-pointer items-start gap-3 text-sm">
          <input type="checkbox" className="mt-0.5 size-4 accent-[hsl(var(--primary))]" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
          <span>{t('auth.mfa.savedCheck')}</span>
        </label>
        <Button size="lg" className="mt-5 w-full" disabled={!saved} onClick={() => void afterAuth(confirmation.next, returnTo)}>
          {t('common.continue')}
        </Button>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <ShieldCheck />
          </IconBadge>
        }
        title={t('auth.mfa.setupTitle')}
        subtitle={required ? t('auth.mfa.setupRequired') : t('auth.mfa.setupOptional')}
      />
      {alert ? <Alert tone="danger" className="mb-5">{alert}</Alert> : null}

      <ol className="space-y-6">
        <li className="flex gap-4">
          <StepNumber n={1} />
          <div className="min-w-0 flex-1">
            <p className="font-semibold text-foreground">{t('auth.mfa.step1')}</p>
            <p className="mt-0.5 text-[13px] text-muted-foreground">{t('auth.mfa.step1Body')}</p>
            <div className="mt-3 flex flex-col items-start gap-3 sm:flex-row sm:items-center">
              <div className="grid size-[168px] shrink-0 place-items-center rounded-xl border bg-white p-2 shadow-sm">
                {enrollment ? <img src={enrollment.qrDataUrl} alt={t('auth.mfa.qrAlt')} className="size-full" /> : <Skeleton className="size-full" />}
              </div>
              <div className="min-w-0 text-[13px]">
                <p className="text-muted-foreground">{t('auth.mfa.manualKey')}</p>
                {enrollment ? (
                  <button
                    type="button"
                    onClick={() => void copy('secret', enrollment.secret)}
                    className="mt-1.5 inline-flex max-w-full items-center gap-2 rounded-md border bg-muted/50 px-2.5 py-1.5 font-mono text-xs text-foreground hover:bg-muted"
                  >
                    <span className="truncate">{enrollment.secret.replace(/(.{4})/g, '$1 ').trim()}</span>
                    {copied === 'secret' ? <Check className="size-3.5 shrink-0 text-success" /> : <Copy className="size-3.5 shrink-0" />}
                  </button>
                ) : (
                  <Skeleton className="mt-1.5 h-7 w-56" />
                )}
              </div>
            </div>
          </div>
        </li>
        <li className="flex gap-4">
          <StepNumber n={2} />
          <form
            className="min-w-0 flex-1"
            noValidate
            onSubmit={(e) => {
              e.preventDefault();
              void confirm(code);
            }}
          >
            <p className="font-semibold text-foreground">{t('auth.mfa.step2')}</p>
            <p className="mb-3 mt-0.5 text-[13px] text-muted-foreground">{t('auth.mfa.step2Body')}</p>
            <Field label={t('auth.mfa.code')} error={fieldError}>
              <OtpInput label={t('auth.mfa.code')} value={code} onChange={setCode} onComplete={(v) => void confirm(v)} disabled={!enrollment || busy} />
            </Field>
            <Button type="submit" size="lg" className="mt-5 w-full" loading={busy} disabled={!enrollment}>
              {busy ? t('auth.mfa.confirming') : t('auth.mfa.confirm')}
            </Button>
          </form>
        </li>
      </ol>

      <div className="mt-6 text-center text-[13px]">
        {required ? (
          <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => void signOut()}>
            {t('common.signOut')}
          </button>
        ) : (
          <Link to="/crm/me" className="text-muted-foreground hover:text-foreground">
            {t('auth.mfa.notNow')}
          </Link>
        )}
      </div>
    </AuthLayout>
  );
}

function StepNumber({ n }: { n: number }) {
  return (
    <span className="grid size-7 shrink-0 place-items-center rounded-full bg-primary text-[13px] font-semibold text-primary-foreground shadow-sm shadow-primary/30">
      {n}
    </span>
  );
}
