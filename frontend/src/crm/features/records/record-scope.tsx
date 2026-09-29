import { createContext, useContext, type ReactNode } from 'react';
import { recordsApi, recordsApiFor, type RecordsApi } from '@crm/api/endpoints';
import type { LookupTarget, ObjectAction, ObjectKey, RowScope, WorkspaceContext } from '@crm/api/types';

// Who is looking at the metadata-driven record pages, and what they may do there.
// The owner works the Platform CRM (/platform, everything allowed); a workspace
// member works /w/<code> and sees only what role + permission sets + products allow.
// The server enforces the same rules — this only decides what to render.

export type RecordAudience = 'owner' | 'member';

export interface RecordScope {
  audience: RecordAudience;
  /** API prefix: '/platform' or `/w/<code>`. Part of every record query key. */
  prefix: string;
  /** Route prefix of the record pages: '/crm/owner' or `/crm/w/<code>`. */
  routeBase: string;
  api: RecordsApi;
  can: (object: ObjectKey, action: ObjectAction) => boolean;
  /** Row visibility per object (member only; the owner sees everything). */
  rowScope: (object: ObjectKey) => RowScope | undefined;
  /** May edit page layouts and custom fields. */
  canCustomize: boolean;
  /** Query key of this audience's dashboard, refreshed after record changes. */
  dashboardKey: readonly unknown[];
}

/** Route/API plural ↔ permission singular (objects defined as data use their own key). */
const builtinPermission: Record<string, string> = { leads: 'lead', accounts: 'account', contacts: 'contact' };
export function permissionObject(object: ObjectKey): string {
  return builtinPermission[object] ?? object;
}

export const ownerScope: RecordScope = {
  audience: 'owner',
  prefix: '/platform',
  routeBase: '/crm/owner',
  api: recordsApi,
  can: () => true,
  rowScope: () => undefined,
  canCustomize: true,
  dashboardKey: ['platform', 'dashboard']
};

export function workspaceDashboardKey(code: string) {
  return ['workspace', code, 'dashboard'] as const;
}

/** Scope of a workspace member, from GET /w/{code}/context. */
export function scopeFromContext(ctx: WorkspaceContext, code: string): RecordScope {
  const prefix = `/w/${encodeURIComponent(code)}`;
  const access = (object: ObjectKey) => ctx.effective.objects[permissionObject(object)];
  // The platform owner opening a workspace from the console has full access (server-audited).
  const full = Boolean(ctx.viewerIsOwner);
  return {
    audience: 'member',
    prefix,
    routeBase: `/crm/w/${encodeURIComponent(code)}`,
    api: recordsApiFor(prefix),
    can: (object, action) => {
      if (full) return true;
      const a = access(object);
      return Boolean(a && a.moduleEnabled && a.actions.includes(action));
    },
    rowScope: (object) => access(object)?.scope,
    canCustomize: ctx.canCustomize || full,
    dashboardKey: workspaceDashboardKey(code)
  };
}

const ScopeContext = createContext<RecordScope>(ownerScope);

export function RecordScopeProvider({ value, children }: { value: RecordScope; children: ReactNode }) {
  return <ScopeContext.Provider value={value}>{children}</ScopeContext.Provider>;
}

/** Defaults to the owner scope, so routes without a provider keep their behaviour. */
export function useRecordScope(): RecordScope {
  return useContext(ScopeContext);
}

export function recordHref(scope: RecordScope, object: ObjectKey, id: string): string {
  return `${scope.routeBase}/${object}/${encodeURIComponent(id)}`;
}

export function listHref(scope: RecordScope, object: ObjectKey): string {
  return `${scope.routeBase}/${object}`;
}

export function layoutHref(scope: RecordScope, object: ObjectKey): string {
  return `${scope.routeBase}/setup/${object}/layout`;
}

const NON_RECORD_TARGETS: readonly string[] = ['users', 'workspaces', 'products'];

/**
 * Page of a lookup target in this scope, or null. Members never get owner-console
 * links (users / workspaces / products) and only link records they can read.
 */
export function scopedLookupHref(scope: RecordScope, target: LookupTarget | string | undefined, id: string | undefined): string | null {
  if (!target || !id) return null;
  if (!NON_RECORD_TARGETS.includes(target)) {
    return scope.can(target, 'read') ? recordHref(scope, target, id) : null;
  }
  if (scope.audience !== 'owner') return null;
  if (target === 'users' || target === 'workspaces' || target === 'products') return `/crm/owner/${target}/${encodeURIComponent(id)}`;
  return null;
}

/** Whether a lookup picker for `target` works in this scope (members can't search workspaces/products). */
export function canLookup(scope: RecordScope, target: LookupTarget): boolean {
  if (scope.audience === 'owner') return true;
  if (target === 'workspaces' || target === 'products') return false;
  if (!NON_RECORD_TARGETS.includes(target)) return scope.can(target, 'read');
  return true;
}
