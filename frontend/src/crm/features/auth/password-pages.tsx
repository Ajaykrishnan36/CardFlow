import { useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { ArrowLeft, AtSign, KeyRound, MailCheck, LockKeyhole } from 'lucide-react';
import { toast } from 'sonner';
import { authApi } from '@crm/api/endpoints';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { PasswordInput, StrengthMeter } from '@crm/components/ui/password-input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { useAfterAuth, useMe, useSignOut } from '@crm/auth/session';
import { AuthHeading, AuthLayout } from './auth-layout';
import { useDocumentTitle } from './login-pages';
import { useFormError } from './use-form-error';
import { useShake } from './use-shake';

function BackToSignIn() {
  const { t } = useTranslation();
  return (
    <Link to="/crm/login" className="mt-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-muted-foreground hover:text-foreground">
      <ArrowLeft className="size-3.5" aria-hidden /> {t('auth.forgot.back')}
    </Link>
  );
}

function IconBadge({ children }: { children: React.ReactNode }) {
  return <div className="grid size-12 place-items-center rounded-xl bg-primary-soft text-primary [&_svg]:size-6">{children}</div>;
}

// ---------------------------------------------------------------- forgot
export function ForgotPasswordPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.forgot.title'));
  const [sent, setSent] = useState<string | null>(null);
  const [shakeRef, shake] = useShake();
  const schema = useMemo(() => z.object({ identifier: z.string().trim().min(1, t('common.required')) }), [t]);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting }
  } = useForm<{ identifier: string }>({ resolver: zodResolver(schema), defaultValues: { identifier: '' } });
  const { alert, setAlert, handle } = useFormError(setError, ['identifier']);

  const onSubmit = handleSubmit(async ({ identifier }) => {
    setAlert(null);
    try {
      const res = await authApi.forgotPassword(identifier.trim());
      setSent(res.message);
      toast.success(t('auth.forgot.sentToast'));
    } catch (e) {
      handle(e);
      shake();
    }
  });

  if (sent) {
    return (
      <AuthLayout>
        <AuthHeading
          icon={
            <IconBadge>
              <MailCheck />
            </IconBadge>
          }
          title={t('auth.forgot.sentTitle')}
          subtitle={sent}
        />
        <BackToSignIn />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <KeyRound />
          </IconBadge>
        }
        title={t('auth.forgot.title')}
        subtitle={t('auth.forgot.subtitle')}
      />
      <form onSubmit={onSubmit} noValidate className="space-y-5">
        {alert ? <Alert tone="danger">{alert.message}</Alert> : null}
        <div ref={shakeRef}>
          <Field label={t('auth.forgot.identifier')} error={errors.identifier?.message}>
            <Input
              {...register('identifier')}
              inputSize="lg"
              type="email"
              autoComplete="username"
              autoCapitalize="none"
              autoFocus
              placeholder={t('auth.login.identifierPlaceholder')}
              leading={<AtSign />}
            />
          </Field>
        </div>
        <Button type="submit" size="lg" className="w-full" loading={isSubmitting}>
          {isSubmitting ? t('auth.forgot.submitting') : t('auth.forgot.submit')}
        </Button>
      </form>
      <BackToSignIn />
    </AuthLayout>
  );
}

// ---------------------------------------------------------------- reset
interface NewPasswordValues {
  password: string;
  confirm: string;
}

function useNewPasswordSchema() {
  const { t } = useTranslation();
  return useMemo(
    () =>
      z
        .object({
          password: z.string().min(8, t('auth.reset.subtitle')).max(128),
          confirm: z.string().min(1, t('common.required'))
        })
        .refine((v) => v.password === v.confirm, { path: ['confirm'], message: t('auth.reset.mismatch') }),
    [t]
  );
}

