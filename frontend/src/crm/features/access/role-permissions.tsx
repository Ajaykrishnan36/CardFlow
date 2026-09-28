import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Minus } from 'lucide-react';
import type { AccessCatalog, AccessRules, RoleKey } from '@crm/api/types';
import { isApiError } from '@crm/api/client';
import { Alert, Badge } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { SegmentedFilter } from '@crm/components/page';
import { ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { AccessMatrix, GrantMark, ScopeBadge, type MatrixRow } from './access-matrix';
import { hasAnyGrant, normalizeRules, pluralLabel, roleOptionsFor, useAccessCatalog, useAccessWorkspaces, useWorkspaceRoles, type RoleOption } from './use-access';

/** Roles offered in a workspace (built-in + custom); built-in only when no workspace yet. */
export function useRoleOptions(workspaceId?: string) {
  const catalog = useAccessCatalog();
  const workspaces = useAccessWorkspaces();
  const roles = useWorkspaceRoles(workspaceId);
  const ws = workspaceId ? workspaces.data?.find((w) => w.id === workspaceId) : undefined;
  const options = roleOptionsFor(workspaceId ? roles.data : undefined, ws, catalog.data);
  const loading = workspaceId ? roles.isLoading && !ws : catalog.isLoading;
  return { options, loading, rulesLoading: workspaceId ? roles.isLoading : false, error: workspaceId ? roles.error : catalog.error, refetch: workspaceId ? roles.refetch : catalog.refetch };
}

/** Display name of a role in a workspace; catalog / i18n fallback otherwise. */
export function useRoleName(workspaceId?: string) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const { options } = useRoleOptions(workspaceId);
  return (key?: string | null) => {
    if (!key) return t('users.roles.none');
    return (
      options.find((r) => r.key === key)?.name ??
      catalog.data?.roles.find((r) => r.key === key)?.name ??
      t(`access.roles.${key}.name`, { defaultValue: key })
    );
  };
}

/** Built-in / Custom / Edited. */
export function RoleBadge({ role, showBuiltIn }: { role: Pick<RoleOption, 'isSystem' | 'customized'>; showBuiltIn?: boolean }) {
  const { t } = useTranslation();
  if (!role.isSystem) return <Badge tone="primary">{t('access.roles.badgeCustom')}</Badge>;
  if (role.customized) return <Badge tone="warning">{t('access.roles.badgeEdited')}</Badge>;
  return showBuiltIn ? <Badge>{t('access.roles.badgeBuiltIn')}</Badge> : null;
}

function optionLabel(r: RoleOption, t: (k: string) => string): string {
  if (!r.isSystem) return `${r.name} · ${t('access.roles.badgeCustom')}`;
  if (r.customized) return `${r.name} · ${t('access.roles.badgeEdited')}`;
  return r.name;
}

function roleHint(r: RoleOption | undefined, t: (k: string, o?: Record<string, unknown>) => string): string | undefined {
  if (!r) return undefined;
  if (r.isSystem && !r.customized) return t(`access.roles.${r.key}.hint`, { defaultValue: r.description ?? '' }) || undefined;
  return r.description || undefined;
}

/**
 * Role picker for one workspace: lists its built-in and custom roles, marks custom / edited
 * ones, and links to "What can this role do?". Without a workspace (new one) only built-in roles.
 */
export function RoleField({
  workspaceId,
  value,
  onChange,
  error,
  disabled,
  label,
  linkLabel,
  placeholder,
  'aria-label': ariaLabel
}: {
  workspaceId?: string;
  value: RoleKey | undefined;
  onChange: (key: RoleKey) => void;
  error?: string;
  disabled?: boolean;
  label: string;
  linkLabel?: string;
  placeholder?: string;
  'aria-label'?: string;
}) {
  const { t } = useTranslation();
  const { options, loading } = useRoleOptions(workspaceId);
  const [info, setInfo] = useState(false);
  const current = options.find((r) => r.key === value);
  // Keep an unknown saved key selectable (e.g. a role deleted meanwhile).
  const list = value && !current && !loading ? [...options, { key: value, name: value, isSystem: false, customized: false } as RoleOption] : options;
  return (
    <>
      <Field
        label={
          <span className="inline-flex items-center gap-1.5">
            {label}
            {current ? <RoleBadge role={current} /> : null}
          </span>
        }
        error={error}
        hint={roleHint(current, t)}
        labelAside={
          <button
            type="button"
            onClick={() => setInfo(true)}
            className="text-xs font-medium text-primary hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {linkLabel ?? t('access.roles.whatCanDo')}
          </button>
        }
      >
        <Select
          value={value ?? ''}
          onChange={(e) => e.target.value && onChange(e.target.value)}
          disabled={disabled || loading}
          placeholder={value ? undefined : (placeholder ?? (loading ? t('common.loading') : t('users.roles.none')))}
          options={list.map((r) => ({ value: r.key, label: optionLabel(r, t) }))}
          aria-label={ariaLabel}
        />
      </Field>
      <RolePermissionsDialog open={info} onOpenChange={setInfo} roleKey={value ?? list[0]?.key ?? ''} workspaceId={workspaceId} />
    </>
  );
}

