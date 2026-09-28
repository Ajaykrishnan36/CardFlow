import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccessCatalog, AccessRules, ObjectAction, RowScope } from '@crm/api/types';
import { Checkbox, Select, Switch } from '@crm/components/ui/form-controls';
import { AccessMatrix, TriCheckbox, type MatrixRow } from './access-matrix';
import { DELEGATION_CAPABILITIES, normalizeRules, pluralLabel } from './use-access';

export interface PermissionSetEditorProps {
  value: AccessRules;
  onChange: (rules: AccessRules) => void;
  catalog: AccessCatalog;
  disabled?: boolean;
}

/**
 * Objects × actions checkboxes, a records scope per object and capability switches.
 * Checking any action checks Read; unchecking Read clears the row (server enforces the same).
 */
export function PermissionSetEditor({ value, onChange, catalog, disabled }: PermissionSetEditorProps) {
  const { t } = useTranslation();
  const uid = useId();
  const rules = normalizeRules(value, catalog);

  const emit = (next: AccessRules) => onChange(normalizeRules(next, catalog));

  const setActions = (objKey: string, actions: ObjectAction[]) => {
    const objects = { ...rules.objects, [objKey]: actions };
    const rows = { ...rules.rows };
    if (actions.length && !rows[objKey]) rows[objKey] = { scope: value.rows?.[objKey]?.scope ?? 'own' };
    emit({ ...rules, objects, rows });
  };

  const toggle = (objKey: string, action: ObjectAction, on: boolean) => {
    const current = rules.objects[objKey] ?? [];
    if (action === 'read' && !on) return setActions(objKey, []);
    const next = on ? Array.from(new Set<ObjectAction>([...current, action, 'read'])) : current.filter((a) => a !== action);
    setActions(objKey, next);
  };

  const setScope = (objKey: string, scope: RowScope) => emit({ ...rules, rows: { ...rules.rows, [objKey]: { scope } } });

  const setCapability = (key: string, on: boolean) =>
    emit({ ...rules, capabilities: on ? [...rules.capabilities, key] : rules.capabilities.filter((c) => c !== key) });

  const rows: MatrixRow[] = catalog.objects.map((obj) => {
    const acts = rules.objects[obj.key] ?? [];
    const all = obj.actions.every((a) => acts.includes(a));
    const plural = pluralLabel(obj.label);
    return {
      key: obj.key,
      label: plural,
      module: obj.module,
      supported: obj.actions,
      lead: (
        <TriCheckbox
          checked={all}
          indeterminate={acts.length > 0}
          disabled={disabled}
          onCheckedChange={(on) => setActions(obj.key, on ? [...obj.actions] : [])}
          aria-label={t('access.matrix.allFor', { object: plural })}
        />
      ),
      scope: (
        <Select
          value={rules.rows[obj.key]?.scope ?? 'own'}
          onChange={(e) => setScope(obj.key, e.target.value as RowScope)}
          disabled={disabled || acts.length === 0}
          aria-label={t('access.matrix.scopeFor', { object: obj.label.toLowerCase() })}
          options={[
            { value: 'own', label: t('access.matrix.scope.own') },
            { value: 'workspace', label: t('access.matrix.scope.workspace') }
          ]}
        />
      )
    };
  });

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
            return (
              <Checkbox
                className={variant === 'table' ? 'inline-flex justify-center' : undefined}
                checked={checked}
                disabled={disabled}
                onCheckedChange={(on) => toggle(row.key, action, on)}
                label={variant === 'card' ? actionLabel : undefined}
                aria-label={variant === 'table' ? t('access.matrix.actionFor', { action: actionLabel, object: row.label }) : undefined}
              />
            );
          }}
        />
        <p className="mt-1.5 text-xs text-muted-foreground">{t('access.editor.readImplied')}</p>
      </div>

      {catalog.capabilities.length > 0 ? (
        <fieldset className="rounded-lg border">
          <legend className="sr-only">{t('access.matrix.capabilities')}</legend>
          <div className="flex items-baseline justify-between gap-2 border-b bg-muted/50 px-3 py-2">
            <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.matrix.capabilities')}</span>
            <span className="text-[11px] text-muted-foreground">{t('access.editor.capabilitiesHint')}</span>
          </div>
          <div className="divide-y">
            {catalog.capabilities.map((cap) => (
              <Switch
                key={cap.key}
                id={`${uid}-cap-${cap.key}`}
                className="px-3 py-2.5"
                label={cap.label}
                description={
                  DELEGATION_CAPABILITIES.includes(cap.key) ? (
                    <>
                      {cap.description}
                      <span className="mt-0.5 block text-[11px] font-medium text-primary">{t('access.editor.delegationNote')}</span>
                    </>
                  ) : (
                    cap.description
                  )
                }
                checked={rules.capabilities.includes(cap.key)}
                disabled={disabled}
                onCheckedChange={(on) => setCapability(cap.key, on)}
              />
            ))}
          </div>
        </fieldset>
      ) : null}
    </div>
  );
}
