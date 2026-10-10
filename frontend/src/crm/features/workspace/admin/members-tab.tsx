import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { CircleCheck, Lock, MailPlus, UserPlus, UsersRound } from 'lucide-react';
import { workspaceAdminApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { Invitation, WorkspaceAdminMember, WorkspaceAdminOptions, WorkspaceInviteBody } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select, Switch } from '@crm/components/ui/form-controls';
import { PasswordInput, StrengthMeter } from '@crm/components/ui/password-input';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Tooltip } from '@crm/components/ui/menu';
import { DevLink, SegmentedFilter } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { Avatar } from '@crm/features/shell/user-menu';
import { EffectiveAccessMatrix } from '@crm/features/access/effective-access';
import { guessTone, humanize } from '@crm/features/records/use-object-meta';
import { InviteLinkCard } from '@crm/features/tools/invite-link-card';
import { adminErrorMessage, adminKeys, exceedingGrants, useAdminMembers, useInvalidateAdmin } from './admin-utils';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function useLimits(options: WorkspaceAdminOptions) {
  const { t } = useTranslation();
  const allRecords = t('access.matrix.scope.workspace').toLowerCase();
  return useMemo(() => {
    const roleBeyond = new Map(options.roles.map((r) => [r.key, exceedingGrants(r.rules, options.grantable, options.catalog, allRecords)]));
    const setBeyond = new Map(options.permissionSets.map((s) => [s.id, exceedingGrants(s.rules, options.grantable, options.catalog, allRecords)]));
    return {
      roleExceeds: (key: string) => (roleBeyond.get(key)?.length ?? 0) > 0,
      setExceeds: (id: string) => (setBeyond.get(id)?.length ?? 0) > 0,
      roleName: (key?: string, fallback?: string) => options.roles.find((r) => r.key === key)?.name ?? fallback ?? (key ? humanize(key.toLowerCase()) : '—')
    };
  }, [options, allRecords]);
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  return <Badge tone={guessTone(status)}>{t(`workspaceApp.admin.status.${status}`, { defaultValue: humanize(status) })}</Badge>;
}

function LockMark({ reason }: { reason?: string }) {
  const { t } = useTranslation();
  const text = reason || t('workspaceApp.admin.locked');
  return (
    <Tooltip content={<span className="block max-w-[240px]">{text}</span>} side="top">
      <span tabIndex={0} className="inline-grid size-5 place-items-center rounded text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={text} onClick={(e) => e.stopPropagation()}>
        <Lock className="size-3.5" aria-hidden />
      </span>
    </Tooltip>
  );
}

