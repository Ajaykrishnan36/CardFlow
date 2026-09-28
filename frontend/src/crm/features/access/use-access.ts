import { useCallback } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { accessApi, productsApi } from '@crm/api/endpoints';
import type { AccessCatalog, AccessRules, AccessWorkspaceOption, EffectiveAccess, ObjectAction, RoleKey, RowScope, SystemRoleKey, WorkspaceRole } from '@crm/api/types';

export const ROLE_KEYS: SystemRoleKey[] = ['SUPER_ADMIN', 'ADMIN', 'STAFF', 'END_USER'];
const ACTION_ORDER: ObjectAction[] = ['read', 'create', 'update', 'delete', 'convert', 'export'];

export const accessKeys = {
  all: ['access'] as const,
  catalog: ['access', 'catalog'] as const,
  workspaces: ['access', 'workspaces'] as const,
  permissionSets: (workspaceId: string) => ['access', 'permission-sets', workspaceId] as const,
  roles: (workspaceId: string) => ['access', 'roles', workspaceId] as const
};

/** Capabilities that let a member administer their own workspace (within their own access). */
export const DELEGATION_CAPABILITIES = ['access.manage', 'members.manage'];

/** Objects, actions, capabilities and the built-in roles. Rarely changes. */
/** Owner-only endpoint: pass enabled=false when a catalog is already in hand (e.g. workspace admins). */
export function useAccessCatalog(enabled = true) {
  return useQuery({ queryKey: accessKeys.catalog, queryFn: accessApi.catalog, staleTime: 10 * 60_000, enabled });
}

/** Every active workspace (platform first) with its products and permission sets. */
export function useAccessWorkspaces() {
  return useQuery({ queryKey: accessKeys.workspaces, queryFn: accessApi.workspaces, staleTime: 30_000 });
}

export function usePermissionSets(workspaceId: string | undefined) {
  return useQuery({
    queryKey: accessKeys.permissionSets(workspaceId ?? ''),
    queryFn: () => accessApi.permissionSets(workspaceId ?? ''),
    enabled: Boolean(workspaceId)
  });
}

/** Built-in + custom roles of a workspace, with their (possibly edited) rules. */
export function useWorkspaceRoles(workspaceId: string | undefined) {
  return useQuery({
    queryKey: accessKeys.roles(workspaceId ?? ''),
    queryFn: () => accessApi.roles(workspaceId ?? ''),
    enabled: Boolean(workspaceId),
    staleTime: 30_000
  });
}

export interface RoleOption {
  key: RoleKey;
  name: string;
  description?: string;
  isSystem: boolean;
  customized: boolean;
  rules?: AccessRules;
}

/**
 * Roles to offer for a workspace: its full role list when loaded, else the picker
 * summary from accessApi.workspaces, else (new workspace) the catalog's built-in roles.
 */
export function roleOptionsFor(roles: WorkspaceRole[] | undefined, ws: AccessWorkspaceOption | undefined, catalog: AccessCatalog | undefined): RoleOption[] {
  if (roles?.length) {
    return roles.map((r) => ({ key: r.key, name: r.name, description: r.description, isSystem: r.isSystem, customized: r.customized, rules: r.rules }));
  }
  if (ws?.roles?.length) {
    return ws.roles.map((r) => {
      const def = catalog?.roles.find((c) => c.key === r.key);
      return { key: r.key, name: r.name, description: def?.description, isSystem: r.isSystem, customized: false };
    });
  }
  return (catalog?.roles ?? []).map((r) => ({ key: r.key, name: r.name, description: r.description, isSystem: true, customized: false, rules: r.rules }));
}

/** Same key as the provisioning wizard so the list is shared. */
export function useActiveProducts(enabled = true) {
  return useQuery({ queryKey: ['products', { q: '', status: 'active' }], queryFn: () => productsApi.list({ status: 'active' }), enabled });
}

/**
 * After a permission set changes: pickers, set lists and every open user
 * (their effective access may include that set) are stale.
 */
export function useInvalidateAccess() {
  const qc = useQueryClient();
  return useCallback(() => {
    void qc.invalidateQueries({ queryKey: accessKeys.all });
    void qc.invalidateQueries({ queryKey: ['platform', 'user'] });
    void qc.invalidateQueries({ queryKey: ['platform', 'users'] });
    void qc.invalidateQueries({ queryKey: ['workspace'] });
  }, [qc]);
}

export function workspaceLabel(ws: Pick<AccessWorkspaceOption, 'name' | 'isPlatform'>, platformLabel: string): string {
  return ws.isPlatform ? platformLabel : ws.name;
}

// ---------------------------------------------------------------------------
// Rules helpers
// ---------------------------------------------------------------------------

