import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowLeft, Ellipsis, KeyRound, LogOut, Mail, Pencil, Phone, ShieldOff, UserRoundCheck, UserRoundX, UserX } from 'lucide-react';
import { usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { UserDetail } from '@crm/api/types';
import { useMe } from '@crm/auth/session';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger, Tooltip } from '@crm/components/ui/menu';
import { Breadcrumbs, ConfirmDialog, DevLink, PageContainer, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';
import { shortDate, userStatusTone } from './user-format';
import { useApplyUser, userKey } from './use-user-mutations';
import { UserDetailsTab } from './user-details-tab';
import { UserAccessTab } from './user-access-tab';
import { UserActivityTab } from './user-activity-tab';

type TabKey = 'details' | 'access' | 'activity';
const TABS: TabKey[] = ['details', 'access', 'activity'];

export function UserDetailPage() {
  const { t } = useTranslation();
  const { id = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const rawTab = params.get('tab') as TabKey | null;
  const tab: TabKey = rawTab && TABS.includes(rawTab) ? rawTab : 'details';
  const [editing, setEditing] = useState(false);
  const { data: me } = useMe();

  const q = useQuery({ queryKey: userKey(id), queryFn: () => usersApi.get(id), enabled: id !== '' });
  useDocumentTitle(q.data?.displayName ?? t('users.list.title'));

  const setTab = (next: TabKey) =>
    setParams(
      (prev) => {
        const sp = new URLSearchParams(prev);
        if (next === 'details') sp.delete('tab');
        else sp.set('tab', next);
        return sp;
      },
      { replace: true }
    );

  const crumbs = [{ label: t('users.detail.crumb'), to: '/crm/owner/users' }, { label: q.data?.displayName ?? '…' }];

  if (q.isError) {
    const notFound = isApiError(q.error) && q.error.status === 404;
    return (
      <PageContainer>
        <Breadcrumbs items={crumbs} />
        <Card className="mt-4">
          {notFound ? (
            <EmptyState
              icon={UserX}
              title={t('users.detail.notFoundTitle')}
              body={t('users.detail.notFoundBody')}
              action={
                <Button asChild variant="outline" size="sm">
                  <Link to="/crm/owner/users">
                    <ArrowLeft /> {t('users.detail.backToUsers')}
                  </Link>
                </Button>
              }
            />
          ) : (
            <ErrorState
              title={t('users.detail.errorTitle')}
              message={isApiError(q.error) ? q.error.message : undefined}
              requestId={isApiError(q.error) ? q.error.requestId : undefined}
              onRetry={() => void q.refetch()}
            />
          )}
        </Card>
      </PageContainer>
    );
  }

  const user = q.data;
  if (!user) return <DetailSkeleton crumbs={crumbs} />;
  const isSelf = me?.identity.id === user.id;

  return (
    <PageContainer>
      <Breadcrumbs items={crumbs} />

      <Card className="mt-2 overflow-hidden">
        <div className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center">
          <Avatar name={user.displayName} className="size-14 text-lg" />
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="min-w-0 truncate text-xl font-semibold tracking-tight text-foreground">{user.displayName}</h1>
              <Badge tone={userStatusTone[user.status] ?? 'neutral'}>{t(`users.status.${user.status}`, { defaultValue: user.status })}</Badge>
              {user.isPlatformOwner ? <Badge tone="warning">{t('users.badge.owner')}</Badge> : null}
              {isSelf ? <Badge>{t('users.badge.you')}</Badge> : null}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-[13px] text-muted-foreground">
              {user.email ? (
                <a href={`mailto:${user.email}`} className="flex min-w-0 items-center gap-1.5 hover:text-foreground">
                  <Mail className="size-3.5 shrink-0" aria-hidden />
                  <span className="truncate">{user.email}</span>
                </a>
              ) : null}
              {user.phone ? (
                <a href={`tel:${user.phone}`} className="flex items-center gap-1.5 tabular-nums hover:text-foreground">
                  <Phone className="size-3.5 shrink-0" aria-hidden />
                  {user.phone}
                </a>
              ) : null}
            </div>
          </div>
          <div className="flex items-center gap-2">
            {!editing ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setTab('details');
                  setEditing(true);
                }}
              >
                <Pencil /> {t('users.detail.edit')}
              </Button>
            ) : null}
            <UserActions user={user} isSelf={isSelf} />
          </div>
        </div>

        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 border-t bg-muted/30 px-5 py-3 sm:grid-cols-3 lg:grid-cols-5">
          <Highlight label={t('users.detail.hlLastLogin')}>
            {user.lastLoginAt ? (
              <Tooltip content={new Date(user.lastLoginAt).toLocaleString('en-IN')} side="bottom">
                <span>{relativeTime(user.lastLoginAt)}</span>
              </Tooltip>
            ) : (
              <span className="text-muted-foreground">{t('users.detail.never')}</span>
            )}
          </Highlight>
          <Highlight label={t('users.detail.hlSessions')}>
            <span className="tabular-nums">{user.activeSessions}</span>
          </Highlight>
          <Highlight label={t('users.detail.hlMfa')}>
            <Badge tone={user.mfaEnrolled ? 'success' : 'neutral'}>{user.mfaEnrolled ? t('users.detail.on') : t('users.detail.off')}</Badge>
          </Highlight>
          <Highlight label={t('users.detail.hlPassword')}>
            {user.hasPassword ? t('users.detail.passwordSet') : <span className="text-muted-foreground">{t('users.detail.passwordNotSet')}</span>}
          </Highlight>
          <Highlight label={t('users.detail.hlCreated')}>{shortDate(user.createdAt)}</Highlight>
        </dl>
      </Card>

      {user.status === 'suspended' ? (
        <Alert tone="warning" className="mt-4">
          {t('users.detail.suspendedBanner')}
        </Alert>
      ) : null}

      <Tabs<TabKey>
        className="mt-6"
        value={tab}
        onChange={setTab}
        items={[
          { value: 'details', label: t('users.detail.tabDetails') },
          {
            value: 'access',
            label: (
              <span className="flex items-center gap-1.5">
                {t('users.detail.tabAccess')}
                <span className="rounded-full bg-muted px-1.5 text-[11px] tabular-nums text-muted-foreground">{user.memberships.length}</span>
              </span>
            )
          },
          { value: 'activity', label: t('users.detail.tabActivity') }
        ]}
      />

      <div className="mt-5" role="tabpanel">
        {tab === 'details' ? (
          <UserDetailsTab user={user} editing={editing} onEditingChange={setEditing} />
        ) : tab === 'access' ? (
          <UserAccessTab user={user} />
        ) : (
          <UserActivityTab user={user} />
        )}
      </div>
    </PageContainer>
  );
}

