import { useTranslation } from 'react-i18next';
import type { Invitation, SystemRoleKey, WorkspaceMember, WorkspaceStatus } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { cn } from '@crm/lib/utils';

type Tone = 'neutral' | 'primary' | 'success' | 'warning' | 'danger';

export const WORKSPACE_CODE_RE = /^[a-z0-9-]{3,40}$/;

/** "Acme Realty Pvt Ltd" → "acme-realty-pvt-ltd" (≤40 chars). */
export function slugCode(input: string): string {
  return input
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 40)
    .replace(/-+$/, '');
}

export const TIMEZONES = [
  'Asia/Kolkata',
  'Asia/Dubai',
  'Asia/Singapore',
  'Asia/Tokyo',
  'Asia/Hong_Kong',
  'Australia/Sydney',
  'Europe/London',
  'Europe/Berlin',
  'Europe/Paris',
  'America/New_York',
  'America/Chicago',
  'America/Los_Angeles',
  'UTC'
];

export const LOCALES = ['en', 'hi', 'ta', 'te', 'mr', 'bn', 'ar'];
export const CURRENCIES = ['INR', 'USD', 'EUR', 'GBP', 'AED', 'SGD'];
export const ROLE_KEYS: SystemRoleKey[] = ['SUPER_ADMIN', 'ADMIN', 'STAFF', 'END_USER'];

/** Keeps the saved value selectable even if it isn't in our short list. */
export function withCurrent(list: string[], current: string | undefined): string[] {
  return current && !list.includes(current) ? [current, ...list] : list;
}

export const workspaceStatusTone: Record<WorkspaceStatus, Tone> = {
  active: 'success',
  provisioning: 'primary',
  draft: 'neutral',
  failed: 'danger',
  suspended: 'warning'
};

const memberTone: Record<WorkspaceMember['status'], Tone> = { active: 'success', invited: 'primary', suspended: 'warning', revoked: 'neutral' };
const inviteTone: Record<Invitation['status'], Tone> = {
  pending: 'primary',
  delivered: 'primary',
  accepted: 'success',
  expired: 'neutral',
  revoked: 'neutral',
  delivery_failed: 'danger'
};

export function WorkspaceStatusBadge({ status }: { status: WorkspaceStatus }) {
  const { t } = useTranslation();
  return <Badge tone={workspaceStatusTone[status] ?? 'neutral'}>{t(`status.${status}`, { defaultValue: status })}</Badge>;
}

export function MemberStatusBadge({ status }: { status: WorkspaceMember['status'] }) {
  const { t } = useTranslation();
  return <Badge tone={memberTone[status] ?? 'neutral'}>{t(`workspaces.memberStatus.${status}`, { defaultValue: status })}</Badge>;
}

export function InviteStatusBadge({ status }: { status: Invitation['status'] }) {
  const { t } = useTranslation();
  return <Badge tone={inviteTone[status] ?? 'neutral'}>{t(`workspaces.inviteStatus.${status}`, { defaultValue: status })}</Badge>;
}

/** Letter tile for a workspace (first letter of its name). */
export function WorkspaceTile({ name, size = 'md', className }: { name: string; size?: 'sm' | 'md' | 'lg'; className?: string }) {
  const letter = name.trim()[0]?.toUpperCase() ?? '?';
  return (
    <span
      className={cn(
        'grid shrink-0 place-items-center bg-muted font-semibold text-foreground ring-1 ring-inset ring-border',
        size === 'sm' ? 'size-8 rounded-md text-[13px]' : size === 'lg' ? 'size-12 rounded-xl text-lg' : 'size-10 rounded-lg text-sm',
        className
      )}
      aria-hidden
    >
      {letter}
    </span>
  );
}

export function isOpenInvite(i: Invitation): boolean {
  return i.status === 'pending' || i.status === 'delivered' || i.status === 'delivery_failed';
}
