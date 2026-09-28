import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { MailPlus, RotateCw, UserPlus, UsersRound, X } from 'lucide-react';
import { workspacesApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { Invitation, WorkspaceDetail } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { EmptyState } from '@crm/components/states';
import { ConfirmDialog, DevLink } from '@crm/components/page';
import { relativeTime } from '@crm/lib/utils';
import { Avatar } from '@crm/features/shell/user-menu';
import { InviteStatusBadge, isOpenInvite, MemberStatusBadge } from './workspace-ui';
import { InviteDialog } from './invite-dialog';

export function WorkspacePeopleTab({ workspace }: { workspace: WorkspaceDetail }) {
  const { t } = useTranslation();
  const [inviteOpen, setInviteOpen] = useState(false);
  const members = workspace.memberList;

  const inviteButton = (
    <Button size="sm" onClick={() => setInviteOpen(true)}>
      <UserPlus /> {t('workspaces.people.invite')}
    </Button>
  );

  return (
    <div className="space-y-6">
      <Card className="min-w-0 overflow-hidden">
        <CardHeader title={t('workspaces.people.membersTitle')} description={t('workspaces.people.membersBody')} actions={inviteButton} />
        {members.length === 0 ? (
          <EmptyState icon={UsersRound} title={t('workspaces.people.noMembers')} body={t('workspaces.people.noMembersBody')} action={inviteButton} />
        ) : (
          <>
            <div className="hidden overflow-x-auto sm:block">
              <table className="w-full text-left text-[13px]">
                <thead>
                  <tr className="border-b bg-muted/40 text-xs text-muted-foreground">
                    <th className="px-5 py-2.5 font-medium">{t('workspaces.people.colName')}</th>
                    <th className="px-3 py-2.5 font-medium">{t('workspaces.people.colRole')}</th>
                    <th className="px-3 py-2.5 font-medium">{t('workspaces.people.colStatus')}</th>
                    <th className="px-5 py-2.5 text-right font-medium">{t('workspaces.people.colLastLogin')}</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => (
                    <tr key={m.membershipId} className="border-b last:border-0 hover:bg-muted/50">
                      <td className="px-5 py-3">
                        <Link to={`/crm/owner/users/${m.identityId}`} className="flex items-center gap-3">
                          <Avatar name={m.displayName} />
                          <span className="min-w-0">
                            <span className="block truncate font-medium text-foreground hover:underline">{m.displayName}</span>
                            {m.email ? <span className="block truncate text-xs text-muted-foreground">{m.email}</span> : null}
                          </span>
                        </Link>
                      </td>
                      <td className="px-3 py-3">
                        {m.roleName || m.roleKey ? <Badge tone={m.roleKey === 'SUPER_ADMIN' ? 'primary' : 'neutral'}>{m.roleName ?? m.roleKey}</Badge> : '—'}
                      </td>
                      <td className="px-3 py-3">
                        <MemberStatusBadge status={m.status} />
                      </td>
                      <td className="px-5 py-3 text-right text-muted-foreground">{m.lastLoginAt ? relativeTime(m.lastLoginAt) : t('workspaces.people.never')}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <ul className="divide-y sm:hidden">
              {members.map((m) => (
                <li key={m.membershipId}>
                  <Link to={`/crm/owner/users/${m.identityId}`} className="flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
                    <Avatar name={m.displayName} />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-[13px] font-medium text-foreground">{m.displayName}</p>
                      <p className="truncate text-xs text-muted-foreground">
                        {m.roleName ?? m.roleKey}
                        {' · '}
                        {m.lastLoginAt ? relativeTime(m.lastLoginAt) : t('workspaces.people.never')}
                      </p>
                    </div>
                    <MemberStatusBadge status={m.status} />
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>

      <InvitationsCard workspace={workspace} />
      <InviteDialog open={inviteOpen} onOpenChange={setInviteOpen} workspace={workspace} />
    </div>
  );
}

function InvitationsCard({ workspace }: { workspace: WorkspaceDetail }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [revoking, setRevoking] = useState<Invitation | null>(null);
  const [devLinks, setDevLinks] = useState<Record<string, string>>({});
  const invitations = [...workspace.invitations].sort((a, b) => Number(isOpenInvite(b)) - Number(isOpenInvite(a)) || b.createdAt.localeCompare(a.createdAt));

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['workspace', workspace.id] });
    void qc.invalidateQueries({ queryKey: ['workspaces'] });
  };

  const resend = useMutation({
    mutationFn: (inv: Invitation) => workspacesApi.resendInvite(workspace.id, inv.id),
    onSuccess: (res, inv) => {
      refresh();
      if (res.devAcceptUrl) setDevLinks((d) => ({ ...d, [inv.id]: res.devAcceptUrl as string }));
      toast.success(t('workspaces.people.resent', { email: res.email ?? inv.email ?? inv.displayName }));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  const revoke = useMutation({
    mutationFn: (inv: Invitation) => workspacesApi.revokeInvite(workspace.id, inv.id),
    onSuccess: () => {
      refresh();
      setRevoking(null);
      toast.success(t('workspaces.people.revoked'));
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });

  return (
    <Card className="min-w-0 overflow-hidden">
      <CardHeader title={t('workspaces.people.invitesTitle')} description={t('workspaces.people.invitesBody')} />
      {invitations.length === 0 ? (
        <EmptyState icon={MailPlus} className="py-8" title={t('workspaces.people.noInvites')} />
      ) : (
        <ul className="divide-y">
          {invitations.map((inv) => {
            const open = isOpenInvite(inv);
            const expired = new Date(inv.expiresAt).getTime() < Date.now();
            return (
              <li key={inv.id} className="px-5 py-3">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                  <div className="min-w-0 flex-1">
                    <p className="flex flex-wrap items-center gap-2 text-[13px]">
                      <span className="truncate font-medium text-foreground">{inv.email ?? inv.displayName}</span>
                      <Badge tone={inv.roleKey === 'SUPER_ADMIN' ? 'primary' : 'neutral'}>{inv.roleName}</Badge>
                      <InviteStatusBadge status={inv.status} />
                    </p>
                    <p className="mt-0.5 truncate text-xs text-muted-foreground">
                      {inv.email && inv.displayName ? `${inv.displayName} · ` : ''}
                      {inv.status === 'accepted' && inv.acceptedAt
                        ? t('workspaces.people.acceptedAt', { time: relativeTime(inv.acceptedAt) })
                        : expired
                          ? t('workspaces.people.expiredAt', { time: relativeTime(inv.expiresAt) })
                          : t('workspaces.people.expiresAt', { time: relativeTime(inv.expiresAt) })}
                    </p>
                  </div>
                  {open ? (
                    <div className="flex shrink-0 items-center gap-1.5">
                      <Button
                        variant="outline"
                        size="sm"
                        loading={resend.isPending && resend.variables?.id === inv.id}
                        disabled={resend.isPending}
                        onClick={() => resend.mutate(inv)}
                      >
                        {!(resend.isPending && resend.variables?.id === inv.id) ? <RotateCw /> : null} {t('workspaces.people.resend')}
                      </Button>
                      <Button variant="ghost" size="sm" className="text-danger hover:bg-danger-soft" onClick={() => setRevoking(inv)}>
                        <X /> {t('workspaces.people.revoke')}
                      </Button>
                    </div>
                  ) : null}
                </div>
                {devLinks[inv.id] ? (
                  <div className="mt-2.5">
                    <DevLink url={devLinks[inv.id]} label={t('workspaces.devInviteLink')} />
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      <ConfirmDialog
        open={Boolean(revoking)}
        onOpenChange={(o) => !o && setRevoking(null)}
        tone="danger"
        title={t('workspaces.people.revokeTitle')}
        body={revoking ? t('workspaces.people.revokeBody', { email: revoking.email ?? revoking.displayName }) : undefined}
        confirmLabel={t('workspaces.people.revoke')}
        cancelLabel={t('common.cancel')}
        loading={revoke.isPending}
        onConfirm={() => revoking && revoke.mutate(revoking)}
      />
    </Card>
  );
}
