import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { KeyRound, Laptop, LogOut, ShieldCheck, ShieldOff, Smartphone } from 'lucide-react';
import { meApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import { useMe, useSignOut } from '@crm/auth/session';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { describeUserAgent, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';

export function ProfilePage() {
  const { t } = useTranslation();
  useDocumentTitle(t('me.title'));
  const { data: me } = useMe();
  const signOut = useSignOut();
  if (!me) return null;

  const rows: Array<[string, string | undefined]> = [
    [t('me.email'), me.identity.email],
    [t('me.phone'), me.identity.phone],
    [t('me.timezone'), me.identity.timezone],
    [t('me.lastLogin'), me.identity.lastLoginAt ? new Date(me.identity.lastLoginAt).toLocaleString('en-IN') : undefined]
  ];

  return (
    <div className="mx-auto w-full max-w-4xl px-4 py-6 sm:px-6 lg:py-8">
      <div className="mb-6">
        <h1 className="text-[22px] font-semibold tracking-tight sm:text-2xl">{t('me.title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('me.subtitle')}</p>
      </div>

      <div className="space-y-6">
        <Card>
          <div className="flex flex-wrap items-center gap-4 border-b px-5 py-5">
            <Avatar name={me.identity.displayName} className="size-12 text-base" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-base font-semibold">{me.identity.displayName}</p>
              <div className="mt-1 flex flex-wrap gap-1.5">
                {me.identity.isPlatformOwner ? <Badge tone="warning">{t('me.platformOwner')}</Badge> : null}
                {me.memberships.map((m) => (
                  <Badge key={m.id} tone="primary">
                    {m.workspaceName} · {m.roleName}
                  </Badge>
                ))}
              </div>
            </div>
          </div>
          <dl className="grid gap-x-8 gap-y-4 px-5 py-5 sm:grid-cols-2">
            {rows
              .filter(([, v]) => v)
              .map(([k, v]) => (
                <div key={k}>
                  <dt className="text-xs font-medium text-muted-foreground">{k}</dt>
                  <dd className="mt-0.5 break-words text-sm text-foreground">{v}</dd>
                </div>
              ))}
          </dl>
        </Card>

        <div className="grid gap-6 md:grid-cols-2">
          <Card className="flex flex-col">
            <CardHeader title={t('me.passwordTitle')} description={t('me.passwordBody')} />
            <div className="mt-auto flex p-5">
              <Button asChild variant="outline">
                <Link to="/crm/change-password">
                  <KeyRound /> {t('me.changePassword')}
                </Link>
              </Button>
            </div>
          </Card>

          {/* The owner console signs in without two-step verification (D-133). */}
          {me.identity.isPlatformOwner ? null : (
          <Card className="flex flex-col">
            <CardHeader
              title={t('me.mfaTitle')}
              description={me.session.mfaEnrolled ? t('me.mfaOnBody') : t('me.mfaOffBody')}
              actions={<Badge tone={me.session.mfaEnrolled ? 'success' : 'neutral'}>{me.session.mfaEnrolled ? t('me.mfaOn') : t('me.mfaOff')}</Badge>}
            />
            <div className="mt-auto flex flex-wrap items-center gap-3 p-5">
              {me.session.mfaEnrolled ? (
                <span className="flex items-center gap-2 text-[13px] text-success">
                  <ShieldCheck className="size-4" aria-hidden /> {t('me.mfaOnBody')}
                </span>
              ) : (
                <>
                  <Button asChild>
                    <Link to="/crm/mfa/setup">
                      <ShieldOff /> {t('me.mfaEnable')}
                    </Link>
                  </Button>
                  {me.session.privileged ? <span className="text-xs text-muted-foreground">{t('me.mfaEnforcedNote')}</span> : null}
                </>
              )}
            </div>
          </Card>
          )}
        </div>

        <SessionsCard onSignOutAll={() => void signOut({ all: true })} />
      </div>
    </div>
  );
}

function SessionsCard({ onSignOutAll }: { onSignOutAll: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['me', 'sessions'], queryFn: meApi.sessions });
  const revoke = useMutation({
    mutationFn: meApi.revokeSession,
    onSuccess: () => {
      toast.success(t('me.revoked'));
      void qc.invalidateQueries({ queryKey: ['me', 'sessions'] });
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  return (
    <Card>
      <CardHeader
        title={t('me.sessionsTitle')}
        description={t('me.sessionsBody')}
        actions={
          <Button variant="danger-outline" size="sm" onClick={onSignOutAll}>
            <LogOut /> {t('me.revokeAll')}
          </Button>
        }
      />
      {q.isError ? (
        <ErrorState title={t('common.genericError')} onRetry={() => void q.refetch()} />
      ) : !q.data ? (
        <div className="space-y-3 p-5">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </div>
      ) : (
        <ul className="divide-y">
          {q.data.map((s) => {
            const mobile = /iPhone|Android|iPad/.test(s.userAgent ?? '');
            const Icon = mobile ? Smartphone : Laptop;
            return (
              <li key={s.id} className="flex items-center gap-4 px-5 py-3.5">
                <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                  <Icon className="size-4" aria-hidden />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-2 text-[13px] font-medium text-foreground">
                    {describeUserAgent(s.userAgent)}
                    {s.current ? <Badge tone="success">{t('me.thisDevice')}</Badge> : null}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {s.ip ? `${s.ip} · ` : ''}
                    {t('me.active', { time: relativeTime(s.lastSeenAt) })}
                  </p>
                </div>
                {!s.current ? (
                  <Button variant="ghost" size="sm" loading={revoke.isPending && revoke.variables === s.id} onClick={() => revoke.mutate(s.id)}>
                    {t('me.revoke')}
                  </Button>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
