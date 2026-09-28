import type { Invitation, SystemRoleKey, UserMembership, UserSummary } from '@crm/api/types';

export type Tone = 'success' | 'warning' | 'danger' | 'neutral' | 'primary';

export const userStatusTone: Record<UserSummary['status'], Tone> = {
  active: 'success',
  suspended: 'warning',
  deleted: 'danger'
};

export const membershipStatusTone: Record<UserMembership['status'], Tone> = {
  active: 'success',
  invited: 'primary',
  suspended: 'warning',
  revoked: 'neutral'
};

export const inviteStatusTone: Record<Invitation['status'], Tone> = {
  pending: 'primary',
  delivered: 'primary',
  accepted: 'success',
  expired: 'neutral',
  revoked: 'neutral',
  delivery_failed: 'danger'
};

export const ROLE_KEYS: SystemRoleKey[] = ['SUPER_ADMIN', 'ADMIN', 'STAFF', 'END_USER'];

export const LOCALES: Array<{ value: string; label: string }> = [
  { value: 'en', label: 'English' },
  { value: 'hi', label: 'हिन्दी · Hindi' },
  { value: 'ta', label: 'தமிழ் · Tamil' },
  { value: 'te', label: 'తెలుగు · Telugu' },
  { value: 'ml', label: 'മലയാളം · Malayalam' },
  { value: 'kn', label: 'ಕನ್ನಡ · Kannada' },
  { value: 'mr', label: 'मराठी · Marathi' },
  { value: 'bn', label: 'বাংলা · Bengali' }
];

const ZONES = [
  'Asia/Kolkata',
  'UTC',
  'Asia/Dubai',
  'Asia/Singapore',
  'Asia/Tokyo',
  'Asia/Kathmandu',
  'Asia/Dhaka',
  'Asia/Colombo',
  'Europe/London',
  'Europe/Berlin',
  'America/New_York',
  'America/Chicago',
  'America/Los_Angeles',
  'Australia/Sydney'
];

function offsetLabel(zone: string): string {
  try {
    const part = new Intl.DateTimeFormat('en-US', { timeZone: zone, timeZoneName: 'shortOffset' })
      .formatToParts(new Date())
      .find((p) => p.type === 'timeZoneName');
    return part ? part.value.replace('GMT', 'UTC') : '';
  } catch {
    return '';
  }
}

/** Common zones, plus the user's current zone if it's not one of them. */
export function timezoneOptions(current?: string): Array<{ value: string; label: string }> {
  const zones = current && !ZONES.includes(current) ? [current, ...ZONES] : ZONES;
  return zones.map((z) => {
    const off = offsetLabel(z);
    return { value: z, label: off ? `${z.replace(/_/g, ' ')} (${off})` : z.replace(/_/g, ' ') };
  });
}

export function localeLabel(locale: string): string {
  return LOCALES.find((l) => l.value === locale)?.label ?? locale;
}

export function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' });
}