export function ResetPasswordPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.reset.title'));
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const token = params.get('token') ?? '';
  const schema = useNewPasswordSchema();
  const {
    register,
    handleSubmit,
    setError,
    watch,
    formState: { errors, isSubmitting }
  } = useForm<NewPasswordValues>({ resolver: zodResolver(schema), defaultValues: { password: '', confirm: '' } });
  const { alert, setAlert, handle } = useFormError(setError, ['password']);

  const onSubmit = handleSubmit(async ({ password }) => {
    setAlert(null);
    try {
      await authApi.resetPassword(token, password);
      navigate('/crm/login?reset=1', { replace: true });
    } catch (e) {
      handle(e);
    }
  });

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <LockKeyhole />
          </IconBadge>
        }
        title={t('auth.reset.title')}
        subtitle={t('auth.reset.subtitle')}
      />
      {!token ? (
        <>
          <Alert tone="warning">{t('auth.reset.missingToken')}</Alert>
          <Button asChild variant="outline" className="mt-5 w-full" size="lg">
            <Link to="/crm/forgot-password">{t('auth.reset.requestNew')}</Link>
          </Button>
        </>
      ) : (
        <form onSubmit={onSubmit} noValidate className="space-y-5">
          {alert ? (
            <Alert tone="danger">
              {alert.message} <Link to="/crm/forgot-password">{t('auth.reset.requestNew')}</Link>
            </Alert>
          ) : null}
          <Field label={t('auth.reset.password')} error={errors.password?.message} hint={<StrengthMeter password={watch('password')} />}>
            <PasswordInput {...register('password')} inputSize="lg" autoComplete="new-password" autoFocus />
          </Field>
          <Field label={t('auth.reset.confirm')} error={errors.confirm?.message}>
            <PasswordInput {...register('confirm')} inputSize="lg" autoComplete="new-password" />
          </Field>
          <Button type="submit" size="lg" className="w-full" loading={isSubmitting}>
            {isSubmitting ? t('auth.reset.submitting') : t('auth.reset.submit')}
          </Button>
        </form>
      )}
      <BackToSignIn />
    </AuthLayout>
  );
}

// ---------------------------------------------------------------- change
interface ChangeValues {
  current: string;
  password: string;
  confirm: string;
}

export function ChangePasswordPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.change.title'));
  const { data: me } = useMe();
  const navigate = useNavigate();
  const afterAuth = useAfterAuth();
  const signOut = useSignOut();
  const forced = Boolean(me?.session.mustChangePassword);

  const schema = useMemo(
    () =>
      z
        .object({
          current: z.string().min(1, t('common.required')),
          password: z.string().min(8, t('auth.reset.subtitle')).max(128),
          confirm: z.string().min(1, t('common.required'))
        })
        .refine((v) => v.password === v.confirm, { path: ['confirm'], message: t('auth.reset.mismatch') }),
    [t]
  );
  const {
    register,
    handleSubmit,
    setError,
    watch,
    formState: { errors, isSubmitting }
  } = useForm<ChangeValues>({ resolver: zodResolver(schema), defaultValues: { current: '', password: '', confirm: '' } });
  const { alert, setAlert, handle } = useFormError(setError, ['current', 'password']);

  const onSubmit = handleSubmit(async ({ current, password }) => {
    setAlert(null);
    try {
      const res = await authApi.changePassword(current, password);
      toast.success(t('auth.change.done'));
      if (forced) await afterAuth(res.next);
      else navigate('/crm/me', { replace: true });
    } catch (e) {
      // Map server field names onto this form's fields.
      if (e && typeof e === 'object' && 'fieldErrors' in e) {
        const fe = (e as { fieldErrors: Record<string, string> }).fieldErrors;
        if (fe.currentPassword) setError('current', { type: 'server', message: fe.currentPassword });
        if (fe.newPassword) setError('password', { type: 'server', message: fe.newPassword });
        if (fe.currentPassword || fe.newPassword) return;
      }
      handle(e);
    }
  });

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <LockKeyhole />
          </IconBadge>
        }
        title={t('auth.change.title')}
        subtitle={forced ? t('auth.change.forced') : t('auth.change.voluntary')}
      />
      <form onSubmit={onSubmit} noValidate className="space-y-5">
        {alert ? <Alert tone="danger">{alert.message}</Alert> : null}
        <Field label={t('auth.change.current')} error={errors.current?.message}>
          <PasswordInput {...register('current')} inputSize="lg" autoComplete="current-password" autoFocus />
        </Field>
        <Field label={t('auth.change.next')} error={errors.password?.message} hint={<StrengthMeter password={watch('password')} />}>
          <PasswordInput {...register('password')} inputSize="lg" autoComplete="new-password" />
        </Field>
        <Field label={t('auth.change.confirm')} error={errors.confirm?.message}>
          <PasswordInput {...register('confirm')} inputSize="lg" autoComplete="new-password" />
        </Field>
        <Button type="submit" size="lg" className="w-full" loading={isSubmitting}>
          {isSubmitting ? t('auth.change.submitting') : t('auth.change.submit')}
        </Button>
      </form>
      <div className="mt-6 text-center text-[13px]">
        {forced ? (
          <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => void signOut()}>
            {t('common.signOut')}
          </button>
        ) : (
          <Link to="/crm/me" className="text-muted-foreground hover:text-foreground">
            {t('common.cancel')}
          </Link>
        )}
      </div>
    </AuthLayout>
  );
}
