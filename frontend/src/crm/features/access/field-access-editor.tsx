import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Eye, EyeOff, Lock, Pencil } from 'lucide-react';
import type { AccessRules, FieldCatalogObject, FieldLevel } from '@crm/api/types';
import { Badge } from '@crm/components/ui/card';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';

const LEVELS: FieldLevel[] = ['edit', 'read', 'hidden'];
const rank: Record<FieldLevel, number> = { hidden: 0, read: 1, edit: 2 };
const icons = { edit: Pencil, read: Eye, hidden: EyeOff };

/**
 * Field access (D-46): per granted object, each field is Editable, Read only or Hidden.
 * Hidden fields never appear in pages or API responses; read-only ones can't be changed.
 * `limit` (a delegated admin's own access) caps what can be opened up.
 */
export function FieldAccessEditor({
  rules,
  onChange,
  catalog,
  loading,
  disabled,
  limit
}: {
  rules: AccessRules;
  onChange: (rules: AccessRules) => void;
  catalog?: FieldCatalogObject[];
  loading?: boolean;
  disabled?: boolean;
  limit?: AccessRules['fields'];
}) {
  const { t } = useTranslation();
  const granted = useMemo(() => (catalog ?? []).filter((o) => (rules.objects[o.key] ?? []).length > 0), [catalog, rules.objects]);
  const [picked, setPicked] = useState<string>('');
  const current = granted.find((o) => o.key === picked) ?? granted[0];
  const levelOf = (obj: string, field: string): FieldLevel => rules.fields?.[obj]?.[field] ?? 'edit';
  const limitOf = (obj: string, field: string): FieldLevel => limit?.[obj]?.[field] ?? 'edit';

  const set = (obj: string, field: string, level: FieldLevel) => {
    const next = { ...(rules.fields ?? {}) };
    const map = { ...(next[obj] ?? {}) };
    if (level === 'edit') delete map[field];
    else map[field] = level;
    if (Object.keys(map).length) next[obj] = map;
    else delete next[obj];
    onChange({ ...rules, fields: next });
  };
  const setAll = (obj: FieldCatalogObject, level: FieldLevel) => {
    const next = { ...(rules.fields ?? {}) };
    const map: Record<string, 'read' | 'hidden'> = {};
    for (const f of obj.fields) {
      if (f.locked) continue;
      const l = rank[level] > rank[limitOf(obj.key, f.key)] ? limitOf(obj.key, f.key) : level;
      if (l !== 'edit') map[f.key] = l;
    }
    if (Object.keys(map).length) next[obj.key] = map;
    else delete next[obj.key];
    onChange({ ...rules, fields: next });
  };

  const restrictedCount = (obj: string) => Object.keys(rules.fields?.[obj] ?? {}).length;

  return (
    <fieldset className="rounded-lg border">
      <legend className="sr-only">{t('access.fields.title')}</legend>
      <div className="flex flex-col gap-2 border-b bg-muted/50 px-3 py-2 sm:flex-row sm:items-center">
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.fields.title')}</p>
          <p className="text-[11px] text-muted-foreground">{t('access.fields.hint')}</p>
        </div>
        {granted.length > 1 ? (
          <Select
            value={current?.key ?? ''}
            onChange={(e) => setPicked(e.target.value)}
            options={granted.map((o) => ({
              value: o.key,
              label: restrictedCount(o.key) ? `${o.label} · ${t('access.fields.restricted', { count: restrictedCount(o.key) })}` : o.label
            }))}
            className="sm:w-64"
            aria-label={t('access.fields.object')}
          />
        ) : null}
      </div>
      {loading ? (
        <div className="space-y-2 p-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      ) : !current ? (
        <p className="px-3 py-4 text-[13px] text-muted-foreground">{t('access.fields.nothingGranted')}</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{current.label}</span>
            <span>·</span>
            <span>{t('access.fields.setAll')}</span>
            {LEVELS.map((l) => (
              <button
                key={l}
                type="button"
                disabled={disabled}
                onClick={() => setAll(current, l)}
                className="rounded px-1.5 py-0.5 font-medium text-primary hover:bg-primary-soft disabled:opacity-50"
              >
                {t(`access.fields.level.${l}`)}
              </button>
            ))}
          </div>
          <ul className="max-h-80 divide-y overflow-y-auto">
            {current.fields.map((f) => {
              const level = levelOf(current.key, f.key);
              const cap = limitOf(current.key, f.key);
              return (
                <li key={f.key} className="flex items-center gap-3 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-[13px] font-medium">{f.label}</p>
                    <p className="truncate font-mono text-[11px] text-muted-foreground">
                      {f.key}
                      {!f.standard ? <Badge tone="primary" className="ml-1.5 font-sans">{t('access.fields.custom')}</Badge> : null}
                    </p>
                  </div>
                  {f.locked ? (
                    <Tooltip content={t('access.fields.lockedHint')} side="top">
                      <span tabIndex={0} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs text-muted-foreground">
                        <Lock className="size-3" aria-hidden /> {t('access.fields.alwaysEditable')}
                      </span>
                    </Tooltip>
                  ) : (
                    <div role="radiogroup" aria-label={f.label} className="inline-flex shrink-0 rounded-md border bg-muted/60 p-0.5">
                      {LEVELS.map((l) => {
                        const Icon = icons[l];
                        const blocked = rank[l] > rank[cap] && rank[l] > rank[level];
                        return (
                          <button
                            key={l}
                            type="button"
                            role="radio"
                            aria-checked={level === l}
                            disabled={disabled || blocked}
                            title={blocked ? t('access.fields.notYours') : undefined}
                            onClick={() => set(current.key, f.key, l)}
                            className={cn(
                              'inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                              level === l
                                ? l === 'hidden'
                                  ? 'bg-danger-soft text-danger shadow-sm'
                                  : l === 'read'
                                    ? 'bg-warning-soft text-warning shadow-sm'
                                    : 'bg-background text-foreground shadow-sm'
                                : 'text-muted-foreground hover:text-foreground'
                            )}
                          >
                            <Icon className="size-3" aria-hidden />
                            <span className="hidden sm:inline">{t(`access.fields.level.${l}`)}</span>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </fieldset>
  );
}
