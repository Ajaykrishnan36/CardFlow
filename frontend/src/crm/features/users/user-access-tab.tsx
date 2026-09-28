import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useMutation, useQueries, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Building2, Layers, Lock, MailPlus, Plus } from 'lucide-react';
import { productsApi, usersApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessCatalog, AccessWorkspaceOption, EffectiveAccess, MembershipAccessBody, PermissionSet, RoleKey, UserDetail, UserMembership } from '@crm/api/types';
import { Alert, Badge, Card } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Checkbox, Switch } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { ConfirmDialog } from '@crm/components/page';
import { EmptyState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { WorkspaceTile } from '@crm/features/workspaces/workspace-ui';
import { EffectiveAccessMatrix } from '@crm/features/access/effective-access';
import { PermissionSetDialog } from '@crm/features/access/permission-set-dialog';
import { RoleField } from '@crm/features/access/role-permissions';
import { accessKeys, computeEffective, useAccessCatalog, useAccessWorkspaces, usePermissionSets, useWorkspaceRoles, type RuleSource } from '@crm/features/access/use-access';
import { inviteStatusTone, membershipStatusTone } from './user-format';
import { useApplyUser } from './use-user-mutations';
import { AddMembershipDialog } from './add-membership-dialog';

export function UserAccessTab({ user }: { user: UserDetail }) {
  return (
    <div className="space-y-6">
      <MembershipsSection user={user} />
      <InvitationsCard user={user} />
    </div>
  );
}

const sorted = (ids: string[]) => [...ids].sort();
const sameIds = (a: string[], b: string[]) => JSON.stringify(sorted(a)) === JSON.stringify(sorted(b));

function MembershipsSection({ user }: { user: UserDetail }) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const workspaces = useAccessWorkspaces();
  const [adding, setAdding] = useState(false);
  const memberOf = new Set(user.memberships.map((m) => m.workspaceId));
  const available = (workspaces.data ?? []).filter((w) => !memberOf.has(w.id));
  const canAdd = user.status !== 'deleted' && workspaces.data !== undefined && available.length > 0;

  const addButton = (
    <Button size="sm" onClick={() => setAdding(true)} disabled={!canAdd}>
      <Plus /> {t('users.access.add')}
    </Button>
  );

  return (
    <section className="space-y-4" aria-labelledby="user-access-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 id="user-access-title" className="text-[15px] font-semibold text-foreground">
            {t('users.access.title')}
          </h2>
          <p className="mt-0.5 text-[13px] text-muted-foreground">{t('users.access.description')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/crm/owner/users?tab=permission-sets">
              <Layers /> {t('users.access.managePermissionSets')}
            </Link>
          </Button>
          {workspaces.data && available.length === 0 && user.memberships.length > 0 ? (
            <Tooltip content={t('users.access.addNone')} side="top">
              <span tabIndex={0}>{addButton}</span>
            </Tooltip>
          ) : (
            addButton
          )}
        </div>
      </div>

      <Alert tone="info" title={t('users.access.howTitle')}>
        {t('users.access.howBody')}
      </Alert>

      {user.memberships.length === 0 ? (
        <Card>
          <EmptyState icon={Building2} title={t('users.access.noMemberships')} body={t('users.access.noMembershipsBody')} action={canAdd ? addButton : undefined} className="py-10" />
        </Card>
      ) : (
        user.memberships.map((m) => (
          <MembershipCard
            // Remount with the server's copy after a save so the draft resets to it.
            key={`${m.id}:${m.roleKey ?? ''}:${sorted(m.productIds ?? []).join(',')}:${sorted((m.permissionSets ?? []).map((s) => s.id)).join(',')}`}
            user={user}
            m={m}
            catalog={catalog.data}
            wsOption={workspaces.data?.find((w) => w.id === m.workspaceId)}
          />
        ))
      )}

      <AddMembershipDialog open={adding} onOpenChange={setAdding} user={user} available={available} />
    </section>
  );
}

function MembershipCard({ user, m, catalog, wsOption }: { user: UserDetail; m: UserMembership; catalog?: AccessCatalog; wsOption?: AccessWorkspaceOption }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const apply = useApplyUser(user.id);
  const wsRoles = useWorkspaceRoles(m.workspaceId);
  const sets = usePermissionSets(m.workspaceId);

  const savedRole = m.roleKey;
  const savedProducts = m.productIds ?? [];
  const savedSets = (m.permissionSets ?? []).map((s) => s.id);

  const [roleKey, setRoleKey] = useState<RoleKey | undefined>(savedRole);
  const [productIds, setProductIds] = useState<string[]>(savedProducts);
  const [setIds, setSetIds] = useState<string[]>(savedSets);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [newSet, setNewSet] = useState(false);
  const [confirmSuspend, setConfirmSuspend] = useState(false);
  const [extraSets, setExtraSets] = useState<PermissionSet[]>([]);

  const isPlatform = m.isPlatformWorkspace;
  const locked = user.isPlatformOwner && isPlatform;
  const revoked = m.status === 'revoked';
  const readOnly = locked || revoked;

  const roleChanged = roleKey !== savedRole;
  const productsChanged = !isPlatform && !sameIds(productIds, savedProducts);
  const setsChanged = !sameIds(setIds, savedSets);
  const dirty = !readOnly && (roleChanged || productsChanged || setsChanged);

  const invalidateRelated = () => {
    void qc.invalidateQueries({ queryKey: accessKeys.permissionSets(m.workspaceId) });
    void qc.invalidateQueries({ queryKey: accessKeys.workspaces });
    void qc.invalidateQueries({ queryKey: ['workspace', m.workspaceId] });
  };
  const onError = (e: unknown) => {
    if (isApiError(e) && Object.keys(e.fieldErrors).length > 0) {
      setErrors(e.fieldErrors);
      toast.error(t('users.access.fixErrors'));
      return;
    }
    // 403 platform_owner and friends carry a readable message.
    toast.error(isApiError(e) ? e.message : t('common.genericError'));
  };

  const save = useMutation({
    mutationFn: () => {
      const body: MembershipAccessBody = {};
      if (roleChanged && roleKey) body.roleKey = roleKey;
      if (productsChanged) body.productIds = productIds;
      if (setsChanged) body.permissionSetIds = setIds;
      return usersApi.updateMembership(user.id, m.id, body);
    },
    onSuccess: (u) => {
      setErrors({});
      apply(u);
      invalidateRelated();
      toast.success(t('users.access.saved', { workspace: m.workspaceName }));
    },
    onError
  });

  const status = useMutation({
    mutationFn: (next: 'active' | 'suspended') => usersApi.updateMembership(user.id, m.id, { status: next }),
    onSuccess: (u, next) => {
      apply(u);
      invalidateRelated();
      setConfirmSuspend(false);
      toast.success(next === 'suspended' ? t('users.access.suspended') : t('users.access.restored'));
    },
    onError: (e) => {
      setConfirmSuspend(false);
      onError(e);
    }
  });

  const cancel = () => {
    setRoleKey(savedRole);
    setProductIds(savedProducts);
    setSetIds(savedSets);
    setErrors({});
  };

  // Products the workspace offers (fallback: what the membership already has).
  const availableProducts = wsOption?.products ?? m.effective?.products ?? [];

  // Permission sets of the workspace, plus any just created or already assigned.
  const setOptions = useMemo(() => {
    const list: Array<{ id: string; name: string }> = [...(sets.data ?? [])];
    for (const s of [...extraSets, ...(m.permissionSets ?? [])]) if (!list.some((x) => x.id === s.id)) list.push(s);
    return list;
  }, [sets.data, extraSets, m.permissionSets]);

  // Modules of the drafted products (only needed to preview a product change).
  const productDetails = useQueries({
    queries: (productsChanged ? productIds : []).map((id) => ({ queryKey: ['product', id], queryFn: () => productsApi.get(id), staleTime: 60_000 }))
  });
  const modulesLoading = productDetails.some((q) => q.isLoading);

  const preview = useMemo<{ effective: EffectiveAccess; note?: string } | null>(() => {
    if (!dirty || !catalog) return null;
    // The workspace's own (possibly edited) role rules; wait for them rather than guess.
    if (!wsRoles.data) return null;
    const role = wsRoles.data.find((r) => r.key === roleKey);
    const fullSets = [...(sets.data ?? []), ...extraSets];
    const chosen = setIds.map((id) => fullSets.find((s) => s.id === id));
    if (chosen.some((s) => !s)) return null; // rules not loaded yet
    const sources: RuleSource[] = [];
    if (role) sources.push({ label: t('access.effective.sourceRole', { name: role.name }), rules: role.rules });
    for (const s of chosen) if (s) sources.push({ label: t('access.effective.sourceSet', { name: s.name }), rules: s.rules });

    const saved = m.effective;
    let modules: string[] = saved?.modules ?? [];
    let note: string | undefined;
    if (productsChanged) {
      if (modulesLoading) note = t('access.effective.modulesPending');
      else {
        const mods = new Set<string>();
        for (const q of productDetails) (q.data?.publishedConfig?.modules ?? q.data?.draftConfig.modules ?? []).forEach((x) => mods.add(x));
        modules = Array.from(mods);
      }
    }
    const enabled = (module: string, key: string) => {
      if (isPlatform) return true;
      if (productsChanged && !modulesLoading) return modules.includes(module);
      return saved?.objects?.[key]?.moduleEnabled ?? modules.includes(module);
    };
    const products = isPlatform ? [] : availableProducts.filter((p) => productIds.includes(p.id));
    return { effective: computeEffective(catalog, sources, enabled, products, modules), note };
  }, [dirty, catalog, wsRoles.data, roleKey, sets.data, extraSets, setIds, m.effective, productsChanged, modulesLoading, productDetails, isPlatform, availableProducts, productIds, t]);

  const toggle = (list: string[], id: string, on: boolean) => (on ? Array.from(new Set([...list, id])) : list.filter((x) => x !== id));

  const statusControl =
    m.status === 'invited' || m.status === 'revoked' ? (
      <Badge tone={membershipStatusTone[m.status]}>{t(`users.status.${m.status}`)}</Badge>
    ) : (
      <span className="inline-flex items-center gap-2">
        <span className={cn('text-[13px]', m.status === 'active' ? 'text-foreground' : 'text-warning')}>{t(`users.status.${m.status}`)}</span>
        <Switch
          checked={m.status === 'active'}
          onCheckedChange={(on) => (on ? status.mutate('active') : setConfirmSuspend(true))}
          disabled={locked || status.isPending}
          aria-label={t('users.access.statusFor', { workspace: m.workspaceName })}
        />
      </span>
    );

  return (
    <Card className="min-w-0">
      {/* Header */}
      <div className="flex flex-wrap items-center gap-3 border-b px-5 py-3.5">
        <WorkspaceTile name={m.workspaceName} size="sm" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <Link to={`/crm/owner/workspaces/${m.workspaceId}`} className="truncate text-[14px] font-semibold text-foreground hover:underline">
              {m.workspaceName}
            </Link>
            {isPlatform ? <Badge tone="warning">{t('users.badge.platform')}</Badge> : null}
            {locked ? (
              <Tooltip content={t('users.access.lockedShort')} side="top">
                <span tabIndex={0} className="text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  <Lock className="size-3.5" aria-label={t('users.access.lockedShort')} />
                </span>
              </Tooltip>
            ) : null}
          </div>
          <p className="text-xs text-muted-foreground">
            <span className="font-mono">{m.workspaceCode}</span> · {t('users.access.joined', { time: relativeTime(m.createdAt) })}
          </p>
        </div>
        {statusControl}
      </div>

      {locked ? (
        <div className="px-5 pt-4">
          <Alert tone="info">{t('users.access.locked')}</Alert>
        </div>
      ) : revoked ? (
        <div className="px-5 pt-4">
          <Alert tone="warning">{t('users.access.revokedNote')}</Alert>
        </div>
      ) : null}

      {/* Body */}
      <div className="grid gap-6 px-5 py-4 xl:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
        <div className="min-w-0 space-y-5">
          <RoleField
            workspaceId={m.workspaceId}
            label={t('users.access.role')}
            linkLabel={t('access.roles.viewPermissions')}
            value={roleKey}
            onChange={setRoleKey}
            error={errors.roleKey}
            disabled={readOnly || save.isPending}
            aria-label={t('users.access.roleFor', { workspace: m.workspaceName })}
          />

          {!isPlatform ? (
            <fieldset disabled={readOnly || save.isPending}>
              <legend className="mb-2 text-[13px] font-medium text-foreground">{t('users.access.products')}</legend>
              {availableProducts.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('users.access.noProducts')}</p>
              ) : (
                <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
                  {availableProducts.map((p) => (
                    <Checkbox key={p.id} label={p.name} checked={productIds.includes(p.id)} onCheckedChange={(on) => setProductIds((ids) => toggle(ids, p.id, on))} />
                  ))}
                </div>
              )}
              {errors.productIds ? (
                <p className="mt-1.5 text-[13px] text-danger">{errors.productIds}</p>
              ) : availableProducts.length > 0 && productIds.length === 0 ? (
                <p className="mt-1.5 text-xs font-medium text-warning">{t('users.access.noProductsWarning')}</p>
              ) : (
                <p className="mt-1.5 text-xs text-muted-foreground">{t('users.access.productsHint')}</p>
              )}
            </fieldset>
          ) : null}

          <fieldset disabled={readOnly || save.isPending}>
            <div className="mb-2 flex items-center justify-between gap-2">
              <legend className="text-[13px] font-medium text-foreground">{t('users.access.permissionSets')}</legend>
              {!readOnly ? (
                <button
                  type="button"
                  onClick={() => setNewSet(true)}
                  className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <Plus className="size-3.5" aria-hidden /> {t('users.access.newSet')}
                </button>
              ) : null}
            </div>
            {sets.isLoading ? (
              <div className="grid gap-2 sm:grid-cols-2">
                <Skeleton className="h-8" />
                <Skeleton className="h-8" />
              </div>
            ) : setOptions.length === 0 ? (
              <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">{t('users.access.noSets')}</p>
            ) : (
              <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
                {setOptions.map((s) => (
                  <Checkbox key={s.id} label={s.name} checked={setIds.includes(s.id)} onCheckedChange={(on) => setSetIds((ids) => toggle(ids, s.id, on))} />
                ))}
              </div>
            )}
            {errors.permissionSetIds ? (
              <p className="mt-1.5 text-[13px] text-danger">{errors.permissionSetIds}</p>
            ) : (
              <p className="mt-1.5 text-xs text-muted-foreground">{t('users.access.permissionSetsHint')}</p>
            )}
          </fieldset>
        </div>

        <EffectiveAccessMatrix
          className="min-w-0"
          catalog={catalog}
          effective={preview?.effective ?? m.effective}
          isPlatform={isPlatform}
          preview={Boolean(preview)}
          note={preview?.note}
        />
      </div>

      {dirty ? (
        <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-2 rounded-b-lg border-t bg-card/95 px-5 py-2.5 backdrop-blur animate-slide-up">
          <span className="flex items-center gap-2 text-[13px] font-medium text-foreground">
            <span className="size-2 rounded-full bg-warning" aria-hidden />
            {t('users.access.unsaved')}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={cancel} disabled={save.isPending}>
              {t('users.access.cancel')}
            </Button>
            <Button size="sm" onClick={() => save.mutate()} loading={save.isPending} disabled={!roleKey}>
              {t('users.access.save')}
            </Button>
          </div>
        </div>
      ) : null}

      {newSet ? (
        <PermissionSetDialog
          open
          onOpenChange={(o) => !o && setNewSet(false)}
          workspaceId={m.workspaceId}
          workspaceName={m.workspaceName}
          onSaved={(set) => {
            setExtraSets((xs) => [...xs, set]);
            setSetIds((ids) => toggle(ids, set.id, true));
          }}
        />
      ) : null}
      <ConfirmDialog
        open={confirmSuspend}
        onOpenChange={(o) => !status.isPending && setConfirmSuspend(o)}
        title={t('users.access.suspendTitle', { workspace: m.workspaceName })}
        body={t('users.access.suspendBody', { name: user.displayName })}
        confirmLabel={t('users.access.suspendConfirm')}
        cancelLabel={t('common.cancel')}
        tone="danger"
        loading={status.isPending}
        onConfirm={() => status.mutate('suspended')}
      />
    </Card>
  );
}