/** Users tab: members, their access, invitations. Everything is limited to what you hold yourself. */
export function MembersTab({ code, options }: { code: string; options: WorkspaceAdminOptions }) {
  const { t } = useTranslation();
  const q = useAdminMembers(code, true);
  const limits = useLimits(options);
  const [selected, setSelected] = useState<WorkspaceAdminMember | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);
  const members = q.data?.data;
  const invitations = (q.data?.invitations ?? []).filter((i) => i.status === 'pending' || i.status === 'delivered' || i.status === 'delivery_failed');

  return (
    <div className="space-y-4">
      <Card className="min-w-0 overflow-hidden">
        <CardHeader
          title={t('workspaceApp.admin.users.title')}
          description={members ? t('workspaceApp.admin.users.count', { count: members.length }) : undefined}
          actions={
            <Button size="sm" onClick={() => setInviteOpen(true)}>
              <UserPlus /> {t('workspaceApp.admin.users.invite')}
            </Button>
          }
        />
        {q.isError ? (
          <ErrorState
            title={t('workspaceApp.admin.users.errorTitle')}
            message={adminErrorMessage(q.error, t)}
            requestId={isApiError(q.error) ? q.error.requestId : undefined}
            onRetry={() => void q.refetch()}
          />
        ) : !members ? (
          <div className="space-y-3 p-4" aria-busy="true">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3">
                <Skeleton className="size-8 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-40" />
                  <Skeleton className="h-3 w-56" />
                </div>
              </div>
            ))}
          </div>
        ) : members.length === 0 ? (
          <EmptyState icon={UsersRound} title={t('workspaceApp.admin.users.emptyTitle')} body={t('workspaceApp.admin.users.emptyBody')} />
        ) : (
          <>
            <div className="hidden overflow-x-auto md:block">
              <table className="w-full text-left text-[13px]">
                <thead className="bg-muted/40">
                  <tr className="border-b text-xs text-muted-foreground">
                    <th scope="col" className="py-2 pl-4 pr-3 font-medium">{t('workspaceApp.admin.users.colName')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('workspaceApp.admin.users.colRole')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('workspaceApp.admin.users.colSets')}</th>
                    <th scope="col" className="px-3 py-2 font-medium">{t('workspaceApp.admin.users.colStatus')}</th>
                    <th scope="col" className="px-3 py-2 pr-4 text-right font-medium">{t('workspaceApp.admin.users.colLastLogin')}</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => (
                    <tr key={m.membershipId} className="cursor-pointer border-b last:border-0 hover:bg-muted/50" onClick={() => setSelected(m)}>
                      <td className="max-w-[320px] py-2 pl-4 pr-3">
                        <div className="flex items-center gap-2.5">
                          <Avatar name={m.displayName} className="size-7 text-[11px]" />
                          <div className="min-w-0">
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                setSelected(m);
                              }}
                              className="flex max-w-full items-center gap-1.5 truncate text-left font-medium text-foreground hover:text-primary hover:underline"
                            >
                              <span className="truncate">{m.displayName}</span>
                              {m.isSelf ? <Badge tone="primary">{t('workspaceApp.admin.users.you')}</Badge> : null}
                            </button>
                            <p className="truncate text-xs text-muted-foreground">{m.email || m.phone || '—'}</p>
                          </div>
                          {!m.editable ? <LockMark reason={m.lockedReason} /> : null}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-foreground">
                        {limits.roleName(m.roleKey, m.roleName)}
                        {m.userType ? <span className="block text-xs text-muted-foreground">{options.userTypes?.find((u) => u.key === m.userType)?.label ?? m.userType}</span> : null}
                      </td>
                      <td className="max-w-[220px] px-3 py-2">
                        <span className="block truncate text-muted-foreground" title={m.permissionSets.map((s) => s.name).join(', ')}>
                          {m.permissionSets.length ? m.permissionSets.map((s) => s.name).join(', ') : '—'}
                        </span>
                      </td>
                      <td className="px-3 py-2">
                        <StatusBadge status={m.status} />
                      </td>
                      <td className="px-3 py-2 pr-4 text-right text-muted-foreground">
                        {m.lastLoginAt ? <time dateTime={m.lastLoginAt}>{relativeTime(m.lastLoginAt)}</time> : t('workspaceApp.admin.users.never')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <ul className="divide-y md:hidden">
              {members.map((m) => (
                <li key={m.membershipId}>
                  <button type="button" onClick={() => setSelected(m)} className="flex w-full items-start gap-3 px-4 py-3 text-left hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none">
                    <Avatar name={m.displayName} className="size-8" />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-1.5 text-[13px] font-medium text-foreground">
                        <span className="truncate">{m.displayName}</span>
                        {m.isSelf ? <Badge tone="primary">{t('workspaceApp.admin.users.you')}</Badge> : null}
                        {!m.editable ? <Lock className="size-3.5 shrink-0 text-muted-foreground" aria-label={m.lockedReason || t('workspaceApp.admin.locked')} /> : null}
                      </p>
                      <p className="truncate text-xs text-muted-foreground">{m.email || m.phone || '—'}</p>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">
                        {limits.roleName(m.roleKey, m.roleName)}
                        {m.permissionSets.length ? ` · ${m.permissionSets.map((s) => s.name).join(', ')}` : ''}
                      </p>
                    </div>
                    <StatusBadge status={m.status} />
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
      </Card>

      {invitations.length ? <InvitationsCard invitations={invitations} /> : null}

      <MemberDialog code={code} options={options} member={selected} onClose={() => setSelected(null)} />
      <InviteLinkCard />
      <InviteDialog code={code} options={options} open={inviteOpen} onOpenChange={setInviteOpen} />
    </div>
  );
}

function InvitationsCard({ invitations }: { invitations: Invitation[] }) {
  const { t } = useTranslation();
  return (
    <Card className="overflow-hidden">
      <CardHeader title={t('workspaceApp.admin.invitations.title')} description={t('workspaceApp.admin.invitations.count', { count: invitations.length })} />
      <ul className="divide-y">
        {invitations.map((inv) => (
          <li key={inv.id} className="flex flex-wrap items-center gap-3 px-4 py-2.5 sm:px-5">
            <span className="grid size-8 shrink-0 place-items-center rounded-full bg-muted text-muted-foreground">
              <MailPlus className="size-4" aria-hidden />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-medium text-foreground">{inv.displayName}</p>
              <p className="truncate text-xs text-muted-foreground">
                {inv.email ?? '—'} · {inv.roleName}
              </p>
            </div>
            <span className="text-xs text-muted-foreground">{t('workspaceApp.admin.invitations.expires', { time: relativeTime(inv.expiresAt) })}</span>
            <Badge tone={guessTone(inv.status)}>{t(`records.invitationStatus.${inv.status}`, { defaultValue: humanize(inv.status) })}</Badge>
          </li>
        ))}
      </ul>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Member side sheet
// ---------------------------------------------------------------------------

function sameSet(a: string[], b: string[]) {
  return a.length === b.length && a.every((x) => b.includes(x));
}

function MemberDialog({ code, options, member, onClose }: { code: string; options: WorkspaceAdminOptions; member: WorkspaceAdminMember | null; onClose: () => void }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const invalidate = useInvalidateAdmin(code);
  const limits = useLimits(options);
  const open = member !== null;
  const readOnly = !member?.editable;
  const [roleKey, setRoleKey] = useState('');
  const [productIds, setProductIds] = useState<string[]>([]);
  const [setIds, setSetIds] = useState<string[]>([]);
  const [status, setStatus] = useState<WorkspaceAdminMember['status']>('active');
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!member) return;
    setRoleKey(member.roleKey ?? '');
    setProductIds(member.productIds);
    setSetIds(member.permissionSets.map((s) => s.id));
    setStatus(member.status);
    setFormError(null);
  }, [member]);

  const changes = useMemo(() => {
    if (!member) return {};
    const body: { roleKey?: string; productIds?: string[]; permissionSetIds?: string[]; status?: 'active' | 'suspended' } = {};
    if (roleKey && roleKey !== (member.roleKey ?? '')) body.roleKey = roleKey;
    if (!options.isPlatform && !sameSet(productIds, member.productIds)) body.productIds = productIds;
    if (!sameSet(setIds, member.permissionSets.map((s) => s.id))) body.permissionSetIds = setIds;
    if (status !== member.status && (status === 'active' || status === 'suspended')) body.status = status;
    return body;
  }, [member, roleKey, productIds, setIds, status, options.isPlatform]);
  const dirty = Object.keys(changes).length > 0;

  const save = useMutation({
    mutationFn: () => workspaceAdminApi(code).updateMember(member!.membershipId, changes),
    onSuccess: (updated) => {
      qc.setQueryData<{ data: WorkspaceAdminMember[]; invitations: Invitation[] }>(adminKeys.members(code), (prev) =>
        prev ? { ...prev, data: prev.data.map((m) => (m.membershipId === updated.membershipId ? updated : m)) } : prev
      );
      invalidate();
      toast.success(t('workspaceApp.admin.users.saved', { name: updated.displayName }));
      onClose();
    },
    onError: (e) => setFormError(adminErrorMessage(e, t))
  });

  // Products you hold, plus any they already have that you don't (shown, not grantable).
  const productOptions = useMemo(() => {
    if (!member) return [];
    const mine = options.products.map((p) => ({ ...p, grantable: true }));
    const theirs = member.effective.products.filter((p) => !options.products.some((o) => o.id === p.id)).map((p) => ({ ...p, grantable: false }));
    return [...mine, ...theirs];
  }, [member, options.products]);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!dirty) return onClose();
    setFormError(null);
    save.mutate();
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className="left-auto right-0 top-0 flex h-full w-full max-w-xl translate-x-0 flex-col rounded-none border-y-0 border-r-0 p-0 sm:w-[92vw]">
        {member ? (
          <>
            <div className="flex items-center gap-3 border-b px-5 py-4 pr-12">
              <Avatar name={member.displayName} className="size-9" />
              <div className="min-w-0">
                <DialogTitle className="flex items-center gap-2 truncate text-base font-semibold">
                  <span className="truncate">{member.displayName}</span>
                  <StatusBadge status={member.status} />
                </DialogTitle>
                <DialogDescription className="truncate text-[13px] text-muted-foreground">{member.email || member.phone || '—'}</DialogDescription>
              </div>
            </div>
            <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
                {readOnly ? (
                  <Alert tone="info" title={t('workspaceApp.admin.users.lockedTitle')}>
                    {member.lockedReason || t('workspaceApp.admin.locked')}
                  </Alert>
                ) : null}
                {formError ? <Alert tone="danger">{formError}</Alert> : null}

                <Field label={t('workspaceApp.admin.users.role')} hint={t('workspaceApp.admin.users.roleHint')}>
                  <Select value={roleKey} onChange={(e) => setRoleKey(e.target.value)} disabled={readOnly || save.isPending}>
                    {!roleKey ? <option value="">{t('workspaceApp.admin.users.chooseRole')}</option> : null}
                    {options.roles.map((r) => {
                      const beyond = limits.roleExceeds(r.key) && r.key !== member.roleKey;
                      return (
                        <option key={r.key} value={r.key} disabled={beyond}>
                          {r.name}
                          {beyond ? ` — ${t('workspaceApp.admin.moreThanYours')}` : ''}
                        </option>
                      );
                    })}
                  </Select>
                </Field>

                {!options.isPlatform ? (
                  <fieldset>
                    <legend className="mb-2 text-[13px] font-medium text-foreground">{t('workspaceApp.admin.users.products')}</legend>
                    {productOptions.length === 0 ? (
                      <p className="text-[13px] text-muted-foreground">{t('workspaceApp.admin.users.noProducts')}</p>
                    ) : (
                      <div className="grid gap-2 sm:grid-cols-2">
                        {productOptions.map((p) => {
                          const checked = productIds.includes(p.id);
                          return (
                            <Checkbox
                              key={p.id}
                              className="rounded-md border px-3 py-2"
                              checked={checked}
                              disabled={readOnly || save.isPending || (!p.grantable && !checked)}
                              onCheckedChange={(c) => setProductIds((ids) => (c ? [...ids, p.id] : ids.filter((x) => x !== p.id)))}
                              label={p.name}
                              description={p.grantable ? undefined : t('workspaceApp.admin.notYours')}
                            />
                          );
                        })}
                      </div>
                    )}
                  </fieldset>
                ) : null}

                <fieldset>
                  <legend className="mb-2 text-[13px] font-medium text-foreground">{t('workspaceApp.admin.users.sets')}</legend>
                  {options.permissionSets.length === 0 ? (
                    <p className="text-[13px] text-muted-foreground">{t('workspaceApp.admin.users.noSets')}</p>
                  ) : (
                    <div className="space-y-2">
                      {options.permissionSets.map((s) => {
                        const checked = setIds.includes(s.id);
                        const beyond = limits.setExceeds(s.id);
                        return (
                          <Checkbox
                            key={s.id}
                            className="rounded-md border px-3 py-2"
                            checked={checked}
                            disabled={readOnly || save.isPending || (beyond && !checked)}
                            onCheckedChange={(c) => setSetIds((ids) => (c ? [...ids, s.id] : ids.filter((x) => x !== s.id)))}
                            label={s.name}
                            description={beyond ? t('workspaceApp.admin.moreThanYours') : s.description}
                          />
                        );
                      })}
                    </div>
                  )}
                </fieldset>

                {member.status === 'active' || member.status === 'suspended' ? (
                  <Switch
                    id={`member-status-${member.membershipId}`}
                    className="rounded-lg border px-3 py-2.5"
                    label={t('workspaceApp.admin.users.active')}
                    description={status === 'active' ? t('workspaceApp.admin.users.activeOn') : t('workspaceApp.admin.users.activeOff')}
                    checked={status === 'active'}
                    disabled={readOnly || save.isPending}
                    onCheckedChange={(on) => setStatus(on ? 'active' : 'suspended')}
                  />
                ) : null}

                <div className="rounded-lg border p-3">
                  <EffectiveAccessMatrix
                    effective={member.effective}
                    catalog={options.catalog}
                    isPlatform={options.isPlatform}
                    note={dirty ? t('workspaceApp.admin.users.effectiveSavedNote') : undefined}
                  />
                </div>
              </div>
              <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
                <Button type="button" variant="outline" onClick={onClose} disabled={save.isPending}>
                  {readOnly ? t('workspaceApp.admin.close') : t('common.cancel')}
                </Button>
                {!readOnly ? (
                  <Button type="submit" loading={save.isPending} disabled={!dirty}>
                    {t('workspaceApp.admin.save')}
                  </Button>
                ) : null}
              </div>
            </form>
          </>
        ) : (
          <>
            <DialogTitle className="sr-only">{t('workspaceApp.admin.users.title')}</DialogTitle>
            <DialogDescription className="sr-only">{t('workspaceApp.admin.users.title')}</DialogDescription>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Invite
// ---------------------------------------------------------------------------

interface InviteDraft {
  displayName: string;
  email: string;
  phone: string;
  roleKey: string;
  productIds: string[];
  permissionSetIds: string[];
  method: 'invite' | 'password';
  password: string;
  userType: string;
}

type InviteResult = { identityId: string; membershipId: string; invitation?: Invitation; existingLogin?: boolean };

function InviteDialog({ code, options, open, onOpenChange }: { code: string; options: WorkspaceAdminOptions; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const invalidate = useInvalidateAdmin(code);
  const limits = useLimits(options);
  const blank = (): InviteDraft => ({
    displayName: '',
    email: '',
    phone: '',
    roleKey: '',
    productIds: options.products.map((p) => p.id),
    permissionSetIds: [],
    method: 'invite',
    password: '',
    userType: ''
  });
  const [d, setD] = useState<InviteDraft>(blank);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [result, setResult] = useState<{ res: InviteResult; body: WorkspaceInviteBody } | null>(null);

  const reset = () => {
    setD(blank());
    setErrors({});
    setFormError(null);
    setResult(null);
  };
  useEffect(() => {
    if (open) reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const patch = (p: Partial<InviteDraft>) => {
    setD((prev) => ({ ...prev, ...p }));
    const keys = Object.keys(p);
    if (keys.some((k) => errors[k])) setErrors((e) => Object.fromEntries(Object.entries(e).filter(([k]) => !keys.includes(k))));
  };

  const invite = useMutation({
    mutationFn: (body: WorkspaceInviteBody) => workspaceAdminApi(code).invite(body),
    onSuccess: (res, body) => {
      setResult({ res, body });
      invalidate();
    },
    onError: (e) => {
      if (isApiError(e) && Object.keys(e.fieldErrors).length) {
        setErrors(e.fieldErrors);
        setFormError(t('workspaceApp.admin.fixErrors'));
        return;
      }
      setFormError(adminErrorMessage(e, t));
    }
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next: Record<string, string> = {};
    if (!d.displayName.trim()) next.displayName = t('common.required');
    if (!EMAIL_RE.test(d.email.trim())) next.email = t('workspaceApp.admin.invite.emailInvalid');
    if (!d.roleKey) next.roleKey = t('workspaceApp.admin.invite.roleRequired');
    if (d.method === 'password' && d.password.length < 8) next.password = t('workspaceApp.admin.invite.passwordShort');
    setErrors(next);
    if (Object.keys(next).length) {
      setFormError(null);
      return;
    }
    setFormError(null);
    invite.mutate({
      displayName: d.displayName.trim(),
      email: d.email.trim(),
      phone: d.phone.trim() || undefined,
      roleKey: d.roleKey,
      productIds: options.isPlatform ? undefined : d.productIds,
      permissionSetIds: d.permissionSetIds,
      method: d.method,
      password: d.method === 'password' ? d.password : undefined,
      userType: d.userType || undefined
    });
  };

  const busy = invite.isPending;
  const userTypes = options.userTypes ?? [];
  const pickedType = userTypes.find((u) => u.key === d.userType);

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="top-[3vh] flex max-h-[94vh] max-w-2xl flex-col p-0 sm:top-[6vh] sm:max-h-[88vh]">
        {result ? (
          <>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <div className="flex flex-col items-center px-6 pb-4 pt-8 text-center">
                <span className="grid size-11 place-items-center rounded-full bg-success-soft text-success">
                  <CircleCheck className="size-5" aria-hidden />
                </span>
                <DialogTitle className="mt-3 text-base font-semibold">
                  {result.res.existingLogin
                    ? t('workspaceApp.admin.invite.doneExistingTitle', { name: result.body.displayName })
                    : result.res.invitation
                      ? t('workspaceApp.admin.invite.doneInviteTitle', { email: result.body.email })
                      : t('workspaceApp.admin.invite.donePasswordTitle', { name: result.body.displayName })}
                </DialogTitle>
                <DialogDescription className="mt-1 max-w-md text-[13px] text-muted-foreground">
                  {result.res.existingLogin
                    ? t('workspaceApp.admin.invite.doneExistingBody', { name: result.body.displayName })
                    : result.res.invitation
                      ? t('workspaceApp.admin.invite.doneInviteBody', { name: result.body.displayName })
                      : t('workspaceApp.admin.invite.donePasswordBody', { name: result.body.displayName, email: result.body.email })}
                </DialogDescription>
              </div>
              {result.res.invitation?.devAcceptUrl ? (
                <div className="px-5 pb-5">
                  <DevLink url={result.res.invitation.devAcceptUrl} label={t('workspaceApp.admin.invite.devLink')} />
                </div>
              ) : null}
            </div>
            <div className="flex flex-wrap justify-end gap-2 border-t px-5 py-3">
              <Button variant="outline" onClick={reset}>
                {t('workspaceApp.admin.invite.another')}
              </Button>
              <Button onClick={() => onOpenChange(false)}>{t('workspaceApp.admin.invite.done')}</Button>
            </div>
          </>
        ) : (
          <>
            <div className="border-b px-5 py-4 pr-12">
              <DialogTitle className="text-base font-semibold">{t('workspaceApp.admin.invite.title')}</DialogTitle>
              <DialogDescription className="text-[13px] text-muted-foreground">{t('workspaceApp.admin.limitNote')}</DialogDescription>
            </div>
            <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
              <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-5">
                {formError ? <Alert tone="danger">{formError}</Alert> : null}
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('workspaceApp.admin.invite.name')} error={errors.displayName}>
                    <Input value={d.displayName} onChange={(e) => patch({ displayName: e.target.value })} disabled={busy} autoComplete="off" />
                  </Field>
                  <Field label={t('workspaceApp.admin.invite.email')} error={errors.email}>
                    <Input type="email" inputMode="email" value={d.email} onChange={(e) => patch({ email: e.target.value })} disabled={busy} autoComplete="off" />
                  </Field>
                  <Field label={t('workspaceApp.admin.invite.phone')} error={errors.phone} hint={t('workspaceApp.admin.invite.optional')}>
                    <Input type="tel" inputMode="tel" value={d.phone} onChange={(e) => patch({ phone: e.target.value })} disabled={busy} autoComplete="off" />
                  </Field>
                  {userTypes.length ? (
                    <Field label={t('workspaceApp.admin.invite.userType')} error={errors.userType} hint={t('workspaceApp.admin.invite.userTypeHint')}>
                      <Select
                        value={d.userType}
                        onChange={(e) => {
                          const ut = userTypes.find((u) => u.key === e.target.value);
                          patch({ userType: e.target.value, roleKey: ut && !ut.allowedRoles.includes(d.roleKey) ? ut.allowedRoles[0] ?? '' : d.roleKey });
                        }}
                        disabled={busy}
                      >
                        <option value="">{t('workspaceApp.admin.invite.noUserType')}</option>
                        {userTypes.map((u) => (
                          <option key={u.key} value={u.key}>
                            {u.label}
                          </option>
                        ))}
                      </Select>
                    </Field>
                  ) : null}
                  <Field label={t('workspaceApp.admin.users.role')} error={errors.roleKey}>
                    <Select value={d.roleKey} onChange={(e) => patch({ roleKey: e.target.value })} disabled={busy}>
                      <option value="">{t('workspaceApp.admin.users.chooseRole')}</option>
                      {options.roles.filter((r) => !pickedType || pickedType.allowedRoles.includes(r.key)).map((r) => {
                        const beyond = limits.roleExceeds(r.key);
                        return (
                          <option key={r.key} value={r.key} disabled={beyond}>
                            {r.name}
                            {beyond ? ` — ${t('workspaceApp.admin.moreThanYours')}` : ''}
                          </option>
                        );
                      })}
                    </Select>
                  </Field>
                </div>

                {!options.isPlatform && options.products.length ? (
                  <fieldset>
                    <legend className="mb-2 text-[13px] font-medium text-foreground">{t('workspaceApp.admin.users.products')}</legend>
                    <div className="grid gap-2 sm:grid-cols-2">
                      {options.products.map((p) => (
                        <Checkbox
                          key={p.id}
                          className="rounded-md border px-3 py-2"
                          checked={d.productIds.includes(p.id)}
                          disabled={busy}
                          onCheckedChange={(c) => patch({ productIds: c ? [...d.productIds, p.id] : d.productIds.filter((x) => x !== p.id) })}
                          label={p.name}
                        />
                      ))}
                    </div>
                    {errors.productIds ? <p className="mt-1.5 text-[13px] text-danger">{errors.productIds}</p> : null}
                  </fieldset>
                ) : null}

                {options.permissionSets.length ? (
                  <fieldset>
                    <legend className="mb-2 text-[13px] font-medium text-foreground">{t('workspaceApp.admin.users.sets')}</legend>
                    <div className="space-y-2">
                      {options.permissionSets.map((s) => {
                        const beyond = limits.setExceeds(s.id);
                        return (
                          <Checkbox
                            key={s.id}
                            className="rounded-md border px-3 py-2"
                            checked={d.permissionSetIds.includes(s.id)}
                            disabled={busy || beyond}
                            onCheckedChange={(c) => patch({ permissionSetIds: c ? [...d.permissionSetIds, s.id] : d.permissionSetIds.filter((x) => x !== s.id) })}
                            label={s.name}
                            description={beyond ? t('workspaceApp.admin.moreThanYours') : s.description}
                          />
                        );
                      })}
                    </div>
                  </fieldset>
                ) : null}

                <fieldset className="space-y-3 rounded-lg border p-3">
                  <legend className="px-1 text-[13px] font-medium text-foreground">{t('workspaceApp.admin.invite.method')}</legend>
                  <SegmentedFilter
                    value={d.method}
                    onChange={(v) => patch({ method: v })}
                    options={[
                      { value: 'invite', label: t('workspaceApp.admin.invite.methodInvite') },
                      { value: 'password', label: t('workspaceApp.admin.invite.methodPassword') }
                    ]}
                  />
                  <p className={cn('text-xs text-muted-foreground')}>
                    {d.method === 'invite' ? t('workspaceApp.admin.invite.methodInviteHint') : t('workspaceApp.admin.invite.methodPasswordHint')}
                  </p>
                  {d.method === 'password' ? (
                    <Field label={t('workspaceApp.admin.invite.password')} error={errors.password}>
                      <PasswordInput value={d.password} onChange={(e) => patch({ password: e.target.value })} disabled={busy} autoComplete="new-password" />
                    </Field>
                  ) : null}
                  {d.method === 'password' && d.password ? <StrengthMeter password={d.password} /> : null}
                </fieldset>
              </div>
              <div className="flex flex-col-reverse gap-2 border-t bg-muted/30 px-5 py-3 sm:flex-row sm:justify-end">
                <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
                  {t('common.cancel')}
                </Button>
                <Button type="submit" loading={busy}>
                  {d.method === 'invite' ? t('workspaceApp.admin.invite.send') : t('workspaceApp.admin.invite.create')}
                </Button>
              </div>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
