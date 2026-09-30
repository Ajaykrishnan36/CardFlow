import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Check, ExternalLink, Info, Minus } from 'lucide-react';
import type { FieldDef, LookupValue, ObjectMeta, RecordRow } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { Tooltip } from '@crm/components/ui/menu';
import { cn, relativeTime } from '@crm/lib/utils';
import { humanize, inr, isEmptyValue, parseLocalDate, statusOption } from './use-object-meta';
import { scopedLookupHref, useRecordScope } from './record-scope';

export function EmptyValue() {
  return <span className="text-muted-foreground/60">—</span>;
}

function optionLabel(field: FieldDef, v: unknown): string {
  const s = String(v);
  // A value no longer in the pick-list (an older setup) still reads as words.
  return field.options?.find((o) => o.value === s)?.label ?? humanize(s);
}

function formatDate(v: unknown): string {
  if (typeof v !== 'string') return String(v);
  const d = parseLocalDate(v);
  return d ? d.toLocaleDateString('en-IN', { day: 'numeric', month: 'short', year: 'numeric' }) : v;
}

function formatDateTime(v: unknown): string {
  if (typeof v !== 'string') return String(v);
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function asNumber(v: unknown): number | null {
  const n = typeof v === 'number' ? v : typeof v === 'string' && v.trim() !== '' ? Number(v) : NaN;
  return Number.isFinite(n) ? n : null;
}

/** Plain-text rendering (card lists, titles, aria labels). */
export function formatValueText(field: FieldDef, value: unknown, lookup?: LookupValue): string {
  if (isEmptyValue(value)) return '—';
  switch (field.type) {
    case 'number': {
      const n = asNumber(value);
      return n === null ? String(value) : n.toLocaleString('en-IN');
    }
    case 'currency': {
      const n = asNumber(value);
      return n === null ? String(value) : inr.format(n);
    }
    case 'percent': {
      const n = asNumber(value);
      return n === null ? String(value) : `${n.toLocaleString('en-IN')}%`;
    }
    case 'date':
      return formatDate(value);
    case 'datetime':
      return formatDateTime(value);
    case 'select':
      return optionLabel(field, value);
    case 'multiselect':
      return (Array.isArray(value) ? value : [value]).map((v) => optionLabel(field, v)).join(', ');
    case 'boolean':
      return value ? 'Yes' : 'No';
    case 'lookup':
      return lookup?.label ?? String(value);
    case 'rating':
      return '★'.repeat(Math.max(0, Math.min(5, Number(value) || 0)));
    case 'address':
    case 'fullName':
      return partsText(value);
    case 'emails':
    case 'phones':
    case 'links':
      return (Array.isArray(value) ? value : [value]).map(String).join(', ');
    case 'json':
      return JSON.stringify(value);
    case 'files':
      return (Array.isArray(value) ? value : []).map((f) => (f as { name?: string }).name ?? '').join(', ');
    default:
      return String(value);
  }
}

function partsText(value: unknown): string {
  if (!value || typeof value !== 'object') return String(value ?? '');
  const v = value as Record<string, string>;
  if ('firstName' in v || 'lastName' in v) return [v.firstName, v.lastName].filter(Boolean).join(' ');
  return ['street', 'street2', 'city', 'state', 'postalCode', 'country'].map((k) => v[k]).filter(Boolean).join(', ');
}

/**
 * Display renderer for one field value. `compact` is the list-view flavour (single line,
 * relative datetimes); the default is the record-page flavour.
 */
export function FieldValue({
  field,
  record,
  meta,
  compact,
  className
}: {
  field: FieldDef;
  record: Pick<RecordRow, 'values' | 'lookups'> & { links?: RecordRow['links'] };
  meta?: ObjectMeta;
  compact?: boolean;
  className?: string;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const value = record.values[field.key];
  if (isEmptyValue(value)) return <EmptyValue />;

  const linkCls = 'font-medium text-primary hover:underline underline-offset-2';

  if (meta?.statusField === field.key) {
    const s = statusOption(meta, value);
    return <Badge tone={s?.tone ?? 'neutral'}>{s?.label ?? optionLabel(field, value)}</Badge>;
  }

  switch (field.type) {
    case 'email':
      return (
        <a href={`mailto:${String(value)}`} className={cn(linkCls, 'break-all', compact && 'truncate', className)} onClick={(e) => e.stopPropagation()}>
          {String(value)}
        </a>
      );
    case 'phone':
      return (
        <a href={`tel:${String(value).replace(/\s+/g, '')}`} className={cn(linkCls, 'whitespace-nowrap tabular-nums', className)} onClick={(e) => e.stopPropagation()}>
          {String(value)}
        </a>
      );
    case 'url': {
      const raw = String(value);
      const href = /^https?:\/\//i.test(raw) ? raw : `https://${raw}`;
      return (
        <a href={href} target="_blank" rel="noreferrer noopener" className={cn(linkCls, 'inline-flex max-w-full items-center gap-1', className)} onClick={(e) => e.stopPropagation()}>
          <span className="truncate">{raw.replace(/^https?:\/\//i, '').replace(/\/$/, '')}</span>
          <ExternalLink className="size-3 shrink-0" aria-hidden />
        </a>
      );
    }
    case 'textarea':
      return <span className={cn(compact ? 'line-clamp-1' : 'whitespace-pre-wrap', className)}>{String(value)}</span>;
    case 'number':
    case 'currency':
    case 'percent':
      return <span className={cn('tabular-nums', className)}>{formatValueText(field, value)}</span>;
    case 'date':
      return <span className={cn('whitespace-nowrap', className)}>{formatDate(value)}</span>;
    case 'datetime': {
      const full = formatDateTime(value);
      if (!compact) return <span className={className}>{full}</span>;
      return (
        <time dateTime={String(value)} title={full} className={cn('whitespace-nowrap', className)}>
          {relativeTime(String(value))}
        </time>
      );
    }
    case 'select':
      return <span className={className}>{optionLabel(field, value)}</span>;
    case 'multiselect': {
      const items = Array.isArray(value) ? value : [value];
      return (
        <span className={cn('flex gap-1', compact ? 'overflow-hidden' : 'flex-wrap', className)}>
          {items.map((v) => (
            <Badge key={String(v)} className="font-medium">
              {optionLabel(field, v)}
            </Badge>
          ))}
        </span>
      );
    }
    case 'boolean':
      return value ? (
        <span className={cn('inline-flex items-center gap-1 text-foreground', className)}>
          <Check className="size-3.5 text-success" aria-hidden /> {t('records.common.yes')}
        </span>
      ) : (
        <span className={cn('inline-flex items-center gap-1 text-muted-foreground', className)}>
          <Minus className="size-3.5" aria-hidden /> {t('records.common.no')}
        </span>
      );
    case 'lookup': {
      const lk = record.lookups[field.key];
      const id = String(value);
      const href = scopedLookupHref(scope, lk?.object ?? field.lookup, id);
      const label = lk?.label ?? id;
      if (!href) return <span className={cn(!lk && 'font-mono text-xs', className)}>{label}</span>;
      return (
        <Link to={href} className={cn(linkCls, compact && 'truncate', !lk && 'font-mono text-xs', className)} onClick={(e) => e.stopPropagation()}>
          {label}
        </Link>
      );
    }
    case 'rating': {
      const n = Math.max(0, Math.min(5, Number(value) || 0));
      return (
        <span className={cn('whitespace-nowrap text-sm leading-none', className)} aria-label={t('records.input.stars', { count: n })}>
          <span className="text-amber-500">{'★'.repeat(n)}</span>
          <span className="text-muted-foreground/30">{'★'.repeat(5 - n)}</span>
        </span>
      );
    }
    case 'address':
    case 'fullName':
      return <span className={cn(compact ? 'truncate' : 'whitespace-pre-wrap', className)}>{partsText(value)}</span>;
    case 'emails':
    case 'phones':
    case 'links': {
      const items = (Array.isArray(value) ? value : [value]).map(String);
      const href = (v: string) =>
        field.type === 'emails' ? `mailto:${v}` : field.type === 'phones' ? `tel:${v.replace(/\s+/g, '')}` : /^https?:\/\//i.test(v) ? v : `https://${v}`;
      return (
        <span className={cn('flex gap-x-2 gap-y-0.5', compact ? 'overflow-hidden' : 'flex-wrap', className)}>
          {items.map((v) => (
            <a key={v} href={href(v)} target={field.type === 'links' ? '_blank' : undefined} rel="noreferrer noopener" className={cn(linkCls, 'truncate')} onClick={(e) => e.stopPropagation()}>
              {field.type === 'links' ? v.replace(/^https?:\/\//i, '').replace(/\/$/, '') : v}
            </a>
          ))}
        </span>
      );
    }
    case 'json':
      return compact ? (
        <code className={cn('truncate font-mono text-xs', className)}>{JSON.stringify(value)}</code>
      ) : (
        <pre className={cn('max-h-48 overflow-auto rounded bg-muted p-2 font-mono text-xs', className)}>{JSON.stringify(value, null, 2)}</pre>
      );
    case 'relations': {
      const list = record.links?.[field.key] ?? (Array.isArray(value) ? value.map((id) => ({ id: String(id), label: String(id) })) : []);
      return (
        <span className={cn('flex gap-1', compact ? 'overflow-hidden' : 'flex-wrap', className)}>
          {list.map((l) => {
            const href = scopedLookupHref(scope, field.lookup, l.id);
            return href ? (
              <Link key={l.id} to={href} className="rounded bg-primary-soft px-1.5 py-0.5 text-xs font-medium text-primary hover:underline" onClick={(e) => e.stopPropagation()}>
                {l.label}
              </Link>
            ) : (
              <Badge key={l.id}>{l.label}</Badge>
            );
          })}
        </span>
      );
    }
    case 'files': {
      const files = (Array.isArray(value) ? value : []) as Array<{ id: string; name: string }>;
      return (
        <span className={cn('flex gap-x-2 gap-y-0.5', compact ? 'overflow-hidden' : 'flex-col', className)}>
          {files.map((f) => (
            <a key={f.id} href={scope.api.fileUrl(f.id)} className={cn(linkCls, 'truncate')} onClick={(e) => e.stopPropagation()}>
              {f.name}
            </a>
          ))}
        </span>
      );
    }
    default:
      return <span className={cn(compact && 'truncate', 'break-words', className)}>{String(value)}</span>;
  }
}

/** Field label with an optional info tooltip for `helpText`. */
export function FieldLabel({ field, required, className }: { field: FieldDef; required?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-1', className)}>
      <span>{field.label}</span>
      {required ? (
        <span className="text-danger" aria-hidden>
          *
        </span>
      ) : null}
      {field.helpText ? <HelpTip text={field.helpText} /> : null}
    </span>
  );
}

export function HelpTip({ text }: { text: string }) {
  return (
    <Tooltip content={<span className="block max-w-[240px] whitespace-normal">{text}</span>} side="top">
      <button type="button" className="grid size-4 place-items-center rounded-full text-muted-foreground/70 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={text}>
        <Info className="size-3.5" aria-hidden />
      </button>
    </Tooltip>
  );
}