/** Read-only objects × actions view of a rules object. */
export function RulesMatrix({ catalog, rules, label }: { catalog: AccessCatalog; rules: AccessRules; label: string }) {
  const { t } = useTranslation();
  const n = normalizeRules(rules, catalog);
  const rows: MatrixRow[] = catalog.objects.map((obj) => {
    const acts = n.objects[obj.key] ?? [];
    return {
      key: obj.key,
      label: pluralLabel(obj.label),
      module: obj.module,
      supported: obj.actions,
      scope: <ScopeBadge scope={n.rows[obj.key]?.scope ?? 'own'} none={acts.length === 0} />
    };
  });
  return (
    <div className="space-y-3">
      <AccessMatrix
        dense
        catalog={catalog}
        rows={rows}
        label={label}
        renderCell={(row, action, variant) => (
          <GrantMark on={(n.objects[row.key] ?? []).includes(action)} label={catalog.actions.find((a) => a.key === action)?.label ?? action} variant={variant} />
        )}
      />
      {catalog.capabilities.length > 0 ? (
        <div>
          <p className="mb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{t('access.matrix.capabilities')}</p>
          <ul className="flex flex-wrap gap-1.5">
            {catalog.capabilities.map((cap) => {
              const on = n.capabilities.includes(cap.key);
              return (
                <li
                  key={cap.key}
                  title={cap.description}
                  className={cn(
                    'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs',
                    on ? 'border-success/30 bg-success-soft text-foreground' : 'text-muted-foreground'
                  )}
                >
                  {on ? <Check className="size-3 text-success" strokeWidth={3} aria-hidden /> : <Minus className="size-3" aria-hidden />}
                  {cap.label}
                  <span className="sr-only">: {on ? t('access.matrix.granted') : t('access.matrix.notGranted')}</span>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}
    </div>
  );
}

/** What a role allows: the workspace's (possibly edited) rules, or the catalog default. */
export function RolePermissions({ roleKey, workspaceId, rules: given }: { roleKey: RoleKey; workspaceId?: string; rules?: AccessRules }) {
  const { t } = useTranslation();
  const catalog = useAccessCatalog();
  const { options, rulesLoading, error, refetch } = useRoleOptions(workspaceId);
  if (!catalog.data || (rulesLoading && !given)) {
    const err = catalog.error ?? error;
    if (err) {
      return <ErrorState title={t('access.roles.errorTitle')} message={isApiError(err) ? err.message : undefined} onRetry={() => void (catalog.error ? catalog.refetch() : refetch())} />;
    }
    return (
      <div className="space-y-2" aria-busy>
        <Skeleton className="h-9" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
        <Skeleton className="h-12" />
      </div>
    );
  }
  const role = options.find((r) => r.key === roleKey);
  const rules = given ?? role?.rules ?? catalog.data.roles.find((r) => r.key === roleKey)?.rules;
  if (!rules) return null;
  return (
    <div className="space-y-3">
      {!hasAnyGrant(rules) ? <Alert tone="info">{t('access.roles.noAccess')}</Alert> : null}
      <RulesMatrix catalog={catalog.data} rules={rules} label={role?.name ?? roleKey} />
    </div>
  );
}

/** "What can this role do?" — compare the workspace's roles, starting at the given one. */
export function RolePermissionsDialog({
  open,
  onOpenChange,
  roleKey,
  workspaceId
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  roleKey: RoleKey;
  workspaceId?: string;
}) {
  const { t } = useTranslation();
  const { options } = useRoleOptions(open ? workspaceId : undefined);
  const [shown, setShown] = useState<RoleKey>(roleKey);
  useEffect(() => {
    if (open) setShown(roleKey);
  }, [open, roleKey]);
  const role = options.find((r) => r.key === shown);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[6vh] flex max-h-[88vh] max-w-2xl flex-col p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="pr-8 text-base font-semibold">{t('access.roles.dialogTitle')}</DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('access.roles.dialogDescription')}</DialogDescription>
        </div>
        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4">
          {options.length <= 5 ? (
            <SegmentedFilter<RoleKey> value={shown} onChange={setShown} options={options.map((r) => ({ value: r.key, label: r.name }))} />
          ) : (
            <Select
              className="sm:max-w-xs"
              value={shown}
              onChange={(e) => setShown(e.target.value)}
              aria-label={t('access.roles.dialogTitle')}
              options={options.map((r) => ({ value: r.key, label: optionLabel(r, t) }))}
            />
          )}
          <div className="flex flex-wrap items-center gap-2">
            {role ? <RoleBadge role={role} showBuiltIn /> : null}
            <p className="text-[13px] text-muted-foreground">{roleHint(role, t) ?? role?.description}</p>
          </div>
          <RolePermissions roleKey={shown} workspaceId={open ? workspaceId : undefined} />
        </div>
        <div className="flex justify-end border-t px-5 py-3">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