function Highlight({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="truncate text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className="mt-1 truncate text-[13px] font-medium text-foreground">{children}</dd>
    </div>
  );
}

type ConfirmKind = 'revoke' | 'mfa' | 'suspend' | 'reactivate';

function UserActions({ user, isSelf }: { user: UserDetail; isSelf: boolean }) {
  const { t } = useTranslation();
  const apply = useApplyUser(user.id);
  const [confirm, setConfirm] = useState<ConfirmKind | null>(null);
  const [devResetUrl, setDevResetUrl] = useState<string | null>(null);
  const onError = (e: unknown) => toast.error(isApiError(e) ? e.message : t('common.genericError'));

  const reset = useMutation({
    mutationFn: () => usersApi.sendPasswordReset(user.id),
    onSuccess: (r) => {
      toast.success(r.message || t('users.actions.resetSent'));
      if (r.devResetUrl) setDevResetUrl(r.devResetUrl);
    },
    onError
  });
  const revoke = useMutation({
    mutationFn: () => usersApi.revokeSessions(user.id),
    onSuccess: (u) => {
      apply(u);
      setConfirm(null);
      toast.success(t('users.actions.revoked'));
    },
    onError
  });
  const mfa = useMutation({
    mutationFn: () => usersApi.resetMfa(user.id),
    onSuccess: (u) => {
      apply(u);
      setConfirm(null);
      toast.success(t('users.actions.mfaReset'));
    },
    onError
  });
  const status = useMutation({
    mutationFn: (next: 'active' | 'suspended') => usersApi.update(user.id, { status: next }),
    onSuccess: (u, next) => {
      apply(u);
      setConfirm(null);
      toast.success(next === 'suspended' ? t('users.actions.suspended') : t('users.actions.reactivated'));
    },
    onError
  });

  const name = user.displayName;
  const dialogs: Record<ConfirmKind, { title: string; body: string; confirmLabel: string; tone: 'primary' | 'danger'; loading: boolean; run: () => void }> = {
    revoke: {
      title: t('users.actions.revokeTitle', { name }),
      body: isSelf ? t('users.actions.revokeSelfBody') : t('users.actions.revokeBody'),
      confirmLabel: t('users.actions.revokeConfirm'),
      tone: 'danger',
      loading: revoke.isPending,
      run: () => revoke.mutate()
    },
    mfa: {
      title: t('users.actions.resetMfaTitle', { name }),
      body: t('users.actions.resetMfaBody'),
      confirmLabel: t('users.actions.resetMfaConfirm'),
      tone: 'danger',
      loading: mfa.isPending,
      run: () => mfa.mutate()
    },
    suspend: {
      title: t('users.actions.suspendTitle', { name }),
      body: t('users.actions.suspendBody'),
      confirmLabel: t('users.actions.suspendConfirm'),
      tone: 'danger',
      loading: status.isPending,
      run: () => status.mutate('suspended')
    },
    reactivate: {
      title: t('users.actions.reactivateTitle', { name }),
      body: t('users.actions.reactivateBody'),
      confirmLabel: t('users.actions.reactivateConfirm'),
      tone: 'primary',
      loading: status.isPending,
      run: () => status.mutate('active')
    }
  };
  const active = confirm ? dialogs[confirm] : null;
  const suspended = user.status === 'suspended';

  return (
    <>
      {/* modal={false}: lets the confirm dialogs take focus cleanly after the menu closes. */}
      <Menu modal={false}>
        <MenuTrigger asChild>
          <Button variant="outline" size="icon-sm" aria-label={t('users.detail.moreActions')} loading={reset.isPending}>
            {!reset.isPending ? <Ellipsis /> : null}
          </Button>
        </MenuTrigger>
        <MenuContent align="end">
          <MenuItem onSelect={() => reset.mutate()} disabled={!user.email || user.status === 'deleted'}>
            <KeyRound /> {t('users.actions.sendReset')}
          </MenuItem>
          <MenuItem onSelect={() => setConfirm('revoke')} disabled={user.activeSessions === 0 && !isSelf}>
            <LogOut /> {t('users.actions.revokeSessions')}
          </MenuItem>
          {user.mfaEnrolled ? (
            <MenuItem onSelect={() => setConfirm('mfa')}>
              <ShieldOff /> {t('users.actions.resetMfa')}
            </MenuItem>
          ) : null}
          <MenuSeparator />
          {user.isPlatformOwner ? (
            <Tooltip content={t('users.actions.ownerCannotSuspend')} side="left">
              <div>
                <MenuItem disabled danger>
                  <UserRoundX /> {t('users.actions.suspend')}
                </MenuItem>
              </div>
            </Tooltip>
          ) : suspended ? (
            <MenuItem onSelect={() => setConfirm('reactivate')}>
              <UserRoundCheck /> {t('users.actions.reactivate')}
            </MenuItem>
          ) : (
            <MenuItem danger onSelect={() => setConfirm('suspend')} disabled={user.status === 'deleted'}>
              <UserRoundX /> {t('users.actions.suspend')}
            </MenuItem>
          )}
        </MenuContent>
      </Menu>

      {active ? (
        <ConfirmDialog
          open
          onOpenChange={(o) => {
            if (!o && !active.loading) setConfirm(null);
          }}
          title={active.title}
          body={active.body}
          confirmLabel={active.confirmLabel}
          tone={active.tone}
          loading={active.loading}
          onConfirm={active.run}
        />
      ) : null}

      <Dialog open={devResetUrl !== null} onOpenChange={(o) => !o && setDevResetUrl(null)}>
        <DialogContent className="max-w-md p-5">
          <DialogTitle className="pr-8 text-base font-semibold">{t('users.actions.resetDialogTitle')}</DialogTitle>
          <DialogDescription className="mt-2 text-sm text-muted-foreground">{t('users.actions.resetSent')}</DialogDescription>
          {devResetUrl ? (
            <div className="mt-4">
              <DevLink url={devResetUrl} label={t('users.actions.resetDevLabel')} />
            </div>
          ) : null}
          <div className="mt-5 flex justify-end">
            <Button variant="outline" onClick={() => setDevResetUrl(null)}>
              {t('common.close')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function DetailSkeleton({ crumbs }: { crumbs: Array<{ label: string; to?: string }> }) {
  return (
    <PageContainer>
      <Breadcrumbs items={crumbs} />
      <Card className="mt-2 overflow-hidden" aria-busy>
        <div className="flex items-center gap-4 p-5">
          <Skeleton className="size-14 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-3.5 w-64 max-w-full" />
          </div>
          <Skeleton className="h-8 w-24" />
        </div>
        <div className="grid grid-cols-2 gap-4 border-t px-5 py-3 sm:grid-cols-3 lg:grid-cols-5">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="space-y-1.5">
              <Skeleton className="h-3 w-20" />
              <Skeleton className="h-4 w-16" />
            </div>
          ))}
        </div>
      </Card>
      <Skeleton className="mt-6 h-9 w-72 max-w-full" />
      <Card className="mt-5 space-y-4 p-5">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </div>
        ))}
      </Card>
    </PageContainer>
  );
}
