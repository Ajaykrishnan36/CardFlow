import { useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowLeft, AtSign, MailX, TimerOff, UserRound, UsersRound } from 'lucide-react';
import { invitationsApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { InvitationPreview } from '@crm/api/types';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { PasswordInput, StrengthMeter } from '@crm/components/ui/password-input';
import { Field } from '@crm/components/ui/field';
import { Alert } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { useAfterAuth } from '@crm/auth/session';
import { AuthHeading, AuthLayout } from './auth-layout';
import { useDocumentTitle } from './login-pages';
import { useFormError } from './use-form-error';

function IconBadge({ children, tone = 'primary' }: { children: React.ReactNode; tone?: 'primary' | 'warning' }) {
  return (
    <div className={`grid size-12 place-items-center rounded-xl [&_svg]:size-6 ${tone === 'warning' ? 'bg-warning-soft text-warning' : 'bg-primary-soft text-primary'}`}>
      {children}
    </div>
  );
}

function ToSignIn() {
  const { t } = useTranslation();
  return (
    <Button asChild variant="outline" size="lg" className="mt-2 w-full">
      <Link to="/crm/login">
        <ArrowLeft /> {t('invite.goToSignIn')}
      </Link>
    </Button>
  );
}

/** Friendly dead-end for links that can't be used (missing, unknown, expired, revoked, accepted). */
function InviteUnavailable({ title, body, tone = 'warning' }: { title: string; body: string; tone?: 'primary' | 'warning' }) {
  const Icon = tone === 'warning' ? TimerOff : MailX;
  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge tone={tone}>
            <Icon />
          </IconBadge>
        }
        title={title}
        subtitle={body}
      />
      <ToSignIn />
    </AuthLayout>
  );
}

/** /crm/accept-invite?token=… — public page an invitee lands on from their email. */
export function AcceptInvitePage() {
  const { t } = useTranslation();
  useDocumentTitle(t('invite.docTitle'));
  const [params] = useSearchParams();
  const token = params.get('token') ?? '';

  const q = useQuery({
    queryKey: ['invite', token],
    queryFn: () => invitationsApi.preview(token),
    enabled: Boolean(token),
    retry: false,
    staleTime: Infinity
  });

  if (!token) return <InviteUnavailable title={t('invite.missingTitle')} body={t('invite.missingBody')} />;

  if (q.isError) {
    if (isApiError(q.error) && (q.error.status === 404 || q.error.code === 'invitation_not_found')) {
      return <InviteUnavailable title={t('invite.notFoundTitle')} body={t('invite.notFoundBody')} />;
    }
    return (
      <AuthLayout>
        <AuthHeading title={t('invite.errorTitle')} />
        <Alert tone="danger">
          {isApiError(q.error) ? q.error.message : t('common.genericError')}
          {isApiError(q.error) && q.error.requestId ? (
            <span className="mt-1 block font-mono text-[11px]">{t('common.requestId', { id: q.error.requestId })}</span>
          ) : null}
        </Alert>
        <Button variant="outline" size="lg" className="mt-5 w-full" onClick={() => void q.refetch()} loading={q.isFetching}>
          {t('common.retry')}
        </Button>
      </AuthLayout>
    );
  }

  if (!q.data) {
    return (
      <AuthLayout>
        <div className="space-y-4" aria-busy>
          <Skeleton className="size-12 rounded-xl" />
          <Skeleton className="h-7 w-3/4" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="mt-6 h-11 w-full" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="h-11 w-full" />
        </div>
      </AuthLayout>
    );
  }

  const invite = q.data;
  const expired = invite.status === 'expired' || new Date(invite.expiresAt).getTime() < Date.now();
  if (invite.status === 'accepted') {
    return <InviteUnavailable tone="primary" title={t('invite.acceptedTitle')} body={t('invite.acceptedBody', { workspace: invite.workspaceName })} />;
  }
  if (invite.status === 'revoked') {
    return <InviteUnavailable title={t('invite.revokedTitle')} body={t('invite.revokedBody', { workspace: invite.workspaceName })} />;
  }
  if (expired) {
    return <InviteUnavailable title={t('invite.expiredTitle')} body={t('invite.expiredBody', { workspace: invite.workspaceName })} />;
  }

  return <AcceptForm token={token} invite={invite} />;
}

