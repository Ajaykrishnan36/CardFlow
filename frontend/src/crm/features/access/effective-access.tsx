import { useTranslation } from 'react-i18next';
import { Boxes, Check, Info } from 'lucide-react';
import type { AccessCatalog, EffectiveAccess } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';
import { AccessMatrix, GrantMark, ScopeBadge, type MatrixRow } from './access-matrix';
import { pluralLabel, useAccessCatalog } from './use-access';

function SourcesTip({ sources, label }: { sources: string[]; label: string }) {
  const { t } = useTranslation();
  const content = (
    <div className="max-w-[260px] space-y-0.5">
      <p className="font-semibold">{t('access.effective.sources')}</p>
      {sources.length ? sources.map((s) => <p key={s}>{s}</p>) : <p>{t('access.effective.noSources')}</p>}
    </div>
  );
  return (
    <Tooltip content={content} side="top">
      <button
        type="button"
        className="inline-flex items-center gap-1 rounded text-[11px] text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={`${label} — ${t('access.effective.sources')}: ${sources.join(', ') || t('access.effective.noSources')}`}
      >
        <Info className="size-3" aria-hidden />
        <span className="max-w-[180px] truncate">{sources.length ? sources.join(' · ') : t('access.effective.noSources')}</span>
      </button>
    </Tooltip>
  );
}

/**
 * Read-only "what can this user actually do" view: role + permission sets,
 * limited to the modules their products enable.
 */
export function EffectiveAccessMatrix({
  effective,
  catalog: given,
  isPlatform,
  preview,
  note,
  className
}: {
  effective: EffectiveAccess | undefined;
  catalog?: AccessCatalog;
  /** Owner's platform workspace: no products, every module on. */
  isPlatform?: boolean;
  /** Showing unsaved changes. */
  preview?: boolean;
  note?: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const q = useAccessCatalog(!given);
  const catalog = given ?? q.data;

  const header = (
    <div className="mb-2.5 flex flex-wrap items-start justify-between gap-2">
      <div className="min-w-0">
        <h3 className="text-[13px] font-semibold text-foreground">{t('access.effective.title')}</h3>
        <p className="text-xs text-muted-foreground">{t('access.effective.description')}</p>
      </div>
      {preview ? <Badge tone="warning">{t('access.effective.preview')}</Badge> : null}
    </div>
  );

  if (!catalog || !effective) {
    return (
      <section className={className} aria-busy>
        {header}
        <div className="space-y-2">
          <Skeleton className="h-9" />
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
        </div>
      </section>
    );
  }

  const objects = effective.objects ?? {};
  const rows: MatrixRow[] = catalog.objects.map((obj) => {
    const e = objects[obj.key];
    const label = pluralLabel(obj.label);
    const enabled = isPlatform ? true : (e?.moduleEnabled ?? false);
    const acts = e?.actions ?? [];
    return {
      key: obj.key,
      label,
      module: obj.module,
      supported: obj.actions,
      muted: !enabled,
      aside: (
        <div className="flex flex-wrap items-center gap-1.5">
          {!enabled ? <Badge>{t('access.effective.notInProducts')}</Badge> : null}
          <SourcesTip sources={e?.sources ?? []} label={label} />
        </div>
      ),
      scope: <ScopeBadge scope={e?.scope ?? 'own'} none={!enabled || acts.length === 0} />
    };
  });

  const anything = catalog.objects.some((o) => {
    const e = objects[o.key];
    return (isPlatform || e?.moduleEnabled) && (e?.actions.length ?? 0) > 0;
  });
  const grantedCaps = new Map((effective.capabilities ?? []).map((c) => [c.key, c.sources]));

  return (
    <section className={className} aria-label={t('access.effective.title')}>
      {header}

      <AccessMatrix
        dense
        catalog={catalog}
        rows={rows}
        label={t('access.effective.title')}
        renderCell={(row, action, variant) => (
          <GrantMark
            on={(objects[row.key]?.actions ?? []).includes(action)}
            muted={row.muted}
            label={catalog.actions.find((a) => a.key === action)?.label ?? action}
            variant={variant}
          />
        )}
      />

      {!anything ? <p className="mt-2 text-xs font-medium text-warning">{t('access.effective.nothing')}</p> : null}
      {note ? <p className="mt-2 text-xs text-muted-foreground">{note}</p> : null}

      <div className="mt-3 grid gap-3 sm:grid-cols-2">
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.effective.capabilities')}</p>
          <ul className="flex flex-wrap gap-1.5">
            {catalog.capabilities.map((cap) => {
              const sources = grantedCaps.get(cap.key);
              const on = Boolean(sources);
              return (
                <li key={cap.key}>
                  <Tooltip
                    side="top"
                    content={
                      <div className="max-w-[240px] space-y-0.5">
                        <p className="font-semibold">{cap.label}</p>
                        {on ? sources?.map((s) => <p key={s}>{s}</p>) : <p>{t('access.effective.noSources')}</p>}
                      </div>
                    }
                  >
                    <span
                      tabIndex={0}
                      className={cn(
                        'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                        on ? 'border-success/30 bg-success-soft text-foreground' : 'text-muted-foreground line-through decoration-muted-foreground/40'
                      )}
                    >
                      {on ? <Check className="size-3 text-success" strokeWidth={3} aria-hidden /> : null}
                      {cap.label}
                      <span className="sr-only">
                        : {on ? `${t('access.matrix.granted')} — ${sources?.join(', ')}` : t('access.matrix.notGranted')}
                      </span>
                    </span>
                  </Tooltip>
                </li>
              );
            })}
          </ul>
        </div>
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.effective.products')}</p>
          {isPlatform ? (
            <p className="text-xs text-muted-foreground">{t('access.effective.allModules')}</p>
          ) : (effective.products ?? []).length === 0 ? (
            <p className="text-xs font-medium text-warning">{t('access.effective.noProducts')}</p>
          ) : (
            <ul className="flex flex-wrap gap-1.5">
              {effective.products.map((p) => (
                <li key={p.id} className="inline-flex items-center gap-1 rounded-md border bg-muted/50 px-2 py-0.5 text-xs font-medium text-foreground">
                  <Boxes className="size-3 text-muted-foreground" aria-hidden />
                  {p.name}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </section>
  );
}
