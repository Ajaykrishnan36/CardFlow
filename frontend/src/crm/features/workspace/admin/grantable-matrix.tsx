import { useId, type ReactElement } from 'react';
import { useTranslation } from 'react-i18next';
import { Lock } from 'lucide-react';
import type { AccessCatalog, AccessRules, ObjectAction, RowScope } from '@crm/api/types';
import { Checkbox, Select, Switch } from '@crm/components/ui/form-controls';
import { Tooltip } from '@crm/components/ui/menu';
import { AccessMatrix, TriCheckbox, type MatrixRow } from '@crm/features/access/access-matrix';
import { normalizeRules, pluralLabel } from '@crm/features/access/use-access';
import { cn } from '@crm/lib/utils';
import { canGrantAction, canGrantCapability, canGrantWorkspaceScope } from './admin-utils';

/**
 * Objects × actions editor limited to what the signed-in admin holds (`grantable`).
 * Grants they don't have are disabled with "You don't have this yourself"; grants
 * already present beyond that limit can be removed but not re-added.
 */
export function GrantableMatrix({
  value,
  onChange,
  catalog,
  grantable,
  disabled
}: {
  value: AccessRules;
  onChange: (rules: AccessRules) => void;
  catalog: AccessCatalog;
  grantable: AccessRules;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const uid = useId();
  const rules = normalizeRules(value, catalog);
  const notYours = t('workspaceApp.admin.notYours');
  const emit = (next: AccessRules) => onChange(normalizeRules(next, catalog));

  const setActions = (objKey: string, actions: ObjectAction[]) => {
    const objects = { ...rules.objects, [objKey]: actions };
    const rows = { ...rules.rows };
    if (actions.length && !rows[objKey]) rows[objKey] = { scope: 'own' };
    emit({ ...rules, objects, rows });
  };

  const toggle = (objKey: string, action: ObjectAction, on: boolean) => {
    const current = rules.objects[objKey] ?? [];
    if (action === 'read' && !on) return setActions(objKey, []);
    if (on && !canGrantAction(grantable, objKey, action)) return;
    const next = on ? Array.from(new Set<ObjectAction>([...current, action, 'read'])) : current.filter((a) => a !== action);
    setActions(objKey, next);
  };

  /** Wrap a disabled control so its tooltip is still reachable (disabled inputs swallow pointer events). */
  const locked = (node: ReactElement, reason: string) => (
    <Tooltip content={reason} side="top">
      <span tabIndex={0} className="inline-flex rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={reason}>
        {node}
      </span>
    </Tooltip>
  );

  const rows: MatrixRow[] = catalog.objects.map((obj) => {
    const acts = rules.objects[obj.key] ?? [];
    const allowed = obj.actions.filter((a) => canGrantAction(grantable, obj.key, a));
    const allAllowedOn = allowed.length > 0 && allowed.every((a) => acts.includes(a));
    const plural = pluralLabel(obj.label);
    const scope = rules.rows[obj.key]?.scope ?? 'own';
    const workspaceOk = canGrantWorkspaceScope(grantable, obj.key);
    const lead = (
      <TriCheckbox
        checked={allAllowedOn}
        indeterminate={acts.length > 0}
        disabled={disabled || (allowed.length === 0 && acts.length === 0)}
        // "All" means all that you can grant; unchecking clears the row.
        onCheckedChange={(on) => setActions(obj.key, on ? Array.from(new Set([...acts, ...allowed])) : [])}
        aria-label={t('access.matrix.allFor', { object: plural })}
      />
    );
    return {
      key: obj.key,
      label: plural,
      module: obj.module,
      supported: obj.actions,
      muted: allowed.length === 0,
      lead: allowed.length === 0 && acts.length === 0 ? locked(lead, notYours) : lead,
      aside: allowed.length === 0 ? <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground"><Lock className="size-3" aria-hidden />{notYours}</span> : undefined,
      scope: (
        <Select
          value={scope}
          onChange={(e) => emit({ ...rules, rows: { ...rules.rows, [obj.key]: { scope: e.target.value as RowScope } } })}
          disabled={disabled || acts.length === 0}
          aria-label={t('access.matrix.scopeFor', { object: obj.label.toLowerCase() })}
        >
          <option value="own">{t('access.matrix.scope.own')}</option>
          {/* Keep an existing "all records" grant visible even when the admin can't give it. */}
          <option value="workspace" disabled={!workspaceOk && scope !== 'workspace'}>
            {t('access.matrix.scope.workspace')}
            {!workspaceOk ? ` — ${notYours}` : ''}
          </option>
        </Select>
      )
    };
  });

  const grantableCaps = catalog.capabilities.filter((c) => canGrantCapability(grantable, c.key) || rules.capabilities.includes(c.key));
  const lockedCaps = catalog.capabilities.filter((c) => !grantableCaps.includes(c));

  return (
    <div className="space-y-4">
      <div>
        <AccessMatrix
          catalog={catalog}
          rows={rows}
          label={t('access.sets.permissions')}
          renderCell={(row, action, variant) => {
            const actionLabel = catalog.actions.find((a) => a.key === action)?.label ?? action;
            const checked = (rules.objects[row.key] ?? []).includes(action);
            const allowed = canGrantAction(grantable, row.key, action);
            const box = (
              <Checkbox
                className={cn(variant === 'table' && 'inline-flex justify-center')}
                checked={checked}
                // Beyond your access: can be removed, never added.
                disabled={disabled || (!allowed && !checked)}
                onCheckedChange={(on) => toggle(row.key, action, on)}
                label={variant === 'card' ? actionLabel : undefined}
                aria-label={variant === 'table' ? t('access.matrix.actionFor', { action: actionLabel, object: row.label }) : undefined}
              />
            );
            return !allowed && !checked ? locked(box, notYours) : box;
          }}
        />
        <p className="mt-1.5 text-xs text-muted-foreground">{t('workspaceApp.admin.matrixHint')}</p>
      </div>

      {catalog.capabilities.length > 0 ? (
        <fieldset className="rounded-lg border">
          <legend className="sr-only">{t('access.matrix.capabilities')}</legend>
          <div className="border-b bg-muted/50 px-3 py-2">
            <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.matrix.capabilities')}</span>
          </div>
          <div className="divide-y">
            {grantableCaps.map((cap) => (
              <Switch
                key={cap.key}
                id={`${uid}-cap-${cap.key}`}
                className="px-3 py-2.5"
                label={cap.label}
                description={canGrantCapability(grantable, cap.key) ? cap.description : `${cap.description} · ${notYours}`}
                checked={rules.capabilities.includes(cap.key)}
                disabled={disabled || (!canGrantCapability(grantable, cap.key) && !rules.capabilities.includes(cap.key))}
                onCheckedChange={(on) => {
                  if (on && !canGrantCapability(grantable, cap.key)) return;
                  emit({ ...rules, capabilities: on ? [...rules.capabilities, cap.key] : rules.capabilities.filter((c) => c !== cap.key) });
                }}
              />
            ))}
            {lockedCaps.map((cap) => (
              <div key={cap.key} className="flex items-start justify-between gap-4 px-3 py-2.5 opacity-60">
                <div className="min-w-0 leading-5">
                  <p className="text-[13px] font-medium text-foreground">{cap.label}</p>
                  <p className="text-xs text-muted-foreground">{cap.description}</p>
                </div>
                <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground">
                  <Lock className="size-3" aria-hidden />
                  {notYours}
                </span>
              </div>
            ))}
          </div>
        </fieldset>
      ) : null}
    </div>
  );
}