interface AcceptValues {
  displayName: string;
  password: string;
  confirm: string;
}

function AcceptForm({ token, invite }: { token: string; invite: InvitationPreview }) {
  const { t } = useTranslation();
  const afterAuth = useAfterAuth();
  const existing = invite.identityExists;

  const schema = useMemo(
    () =>
      existing
        ? z.object({
            displayName: z.string(),
            password: z.string().min(1, t('common.required')),
            confirm: z.string()
          })
        : z
            .object({
              displayName: z.string().trim().min(1, t('invite.nameRequired')).max(120),
              password: z.string().min(8, t('invite.passwordMin')).max(128),
              confirm: z.string().min(1, t('common.required'))
            })
            .refine((v) => v.password === v.confirm, { path: ['confirm'], message: t('auth.reset.mismatch') }),
    [existing, t]
  );

  const {
    register,
    handleSubmit,
    setError,
    watch,
    formState: { errors, isSubmitting }
  } = useForm<AcceptValues>({ resolver: zodResolver(schema), defaultValues: { displayName: invite.displayName ?? '', password: '', confirm: '' } });
  const { alert, setAlert, handle } = useFormError(setError, ['password', 'displayName']);

  const onSubmit = handleSubmit(async ({ displayName, password }) => {
    setAlert(null);
    try {
      const step = await invitationsApi.accept({ token, password, displayName: existing ? undefined : displayName.trim() });
      toast.success(t('invite.joined', { workspace: invite.workspaceName }));
      await afterAuth(step.next);
    } catch (e) {
      if (isApiError(e) && (e.status === 401 || e.code === 'invalid_credentials')) {
        setError('password', { type: 'server', message: e.message || t('invite.wrongPassword') });
        return;
      }
      handle(e);
    }
  });

  return (
    <AuthLayout>
      <AuthHeading
        icon={
          <IconBadge>
            <UsersRound />
          </IconBadge>
        }
        title={t('invite.title', { workspace: invite.workspaceName })}
        subtitle={t('invite.subtitle', { role: invite.roleName })}
      />
      <form onSubmit={onSubmit} noValidate className="space-y-5">
        {alert ? (
          <Alert tone="danger">
            {alert.message}
            {alert.requestId ? <span className="mt-1 block font-mono text-[11px]">{t('common.requestId', { id: alert.requestId })}</span> : null}
          </Alert>
        ) : null}

        <Field label={t('invite.email')}>
          <Input value={invite.email} readOnly disabled inputSize="lg" leading={<AtSign />} autoComplete="username" />
        </Field>

        {existing ? (
          <>
            <Alert tone="info" title={t('invite.existingTitle')}>
              {t('invite.existingBody')}
            </Alert>
            <Field
              label={t('invite.currentPassword')}
              error={errors.password?.message}
              labelAside={
                <Link to="/crm/forgot-password" className="text-[13px] font-medium text-primary hover:underline">
                  {t('auth.login.forgot')}
                </Link>
              }
            >
              <PasswordInput {...register('password')} inputSize="lg" autoComplete="current-password" autoFocus />
            </Field>
            <Button type="submit" size="lg" className="w-full" loading={isSubmitting}>
              {isSubmitting ? t('invite.accepting') : t('invite.signInAccept')}
            </Button>
          </>
        ) : (
          <>
            <Field label={t('invite.displayName')} error={errors.displayName?.message}>
              <Input {...register('displayName')} inputSize="lg" autoComplete="name" leading={<UserRound />} autoFocus />
            </Field>
            <Field label={t('invite.newPassword')} error={errors.password?.message} hint={<StrengthMeter password={watch('password')} />}>
              <PasswordInput {...register('password')} inputSize="lg" autoComplete="new-password" />
            </Field>
            <Field label={t('invite.confirmPassword')} error={errors.confirm?.message}>
              <PasswordInput {...register('confirm')} inputSize="lg" autoComplete="new-password" />
            </Field>
            <Button type="submit" size="lg" className="w-full" loading={isSubmitting}>
              {isSubmitting ? t('invite.accepting') : t('invite.createAccept')}
            </Button>
          </>
        )}
      </form>
      <p className="mt-6 text-center text-xs text-muted-foreground">{t('invite.expires', { date: new Date(invite.expiresAt).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }) })}</p>
    </AuthLayout>
  );
}
