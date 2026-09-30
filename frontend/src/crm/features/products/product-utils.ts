import type { ProductConfig, ProductDetail, ProductRole, ProductStatus, SystemRoleKey } from '@crm/api/types';
import { ACCENT_COLORS } from './product-icon';

export const SYSTEM_ROLES: SystemRoleKey[] = ['SUPER_ADMIN', 'ADMIN', 'STAFF', 'END_USER'];

export const productStatusTone: Record<ProductStatus, 'neutral' | 'success' | 'warning'> = {
  draft: 'neutral',
  active: 'success',
  archived: 'warning'
};

export const PRODUCT_KEY_RE = /^[a-z][a-z0-9_]{2,40}$/;

/** "Real Estate CRM" → "real_estate_crm" (lowercase, starts with a letter, ≤40 chars). */
export function slugKey(input: string): string {
  return input
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^[^a-z]+/, '')
    .replace(/_+$/, '')
    .slice(0, 40)
    .replace(/_+$/, '');
}

/**
 * Keeps an auto-derived key in sync with its label until the user edits the key:
 * if the key still equals the slug of the old label (or is empty), follow the new label.
 */
export function syncedKey(currentKey: string, oldLabel: string, newLabel: string): string {
  return !currentKey || currentKey === slugKey(oldLabel) ? slugKey(newLabel) : currentKey;
}

export function nextVersion(p: Pick<ProductDetail, 'currentVersion'>): number {
  return (p.currentVersion ?? 0) + 1;
}

export function canPublish(p: Pick<ProductDetail, 'status' | 'hasUnpublishedChanges'>, locallyDirty = false): boolean {
  if (p.status === 'archived') return false;
  return p.status === 'draft' || p.hasUnpublishedChanges || locallyDirty;
}

const defaultRoleLabels: Record<SystemRoleKey, string> = {
  SUPER_ADMIN: 'Super Admin',
  ADMIN: 'Admin',
  STAFF: 'Staff',
  END_USER: 'End user'
};

/**
 * Fills anything the server left out so the wizard always edits a complete config,
 * and guarantees all four system roles exist with SUPER_ADMIN enabled.
 */
export function normalizeConfig(c: Partial<ProductConfig> | undefined): ProductConfig {
  const byKey = new Map((c?.roles ?? []).map((r) => [r.key, r]));
  const roles: ProductRole[] = SYSTEM_ROLES.map((key) => {
    const r = byKey.get(key);
    return { key, label: r?.label || defaultRoleLabels[key], enabled: key === 'SUPER_ADMIN' ? true : r?.enabled ?? true };
  });
  return {
    accentColor: c?.accentColor || ACCENT_COLORS[0],
    modules: c?.modules ?? [],
    userTypes: (c?.userTypes ?? []).map((u) => ({ ...u, allowedRoles: u.allowedRoles ?? [] })),
    roles,
    leadStatuses: c?.leadStatuses ?? [],
    pipelineStages: c?.pipelineStages ?? [],
    conversion: { createContact: true, createOpportunity: false, requireQualified: false, ...c?.conversion },
    loginMethods: { password: true, otp: true, google: false, microsoft: false, linkedin: false, sso: false, ...c?.loginMethods, enforced: true },
    selfRegistration: c?.selfRegistration ?? false,
    integrations: { apiAccess: false, webhooks: false, ...c?.integrations }
  };
}

/** Sign-in methods a product can allow (D-64), in the order the setup shows them. */
export const LOGIN_METHODS = ['password', 'otp', 'google', 'microsoft', 'linkedin', 'sso'] as const;

export type SetupStep = 'general' | 'modules' | 'roles' | 'pipeline' | 'login' | 'review';
export const SETUP_STEPS: SetupStep[] = ['general', 'modules', 'roles', 'pipeline', 'login', 'review'];

/** Publish-validation field key (possibly nested, e.g. "userTypes.0.label") → wizard step that fixes it. */
export function stepForField(field: string): SetupStep {
  const root = field.split(/[.[]/)[0];
  switch (root) {
    case 'name':
    case 'description':
    case 'icon':
    case 'accentColor':
      return 'general';
    case 'modules':
      return 'modules';
    case 'userTypes':
    case 'roles':
      return 'roles';
    case 'leadStatuses':
    case 'pipelineStages':
    case 'conversion':
      return 'pipeline';
    case 'loginMethods':
    case 'selfRegistration':
    case 'integrations':
      return 'login';
    default:
      return 'review';
  }
}

export function moveItem<T>(list: T[], index: number, delta: -1 | 1): T[] {
  const target = index + delta;
  if (target < 0 || target >= list.length) return list;
  const next = list.slice();
  [next[index], next[target]] = [next[target], next[index]];
  return next;
}