export const emptyRules = (): AccessRules => ({ objects: {}, rows: {}, capabilities: [] });

/**
 * Canonical form: only supported actions, Read implied by any other action,
 * a row scope for every object that has actions, sorted for stable comparison.
 */
export function normalizeRules(rules: AccessRules | undefined, catalog?: AccessCatalog): AccessRules {
  const src = rules ?? emptyRules();
  const order = catalog ? catalog.actions.map((a) => a.key) : ACTION_ORDER;
  const keys = catalog ? catalog.objects.map((o) => o.key) : Object.keys(src.objects ?? {});
  const objects: Record<string, ObjectAction[]> = {};
  const rows: Record<string, { scope: RowScope }> = {};
  for (const key of keys) {
    const supported = catalog?.objects.find((o) => o.key === key)?.actions;
    let acts = (src.objects?.[key] ?? []).filter((a) => !supported || supported.includes(a));
    if (acts.length === 0) continue;
    if (!acts.includes('read')) acts = ['read', ...acts];
    objects[key] = Array.from(new Set(acts)).sort((a, b) => order.indexOf(a) - order.indexOf(b));
    rows[key] = { scope: src.rows?.[key]?.scope === 'workspace' ? 'workspace' : 'own' };
  }
  const caps = Array.from(new Set(src.capabilities ?? [])).filter((c) => !catalog || catalog.capabilities.some((x) => x.key === c));
  return { objects, rows, capabilities: caps.sort() };
}

export function rulesEqual(a: AccessRules, b: AccessRules, catalog?: AccessCatalog): boolean {
  return JSON.stringify(normalizeRules(a, catalog)) === JSON.stringify(normalizeRules(b, catalog));
}

export function hasAnyGrant(rules: AccessRules): boolean {
  return Object.values(rules.objects ?? {}).some((a) => a.length > 0) || (rules.capabilities ?? []).length > 0;
}

/** "Leads: read, create · Accounts: read · View dashboard" */
export function summarizeRules(rules: AccessRules, catalog: AccessCatalog): string {
  const parts: string[] = [];
  const n = normalizeRules(rules, catalog);
  const actionLabel = (k: ObjectAction) => (catalog.actions.find((a) => a.key === k)?.label ?? k).toLowerCase();
  for (const obj of catalog.objects) {
    const acts = n.objects[obj.key];
    if (!acts?.length) continue;
    parts.push(`${pluralLabel(obj.label)}: ${acts.map(actionLabel).join(', ')}`);
  }
  for (const cap of catalog.capabilities) if (n.capabilities.includes(cap.key)) parts.push(cap.label);
  return parts.join(' · ');
}

/** Catalog labels are singular ("Lead"); lists read better plural. */
export function pluralLabel(label: string): string {
  if (/s$/i.test(label)) return label;
  if (/[^aeiou]y$/i.test(label)) return `${label.slice(0, -1)}ies`;
  return `${label}s`;
}

// ---------------------------------------------------------------------------
// Client-side preview of effective access (mirrors the server's union rule)
// ---------------------------------------------------------------------------

export interface RuleSource {
  label: string;
  rules: AccessRules;
}

/**
 * Union of all sources (broadest scope wins), limited by modules. Used to preview
 * unsaved role / permission-set / product changes; the server stays authoritative.
 */
export function computeEffective(
  catalog: AccessCatalog,
  sources: RuleSource[],
  moduleEnabled: (module: string, objectKey: string) => boolean,
  products: EffectiveAccess['products'],
  modules: string[]
): EffectiveAccess {
  const objects: EffectiveAccess['objects'] = {};
  for (const obj of catalog.objects) {
    const acts = new Set<ObjectAction>();
    let scope: RowScope = 'own';
    const from: string[] = [];
    for (const s of sources) {
      const n = normalizeRules(s.rules, catalog);
      const a = n.objects[obj.key];
      if (!a?.length) continue;
      a.forEach((x) => acts.add(x));
      if (n.rows[obj.key]?.scope === 'workspace') scope = 'workspace';
      from.push(s.label);
    }
    const enabled = moduleEnabled(obj.module, obj.key);
    objects[obj.key] = {
      actions: enabled ? ACTION_ORDER.filter((a) => acts.has(a)) : [],
      scope,
      sources: from,
      moduleEnabled: enabled
    };
  }
  const capabilities: EffectiveAccess['capabilities'] = [];
  for (const cap of catalog.capabilities) {
    const from = sources.filter((s) => (s.rules.capabilities ?? []).includes(cap.key)).map((s) => s.label);
    if (from.length) capabilities.push({ key: cap.key, sources: from });
  }
  return { objects, capabilities, products, modules };
}
