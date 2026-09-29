import { useQuery } from '@tanstack/react-query';
import { Building2, Contact, Target, type LucideIcon } from 'lucide-react';
import type { FieldDef, Layout, LookupTarget, ObjectKey, ObjectMeta, RecordListParams, StatusOption } from '@crm/api/types';
import { navIcon } from '@crm/features/shell/nav-icons';
import { ownerScope, scopedLookupHref, useRecordScope } from './record-scope';

// Shared plumbing for the metadata-driven record pages (leads / accounts / contacts).

/** Query keys carry the API prefix ('/platform' or '/w/<code>') so audiences never share cache. */
export const recordKeys = {
  prefix: (prefix: string) => ['records', prefix] as const,
  all: (prefix: string, object: ObjectKey) => ['records', prefix, object] as const,
  meta: (prefix: string, object: ObjectKey) => ['records', prefix, object, 'meta'] as const,
  lists: (prefix: string, object: ObjectKey) => ['records', prefix, object, 'list'] as const,
  list: (prefix: string, object: ObjectKey, params: RecordListParams) => ['records', prefix, object, 'list', params] as const,
  detail: (prefix: string, object: ObjectKey, id: string) => ['records', prefix, object, 'detail', id] as const
};

export function useObjectMeta(object: ObjectKey) {
  const scope = useRecordScope();
  return useQuery({ queryKey: recordKeys.meta(scope.prefix, object), queryFn: () => scope.api.meta(object), staleTime: 5 * 60_000 });
}

const builtinIcons: Record<string, LucideIcon> = { leads: Target, accounts: Building2, contacts: Contact };

/** Icon of an object: built-ins have their own; objects defined as data carry an icon key in their meta. */
export function objectIcon(object: ObjectKey, iconKey?: string): LucideIcon {
  return builtinIcons[object] ?? navIcon(iconKey ?? 'box');
}

export function useObjectIcon(object: ObjectKey): LucideIcon {
  const q = useObjectMeta(object);
  return objectIcon(object, q.data?.icon);
}

export const RECORD_OBJECTS: readonly ObjectKey[] = ['leads', 'accounts', 'contacts'];

export function isRecordObject(o: unknown): o is ObjectKey {
  return typeof o === 'string' && (RECORD_OBJECTS as readonly string[]).includes(o);
}

export function fieldIndex(meta: ObjectMeta | undefined): Map<string, FieldDef> {
  return new Map((meta?.fields ?? []).map((f) => [f.key, f]));
}

/** Owner-console route of a lookup target record, or null. Prefer `scopedLookupHref` in record pages. */
export function lookupHref(target: LookupTarget | string | undefined, id: string | undefined): string | null {
  return scopedLookupHref(ownerScope, target, id);
}

export function isEmptyValue(v: unknown): boolean {
  return v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0);
}

/** '' / [] / undefined → null so drafts compare and serialise consistently. */
export function normalizeValue(v: unknown): unknown {
  return isEmptyValue(v) ? null : v;
}

export function sameValue(a: unknown, b: unknown): boolean {
  return JSON.stringify(normalizeValue(a)) === JSON.stringify(normalizeValue(b));
}

export const inr = new Intl.NumberFormat('en-IN', { style: 'currency', currency: 'INR', maximumFractionDigits: 0 });

export function statusOption(meta: ObjectMeta | undefined, value: unknown): StatusOption | undefined {
  if (typeof value !== 'string') return undefined;
  return meta?.statuses?.find((s) => s.value === value);
}

type Tone = StatusOption['tone'];

/** Best-effort tone for statuses of other objects (related lists, invitations). */
export function guessTone(status: string | undefined): Tone {
  switch ((status ?? '').toLowerCase()) {
    case 'active':
    case 'accepted':
    case 'converted':
    case 'resolved':
    case 'qualified':
    case 'customer':
    case 'won':
      return 'success';
    case 'new':
    case 'open':
    case 'pending':
    case 'delivered':
    case 'invited':
    case 'working':
    case 'provisioning':
      return 'primary';
    case 'in_progress':
    case 'suspended':
    case 'expired':
    case 'draft':
      return 'warning';
    case 'lost':
    case 'failed':
    case 'revoked':
    case 'delivery_failed':
    case 'archived':
      return 'danger';
    default:
      return 'neutral';
  }
}

export function humanize(s: string): string {
  const spaced = s.replace(/[_-]+/g, ' ').replace(/([a-z])([A-Z])/g, '$1 $2').trim();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** "Annual revenue (₹)" → "annual_revenue" (custom field keys). */
export function toFieldKey(label: string): string {
  const k = label
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .replace(/^[0-9_]+/, '')
    .slice(0, 40)
    .replace(/_+$/, '');
  return k;
}

export const FIELD_KEY_RE = /^[a-z][a-z0-9_]{1,39}$/;
export const WORKSPACE_CODE_RE = /^[a-z0-9-]{3,40}$/;

/** "Acme Traders Pvt. Ltd." → "acme-traders-pvt-ltd" (workspace codes). */
export function slugify(s: string): string {
  return s
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
    .replace(/-+$/, '');
}

export function placedFieldKeys(layout: Layout | undefined): Set<string> {
  return new Set((layout?.sections ?? []).flatMap((s) => s.fields));
}

export function newId(prefix: string): string {
  const rand = typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID().slice(0, 8) : Math.random().toString(36).slice(2, 10);
  return `${prefix}_${rand}`;
}

/** 'YYYY-MM-DD' parsed as a local calendar date (no timezone shift). */
export function parseLocalDate(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s);
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return Number.isNaN(d.getTime()) ? null : d;
}

export function isoToLocalInput(iso: unknown): string {
  if (typeof iso !== 'string' || !iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function localInputToIso(s: string): string | null {
  if (!s) return null;
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}
