// Tenants (D-156): the brand's permanent address, /<slug>/... for all of its businesses.
// The business you are working in is remembered per tenant, so switching between
// PSG School and PSG College never changes the address.
import type { Membership } from '@crm/api/types';

const codeToTenant = new Map<string, string>();
const tenantCodes = new Map<string, string[]>();

/** Learns which tenant each business belongs to (from /me). */
export function registerMemberships(ms: Membership[] | undefined): void {
  tenantCodes.clear();
  for (const m of ms ?? []) {
    if (!m.tenantSlug || m.status !== 'active') continue;
    codeToTenant.set(m.workspaceCode, m.tenantSlug);
    tenantCodes.set(m.tenantSlug, [...(tenantCodes.get(m.tenantSlug) ?? []), m.workspaceCode]);
  }
}

/** The business open under a tenant's address: the last one used there, else the first. */
export function currentBusinessFor(slug: string): string | undefined {
  const codes = tenantCodes.get(slug) ?? [];
  const stored = storedBusiness(slug);
  return codes.find((c) => c === stored) ?? codes[0];
}

export function registerTenant(code: string, slug: string): void {
  if (code && slug) codeToTenant.set(code, slug);
}

/** The tenant address of a business (its own code until we know better). */
export function tenantOf(code: string): string {
  return codeToTenant.get(code) ?? code;
}

/** The tenant of a business the signed-in person belongs to; undefined otherwise (e.g. the platform owner visiting). */
export function memberTenantOf(code: string): string | undefined {
  return codeToTenant.get(code);
}

const key = (slug: string) => `crm.tenant.${slug}.business`;

export function storedBusiness(slug: string): string | null {
  try {
    return window.localStorage.getItem(key(slug));
  } catch {
    return null;
  }
}

export function rememberBusiness(slug: string, code: string): void {
  try {
    window.localStorage.setItem(key(slug), code);
  } catch {
    /* private window: the first business is used */
  }
}

/** The tenant a URL belongs to: its first segment, unless it is one of the site's own. */
const SITE = new Set(['crm', 'share', 'api', '']);
export function tenantFromPath(pathname: string): string | null {
  const first = pathname.split('/')[1] ?? '';
  return SITE.has(first) ? null : decodeURIComponent(first).toLowerCase();
}

/** A server-made business address (/crm/w/<code>/x) as the tenant's address (/<slug>/x). */
export function toTenantPath(path: string): string {
  const m = /^\/crm\/w\/([^/?#]+)(.*)$/.exec(path);
  const slug = m ? memberTenantOf(decodeURIComponent(m[1])) : undefined;
  return m && slug ? `/${slug}${m[2]}` : path;
}

/** Where to sign in again for the page you are on. */
export function loginPathFor(pathname: string): string {
  if (pathname.startsWith('/crm/owner')) return '/crm/owner/login';
  const slug = tenantFromPath(pathname);
  return slug ? `/${slug}/login` : '/';
}

// The tenant whose login page is open (/<tenant>/login). Sign-in requests carry it so the
// server only lets that tenant's people in; "" on the main page.
let signInSlug = '';
export function setSignInTenant(slug: string): void {
  signInSlug = slug;
}
export function signInTenant(): string {
  return signInSlug;
}
