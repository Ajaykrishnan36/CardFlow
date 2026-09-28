import type { useTranslation } from 'react-i18next';
import type { AuditEntry } from '@crm/api/types';

export type TFunction = ReturnType<typeof useTranslation>['t'];

/** Fallback for unknown actions: "workspace.product_assigned" → "Workspace product assigned". */
export function fallbackActionLabel(action: string): string {
  const words = action.replace(/[._]+/g, ' ').trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : action;
}

/**
 * Human label for an audit action. Known actions live in the audit i18n namespace under
 * `audit.actions.<action with dots → underscores>`; anything under `auth.mfa.*` without its own
 * label gets a generic two-step-verification label; everything else falls back to the raw
 * action with dots → spaces.
 */
export function humanizeAction(t: TFunction, action: string): string {
  const key = action.replace(/\./g, '_');
  const fallback = action.startsWith('auth.mfa.')
    ? t('audit.actions.mfaGeneric', { detail: fallbackActionLabel(action.slice('auth.mfa.'.length)).toLowerCase() })
    : fallbackActionLabel(action);
  return t(`audit.actions.${key}`, { defaultValue: fallback });
}

export type ActionTone = 'success' | 'danger' | 'warning' | 'primary' | 'neutral';

/** Colour hint for timeline dots / icons. */
export function actionTone(action: string): ActionTone {
  if (/failed|denied|locked|suspend|revok/.test(action)) return 'danger';
  if (/succeeded|created|accepted|provision|reactivat|enrolled/.test(action)) return 'success';
  if (/reset|mfa|password/.test(action)) return 'warning';
  if (/updated|changed|assigned|published/.test(action)) return 'primary';
  return 'neutral';
}

export function actorLabel(t: TFunction, e: AuditEntry): string {
  return e.actorName?.trim() || t('audit.system');
}

export function shortId(id: string): string {
  return id.length > 10 ? `${id.slice(0, 8)}…` : id;
}

export function absoluteTime(iso: string): string {
  return new Date(iso).toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}