function InvitationsCard({ user }: { user: UserDetail }) {
  const { t } = useTranslation();
  return (
    <Card className="min-w-0">
      <div className="border-b px-5 py-4">
        <h2 className="text-[15px] font-semibold leading-6 text-foreground">{t('users.access.invitationsTitle')}</h2>
        <p className="mt-0.5 text-[13px] text-muted-foreground">{t('users.access.invitationsDescription')}</p>
      </div>
      {user.invitations.length === 0 ? (
        <EmptyState icon={MailPlus} title={t('users.access.noInvitations')} className="py-8" />
      ) : (
        <ul className="divide-y">
          {user.invitations.map((inv) => {
            const expired = new Date(inv.expiresAt).getTime() < Date.now();
            const when =
              inv.status === 'accepted' && inv.acceptedAt
                ? t('users.access.accepted', { time: relativeTime(inv.acceptedAt) })
                : expired || inv.status === 'expired'
                  ? t('users.access.expired', { time: relativeTime(inv.expiresAt) })
                  : t('users.access.expires', { time: relativeTime(inv.expiresAt) });
            return (
              <li key={inv.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 py-3">
                <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
                  <MailPlus className="size-4" aria-hidden />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-medium text-foreground">{inv.workspaceName}</p>
                  <p className="text-xs text-muted-foreground">
                    {inv.roleName || t(`users.roles.${inv.roleKey}`, { defaultValue: inv.roleKey })} ·{' '}
                    <Tooltip content={new Date(inv.expiresAt).toLocaleString('en-IN')} side="top">
                      <span>{when}</span>
                    </Tooltip>
                  </p>
                </div>
                <Badge tone={inviteStatusTone[inv.status] ?? 'neutral'}>{t(`users.inviteStatus.${inv.status}`, { defaultValue: inv.status })}</Badge>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
