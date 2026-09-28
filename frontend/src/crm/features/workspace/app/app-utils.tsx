import { useTranslation } from 'react-i18next';
import { BadgeCheck, Crown, Eye, EyeOff, ShieldCheck } from 'lucide-react';
import type { AppPlanId, AppUser, BusinessListing, BusinessStatus, BusinessVerification, WorkspaceContext } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { workspaceBase } from '../workspace-context';

// The connected app's data (catalog objects app_user / app_business).

export const appKeys = {
  all: (code: string) => ['workspace', code, 'app'] as const,
  users: (code: string, params: object) => ['workspace', code, 'app', 'users', params] as const,
  user: (code: string, id: string) => ['workspace', code, 'app', 'user', id] as const,
  businesses: (code: string, params: object) => ['workspace', code, 'app', 'businesses', params] as const,
  business: (code: string, id: string) => ['workspace', code, 'app', 'business', id] as const,
  categories: (code: string) => ['workspace', code, 'app', 'categories'] as const
};

export type AppObject = 'app_user' | 'app_business';

/** What the viewer may do with an app object (owner: everything). */
export function appAccess(ctx: WorkspaceContext, object: AppObject): { read: boolean; update: boolean; delete: boolean } {
  if (ctx.viewerIsOwner) return { read: true, update: true, delete: true };
  const a = ctx.effective.objects[object];
  const on = Boolean(a && a.moduleEnabled);
  const has = (x: 'read' | 'update' | 'delete') => on && Boolean(a?.actions.includes(x));
  return { read: has('read'), update: has('update'), delete: has('delete') };
}

export const appUserPath = (code: string, id: string) => `${workspaceBase(code)}/app-users/${encodeURIComponent(id)}`;
export const businessPath = (code: string, id: string) => `${workspaceBase(code)}/businesses/${encodeURIComponent(id)}`;

export const PLANS: AppPlanId[] = ['3m', '6m', '12m', 'lifetime'];

export function formatDate(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });
}

export function formatPhone(p: string) {
  const m = /^\+91(\d{5})(\d{5})$/.exec(p);
  return m ? `+91 ${m[1]} ${m[2]}` : p;
}

export function initials(name: string) {
  return (
    name
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((w) => w[0]!.toUpperCase())
      .join('') || '?'
  );
}

export function AccessBadge({ user }: { user: Pick<AppUser, 'premium' | 'planName' | 'expiresAt'> }) {
  const { t } = useTranslation();
  if (!user.premium) return <Badge tone="neutral">{t('workspaceApp.app.free')}</Badge>;
  return (
    <Badge tone="success" className="gap-1">
      <Crown className="size-3" aria-hidden />
      {user.planName || t('workspaceApp.app.premium')}
      {user.expiresAt ? <span className="font-normal opacity-80">· {t('workspaceApp.app.until', { date: formatDate(user.expiresAt) })}</span> : null}
    </Badge>
  );
}

export function RoleBadge({ role }: { role: string }) {
  const { t } = useTranslation();
  return role === 'admin' ? (
    <Badge tone="danger" className="gap-1">
      <ShieldCheck className="size-3" aria-hidden />
      {t('workspaceApp.app.roleAdmin')}
    </Badge>
  ) : (
    <Badge tone="neutral">{t('workspaceApp.app.roleUser')}</Badge>
  );
}

export function UserStatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  if (status === 'active') return null;
  return <Badge tone={status === 'suspended' ? 'danger' : 'warning'}>{t(`workspaceApp.app.userStatus.${status}`, { defaultValue: status })}</Badge>;
}

export const verified = (v: BusinessVerification) => v !== 'pending' && v !== 'failed';

export function VerificationBadge({ value }: { value: BusinessVerification }) {
  const { t } = useTranslation();
  if (!verified(value)) return <Badge tone={value === 'failed' ? 'danger' : 'warning'}>{t(`workspaceApp.app.verification.${value}`)}</Badge>;
  return (
    <Badge tone="warning" className="gap-1 bg-amber-50 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300">
      <BadgeCheck className="size-3" aria-hidden />
      {t(`workspaceApp.app.verification.${value}`)}
    </Badge>
  );
}

export function ListingBadge({ value }: { value: BusinessListing }) {
  const { t } = useTranslation();
  return value === 'listed' ? (
    <Badge tone="success" className="gap-1">
      <Eye className="size-3" aria-hidden />
      {t('workspaceApp.app.listed')}
    </Badge>
  ) : (
    <Badge tone="neutral" className="gap-1">
      <EyeOff className="size-3" aria-hidden />
      {t('workspaceApp.app.hidden')}
    </Badge>
  );
}

export function BusinessStatusBadge({ value }: { value: BusinessStatus }) {
  const { t } = useTranslation();
  if (value === 'live') return null;
  const tone = value === 'suspended' || value === 'removed' ? 'danger' : 'warning';
  return <Badge tone={tone}>{t(`workspaceApp.app.bizStatus.${value}`)}</Badge>;
}
