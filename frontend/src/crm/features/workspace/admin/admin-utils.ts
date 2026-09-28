import { useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { workspaceAdminApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessCatalog, AccessRules, ObjectAction, WorkspaceContext } from '@crm/api/types';
import { normalizeRules, pluralLabel } from '@crm/features/access/use-access';

// Delegated administration inside a workspace: a member with access.manage /
// members.manage may only hand out what they have themselves (`grantable`).
// The server enforces it (403 exceeds_your_access); this mirrors it in the UI.

export const adminKeys = {
  all: (code: string) => ['workspace', code, 'admin'] as const,
  options: (code: string) => ['workspace', code, 'admin', 'options'] as const,
  members: (code: string) => ['workspace', code, 'admin', 'members'] as const
};

export function useAdminOptions(code: string) {
  return useQuery({ queryKey: adminKeys.options(code), queryFn: () => workspaceAdminApi(code).options(), staleTime: 30_000 });
}

export function useAdminMembers(code: string, enabled: boolean) {
  return useQuery({ queryKey: adminKeys.members(code), queryFn: () => workspaceAdminApi(code).members(), enabled });
}

/** After roles / sets / members change: admin lists, the member's own context (nav, access) and records. */
export function useInvalidateAdmin(code: string) {
  const qc = useQueryClient();
  return useCallback(() => {
    void qc.invalidateQueries({ queryKey: adminKeys.all(code) });
    void qc.invalidateQueries({ queryKey: ['workspace', code, 'context'] });
  }, [qc, code]);
}

export function hasCapability(ctx: WorkspaceContext, key: string): boolean {
  return ctx.effective.capabilities.some((c) => c.key === key);
}

export function canAdminister(ctx: WorkspaceContext): boolean {
  return hasCapability(ctx, 'access.manage') || hasCapability(ctx, 'members.manage') || ctx.navigation.some((n) => n.key === 'admin');
}

// ---- Grantable limits ------------------------------------------------------

export function canGrantAction(grantable: AccessRules, objKey: string, action: ObjectAction): boolean {
  const acts = grantable.objects?.[objKey] ?? [];
  return acts.includes(action) || (action === 'read' && acts.length > 0);
}

export function canGrantWorkspaceScope(grantable: AccessRules, objKey: string): boolean {
  return grantable.rows?.[objKey]?.scope === 'workspace';
}

export function canGrantCapability(grantable: AccessRules, key: string): boolean {
  return (grantable.capabilities ?? []).includes(key);
}

/** Human labels of every grant in `rules` that goes beyond `grantable` ("Leads: Delete", "Leads: all records", "Manage users"). */
export function exceedingGrants(rules: AccessRules | undefined, grantable: AccessRules, catalog: AccessCatalog, allRecordsLabel: string): string[] {
  const n = normalizeRules(rules, catalog);
  const out: string[] = [];
  for (const obj of catalog.objects) {
    const acts = n.objects[obj.key] ?? [];
    const plural = pluralLabel(obj.label);
    for (const a of acts) {
      if (!canGrantAction(grantable, obj.key, a)) out.push(`${plural}: ${catalog.actions.find((x) => x.key === a)?.label ?? a}`);
    }
    if (acts.length && n.rows[obj.key]?.scope === 'workspace' && !canGrantWorkspaceScope(grantable, obj.key)) out.push(`${plural}: ${allRecordsLabel}`);
  }
  for (const c of n.capabilities) if (!canGrantCapability(grantable, c)) out.push(catalog.capabilities.find((x) => x.key === c)?.label ?? c);
  return out;
}

export function withinGrantable(rules: AccessRules | undefined, grantable: AccessRules, catalog: AccessCatalog): boolean {
  return exceedingGrants(rules, grantable, catalog, '').length === 0;
}

/** Server error → one line. exceeds_your_access / role_in_use messages name the problem, so show them as-is. */
export function adminErrorMessage(e: unknown, t: TFunction): string {
  if (!isApiError(e)) return t('common.genericError');
  if (e.code === 'exceeds_your_access' || e.code === 'role_in_use') return e.message;
  if (e.status === 403) return e.message || t('workspaceApp.admin.forbidden');
  return e.message || t('common.genericError');
}
