import { useState, type Dispatch, type ReactNode, type SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { objectsApi } from '@crm/api/endpoints';
import { NewObjectDialog } from '@crm/features/objects/object-utils';
import { ArrowDown, ArrowUp, Check, Lock, Plus, Rocket, Trash2 } from 'lucide-react';
import type { ModuleInfo, ProductConfig, ProductDetail, SystemRoleKey } from '@crm/api/types';
import { Alert, Badge } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Checkbox, Switch, Textarea } from '@crm/components/ui/form-controls';
import { cn } from '@crm/lib/utils';
import { ACCENT_COLORS, PRODUCT_ICON_KEYS, PRODUCT_ICONS, ProductIcon } from './product-icon';
import { moveItem, stepForField, syncedKey, type SetupStep } from './product-utils';
import type { ProductDraft } from './use-product-draft';

type UpdateConfig = (patch: Partial<ProductConfig>) => void;

/** Section wrapper used inside each step. */
export function StepSection({ title, description, children, aside }: { title: string; description?: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h3 className="text-[13px] font-semibold text-foreground">{title}</h3>
          {description ? <p className="mt-0.5 text-xs text-muted-foreground">{description}</p> : null}
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}

function RowActions({ index, length, onMove, onRemove, removeLabel }: { index: number; length: number; onMove: (d: -1 | 1) => void; onRemove: () => void; removeLabel: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex shrink-0 items-center gap-0.5">
      <Button type="button" variant="subtle" size="icon-sm" onClick={() => onMove(-1)} disabled={index === 0} aria-label={t('products.setup.moveUp')}>
        <ArrowUp />
      </Button>
      <Button type="button" variant="subtle" size="icon-sm" onClick={() => onMove(1)} disabled={index === length - 1} aria-label={t('products.setup.moveDown')}>
        <ArrowDown />
      </Button>
      <Button type="button" variant="subtle" size="icon-sm" onClick={onRemove} aria-label={removeLabel} className="hover:text-danger">
        <Trash2 />
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------- 1. General
export function GeneralStep({ draft, setDraft, nameError }: { draft: ProductDraft; setDraft: Dispatch<SetStateAction<ProductDraft>>; nameError?: string }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3 rounded-lg border bg-muted/30 p-3">
        <ProductIcon icon={draft.icon} accent={draft.config.accentColor} size="lg" />
        <div className="min-w-0">
          <p className="truncate text-sm font-semibold text-foreground">{draft.name || t('products.setup.general.untitled')}</p>
          <p className="truncate text-xs text-muted-foreground">{draft.description || t('products.setup.general.previewHint')}</p>
        </div>
      </div>

      <div className="grid gap-4">
        <Field label={t('products.create.name')} error={nameError}>
          <Input value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} maxLength={80} />
        </Field>
        <Field label={t('products.create.descriptionLabel')}>
          <Textarea
            value={draft.description}
            onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
            placeholder={t('products.create.descriptionPlaceholder')}
            rows={3}
          />
        </Field>
      </div>

      <StepSection title={t('products.setup.general.icon')}>
        <div className="grid grid-cols-6 gap-2 sm:grid-cols-12" role="radiogroup" aria-label={t('products.setup.general.icon')}>
          {PRODUCT_ICON_KEYS.map((key) => {
            const Icon = PRODUCT_ICONS[key];
            const selected = draft.icon === key;
            return (
              <button
                key={key}
                type="button"
                role="radio"
                aria-checked={selected}
                aria-label={key}
                title={key}
                onClick={() => setDraft((d) => ({ ...d, icon: key }))}
                className={cn(
                  'grid aspect-square place-items-center rounded-lg border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  selected ? 'border-primary bg-primary-soft text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                <Icon className="size-[18px]" aria-hidden />
              </button>
            );
          })}
        </div>
      </StepSection>

      <StepSection title={t('products.setup.general.accent')}>
        <div className="flex flex-wrap gap-2.5" role="radiogroup" aria-label={t('products.setup.general.accent')}>
          {ACCENT_COLORS.map((hex) => {
            const selected = draft.config.accentColor.toLowerCase() === hex.toLowerCase();
            return (
              <button
                key={hex}
                type="button"
                role="radio"
                aria-checked={selected}
                aria-label={hex}
                onClick={() => setDraft((d) => ({ ...d, config: { ...d.config, accentColor: hex } }))}
                className={cn(
                  'grid size-8 place-items-center rounded-full ring-offset-2 ring-offset-background transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  selected && 'ring-2 ring-foreground/70'
                )}
                style={{ backgroundColor: hex }}
              >
                {selected ? <Check className="size-4 text-white" strokeWidth={3} aria-hidden /> : null}
              </button>
            );
          })}
        </div>
      </StepSection>
    </div>
  );
}

// ---------------------------------------------------------------- 2. Modules
function titleCase(s: string) {
  return s.replace(/[_-]+/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

export function ModulesStep({
  catalog,
  modules,
  updateConfig,
  onObjectCreated
}: {
  catalog: ModuleInfo[];
  modules: string[];
  updateConfig: UpdateConfig;
  /** A new custom object was created from here: switch its module on. */
  onObjectCreated?: (moduleKey: string) => void;
}) {
  const { t } = useTranslation();
  const [creating, setCreating] = useState(false);
  const icons = useQuery({ queryKey: ['platform', 'objects'], queryFn: objectsApi.list, enabled: creating });
  // Connected-app modules only show on the product that already uses them.
  const visible = catalog.filter((m) => !m.hidden || modules.includes(m.key));
  const groups = new Map<string, ModuleInfo[]>();
  for (const m of visible) groups.set(m.group, [...(groups.get(m.group) ?? []), m]);
  const toggle = (key: string) => updateConfig({ modules: modules.includes(key) ? modules.filter((k) => k !== key) : [...modules, key] });
  const planned = visible.filter((m) => !m.available && modules.includes(m.key));

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 rounded-lg border border-dashed p-3 sm:flex-row sm:items-center">
        <p className="flex-1 text-[13px] text-muted-foreground">{t('products.setup.modules.objectsIntro')}</p>
        <div className="flex shrink-0 gap-2">
          <Button type="button" variant="outline" size="sm" asChild>
            <Link to="/crm/owner/objects">{t('products.setup.modules.manageObjects')}</Link>
          </Button>
          <Button type="button" size="sm" onClick={() => setCreating(true)}>
            <Plus /> {t('products.setup.modules.newObject')}
          </Button>
        </div>
      </div>
      {modules.length === 0 ? <Alert tone="warning">{t('products.setup.modules.noneHint')}</Alert> : null}
      {planned.length ? <Alert tone="info">{t('products.setup.modules.plannedHint', { list: planned.map((m) => m.label).join(', ') })}</Alert> : null}
      {catalog.length === 0 ? <p className="text-[13px] text-muted-foreground">{t('products.setup.modules.catalogEmpty')}</p> : null}
      {[...groups.entries()].map(([group, items]) => (
        <StepSection key={group} title={t(`products.moduleGroups.${group}`, { defaultValue: titleCase(group) })}>
          <div className="grid gap-2.5 sm:grid-cols-2 xl:grid-cols-3">
            {items.map((m) => {
              const on = modules.includes(m.key);
              const editable = m.objects?.find((o) => !['leads', 'accounts', 'contacts'].includes(o));
              const disabled = !m.available && !on;
              return (
                <div
                  key={m.key}
                  className={cn(
                    'relative flex flex-col rounded-lg border transition-colors',
                    on ? 'border-primary/50 bg-primary-soft/60' : disabled ? 'opacity-60' : 'hover:bg-muted/60'
                  )}
                >
                  <button
                    type="button"
                    role="switch"
                    aria-checked={on}
                    disabled={disabled}
                    onClick={() => toggle(m.key)}
                    className="flex flex-1 items-start gap-3 rounded-lg p-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed"
                  >
                    <span
                      className={cn(
                        'mt-0.5 grid size-4 shrink-0 place-items-center rounded border transition-colors',
                        on ? 'border-primary bg-primary text-primary-foreground' : 'border-input bg-background'
                      )}
                      aria-hidden
                    >
                      {on ? <Check className="size-3" strokeWidth={3} /> : null}
                    </span>
                    <span className="min-w-0">
                      <span className="flex flex-wrap items-center gap-1.5 text-[13px] font-medium text-foreground">
                        {m.label}
                        {m.custom ? <Badge tone="primary">{t('products.setup.modules.customBadge')}</Badge> : null}
                        {!m.available ? <Badge tone="neutral">{t('products.setup.modules.plannedBadge')}</Badge> : null}
                      </span>
                      <span className="mt-0.5 block text-xs leading-4 text-muted-foreground">{m.description}</span>
                    </span>
                  </button>
                  {editable ? (
                    <Link
                      to={`/crm/owner/objects/${encodeURIComponent(editable)}`}
                      className="border-t px-3 py-1.5 text-xs font-medium text-primary hover:underline"
                    >
                      {t('products.setup.modules.editObject')}
                    </Link>
                  ) : null}
                </div>
              );
            })}
          </div>
        </StepSection>
      ))}
      <NewObjectDialog
        open={creating}
        onOpenChange={setCreating}
        icons={icons.data?.icons ?? ['box']}
        navigateOnCreate={false}
        onCreated={(d) => onObjectCreated?.(d.module)}
      />
    </div>
  );
}

// ---------------------------------------------------------------- 3. Roles & user types
export function RolesStep({ config, updateConfig }: { config: ProductConfig; updateConfig: UpdateConfig }) {
  const { t } = useTranslation();
  const enabledRoles = config.roles.filter((r) => r.enabled);
  const setUserType = (i: number, patch: Partial<ProductConfig['userTypes'][number]>) =>
    updateConfig({ userTypes: config.userTypes.map((u, j) => (j === i ? { ...u, ...patch } : u)) });

  return (
    <div className="space-y-8">
      <StepSection title={t('products.setup.roles.rolesTitle')} description={t('products.setup.roles.rolesBody')}>
        <ul className="divide-y rounded-lg border">
          {config.roles.map((r, i) => {
            const locked = r.key === 'SUPER_ADMIN';
            return (
              <li key={r.key} className="flex flex-col gap-2 px-3 py-2.5 sm:flex-row sm:items-center sm:gap-3">
                <span className="w-28 shrink-0 font-mono text-[11px] text-muted-foreground">{r.key}</span>
                <Input
                  className="sm:max-w-xs"
                  value={r.label}
                  aria-label={t('products.setup.roles.roleLabel', { key: r.key })}
                  onChange={(e) => updateConfig({ roles: config.roles.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)) })}
                />
                <div className="flex items-center gap-2 sm:ml-auto">
                  {locked ? (
                    <span className="flex items-center gap-1 text-xs text-muted-foreground">
                      <Lock className="size-3.5" aria-hidden /> {t('products.setup.roles.alwaysOn')}
                    </span>
                  ) : null}
                  <Switch
                    checked={r.enabled || locked}
                    disabled={locked}
                    aria-label={t('products.setup.roles.enable', { role: r.label || r.key })}
                    onCheckedChange={(v) => {
                      const roles = config.roles.map((x, j) => (j === i ? { ...x, enabled: v } : x));
                      // Disabling a role also removes it from every user type.
                      const userTypes = v ? config.userTypes : config.userTypes.map((u) => ({ ...u, allowedRoles: u.allowedRoles.filter((k) => k !== r.key) }));
                      updateConfig({ roles, userTypes });
                    }}
                  />
                </div>
              </li>
            );
          })}
        </ul>
      </StepSection>

      <StepSection
        title={t('products.setup.roles.userTypesTitle')}
        description={t('products.setup.roles.userTypesBody')}
        aside={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => updateConfig({ userTypes: [...config.userTypes, { key: '', label: '', description: '', allowedRoles: ['END_USER'] as SystemRoleKey[] }] })}
          >
            <Plus /> {t('products.setup.roles.addUserType')}
          </Button>
        }
      >
        {config.userTypes.length === 0 ? <Alert tone="warning">{t('products.setup.roles.noUserTypes')}</Alert> : null}
        <div className="space-y-3">
          {config.userTypes.map((u, i) => (
            <div key={i} className="rounded-lg border p-3">
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label={t('products.setup.roles.utLabel')}>
                  <Input
                    value={u.label}
                    placeholder={t('products.setup.roles.utLabelPlaceholder')}
                    onChange={(e) => setUserType(i, { label: e.target.value, key: syncedKey(u.key, u.label, e.target.value) })}
                  />
                </Field>
                <Field label={t('products.setup.roles.utKey')}>
                  <Input
                    className="font-mono"
                    value={u.key}
                    spellCheck={false}
                    autoCapitalize="none"
                    onChange={(e) => setUserType(i, { key: e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, '') })}
                  />
                </Field>
                <Field label={t('products.setup.roles.utDescription')} className="sm:col-span-2">
                  <Input value={u.description ?? ''} onChange={(e) => setUserType(i, { description: e.target.value })} />
                </Field>
              </div>
              <div className="mt-3 flex flex-wrap items-end justify-between gap-3">
                <fieldset>
                  <legend className="mb-1.5 text-[13px] font-medium text-foreground">{t('products.setup.roles.utRoles')}</legend>
                  <div className="flex flex-wrap gap-x-4 gap-y-2">
                    {enabledRoles.map((r) => (
                      <Checkbox
                        key={r.key}
                        label={r.label || r.key}
                        checked={u.allowedRoles.includes(r.key)}
                        onCheckedChange={(v) =>
                          setUserType(i, { allowedRoles: v ? [...u.allowedRoles, r.key] : u.allowedRoles.filter((k) => k !== r.key) })
                        }
                      />
                    ))}
                  </div>
                </fieldset>
                <Button
                  type="button"
                  variant="subtle"
                  size="sm"
                  className="hover:text-danger"
                  onClick={() => updateConfig({ userTypes: config.userTypes.filter((_, j) => j !== i) })}
                >
                  <Trash2 /> {t('products.setup.remove')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      </StepSection>
    </div>
  );
}

// ---------------------------------------------------------------- 4. Pipeline & conversion
export function PipelineStep({ config, updateConfig }: { config: ProductConfig; updateConfig: UpdateConfig }) {
  const { t } = useTranslation();
  const statuses = config.leadStatuses;
  const stages = config.pipelineStages;

  return (
    <div className="space-y-8">
      <StepSection
        title={t('products.setup.pipeline.statusesTitle')}
        description={t('products.setup.pipeline.statusesBody')}
        aside={
          <Button type="button" variant="outline" size="sm" onClick={() => updateConfig({ leadStatuses: [...statuses, { value: '', label: '' }] })}>
            <Plus /> {t('products.setup.pipeline.addStatus')}
          </Button>
        }
      >
        {statuses.length === 0 ? <Alert tone="warning">{t('products.setup.pipeline.noStatuses')}</Alert> : null}
        <ol className="space-y-2">
          {statuses.map((s, i) => (
            <li key={i} className="flex flex-col gap-2 rounded-lg border p-2 sm:flex-row sm:items-center">
              <span className="hidden w-6 shrink-0 text-center text-xs tabular-nums text-muted-foreground sm:block">{i + 1}</span>
              <Input
                value={s.label}
                placeholder={t('products.setup.pipeline.statusLabel')}
                aria-label={t('products.setup.pipeline.statusLabel')}
                onChange={(e) =>
                  updateConfig({
                    leadStatuses: statuses.map((x, j) => (j === i ? { label: e.target.value, value: syncedKey(x.value, x.label, e.target.value) } : x))
                  })
                }
              />
              <div className="flex items-center gap-2">
                <Input
                  className="font-mono sm:w-44"
                  value={s.value}
                  placeholder={t('products.setup.pipeline.statusValue')}
                  aria-label={t('products.setup.pipeline.statusValue')}
                  spellCheck={false}
                  autoCapitalize="none"
                  onChange={(e) =>
                    updateConfig({ leadStatuses: statuses.map((x, j) => (j === i ? { ...x, value: e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, '') } : x)) })
                  }
                />
                <RowActions
                  index={i}
                  length={statuses.length}
                  onMove={(d) => updateConfig({ leadStatuses: moveItem(statuses, i, d) })}
                  onRemove={() => updateConfig({ leadStatuses: statuses.filter((_, j) => j !== i) })}
                  removeLabel={t('products.setup.remove')}
                />
              </div>
            </li>
          ))}
        </ol>
      </StepSection>

      <StepSection
        title={t('products.setup.pipeline.stagesTitle')}
        description={t('products.setup.pipeline.stagesBody')}
        aside={
          <Button type="button" variant="outline" size="sm" onClick={() => updateConfig({ pipelineStages: [...stages, { key: '', label: '', probability: 50 }] })}>
            <Plus /> {t('products.setup.pipeline.addStage')}
          </Button>
        }
      >
        {stages.length === 0 ? <Alert tone="warning">{t('products.setup.pipeline.noStages')}</Alert> : null}
        <ol className="space-y-2">
          {stages.map((s, i) => (
            <li key={i} className="flex flex-col gap-2 rounded-lg border p-2 sm:flex-row sm:items-center">
              <span className="hidden w-6 shrink-0 text-center text-xs tabular-nums text-muted-foreground sm:block">{i + 1}</span>
              <Input
                value={s.label}
                placeholder={t('products.setup.pipeline.stageLabel')}
                aria-label={t('products.setup.pipeline.stageLabel')}
                onChange={(e) =>
                  updateConfig({
                    pipelineStages: stages.map((x, j) => (j === i ? { ...x, label: e.target.value, key: syncedKey(x.key, x.label, e.target.value) } : x))
                  })
                }
              />
              <div className="flex items-center gap-2">
                <Input
                  className="sm:w-28"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  max={100}
                  value={Number.isFinite(s.probability) ? s.probability : 0}
                  aria-label={t('products.setup.pipeline.probability')}
                  trailing={<span className="pr-2 text-xs text-muted-foreground">%</span>}
                  onChange={(e) => {
                    const n = Math.max(0, Math.min(100, Math.round(Number(e.target.value) || 0)));
                    updateConfig({ pipelineStages: stages.map((x, j) => (j === i ? { ...x, probability: n } : x)) });
                  }}
                />
                <RowActions
                  index={i}
                  length={stages.length}
                  onMove={(d) => updateConfig({ pipelineStages: moveItem(stages, i, d) })}
                  onRemove={() => updateConfig({ pipelineStages: stages.filter((_, j) => j !== i) })}
                  removeLabel={t('products.setup.remove')}
                />
              </div>
            </li>
          ))}
        </ol>
      </StepSection>

      <StepSection title={t('products.setup.pipeline.conversionTitle')} description={t('products.setup.pipeline.conversionBody')}>
        <div className="divide-y rounded-lg border">
          {(['createContact', 'createOpportunity', 'requireQualified'] as const).map((k) => (
            <Switch
              key={k}
              id={`conv-${k}`}
              className="px-3 py-3"
              label={t(`products.setup.pipeline.${k}`)}
              description={t(`products.setup.pipeline.${k}Hint`)}
              checked={config.conversion[k]}
              onCheckedChange={(v) => updateConfig({ conversion: { ...config.conversion, [k]: v } })}
            />
          ))}
        </div>
      </StepSection>
    </div>
  );
}

// ---------------------------------------------------------------- 5. Login & integrations
export function LoginStep({ config, updateConfig }: { config: ProductConfig; updateConfig: UpdateConfig }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-8">
      <StepSection title={t('products.setup.login.methodsTitle')} description={t('products.setup.login.methodsBody')}>
        <div className="divide-y rounded-lg border">
          <Switch
            id="login-password"
            className="px-3 py-3"
            label={
              <span className="inline-flex items-center gap-1.5">
                {t('products.setup.login.password')} <Lock className="size-3 text-muted-foreground" aria-hidden />
              </span>
            }
            description={t('products.setup.login.passwordHint')}
            checked
            disabled
            onCheckedChange={() => undefined}
          />
          {(['otp', 'google', 'linkedin'] as const).map((k) => (
            <Switch
              key={k}
              id={`login-${k}`}
              className="px-3 py-3"
              label={t(`products.setup.login.${k}`)}
              description={t('products.setup.login.notYetEnforced')}
              checked={config.loginMethods[k]}
              onCheckedChange={(v) => updateConfig({ loginMethods: { ...config.loginMethods, [k]: v, password: true } })}
            />
          ))}
        </div>
      </StepSection>

      <StepSection title={t('products.setup.login.accessTitle')}>
        <div className="divide-y rounded-lg border">
          <Switch
            id="self-registration"
            className="px-3 py-3"
            label={t('products.setup.login.selfRegistration')}
            description={t('products.setup.login.selfRegistrationHint')}
            checked={config.selfRegistration}
            onCheckedChange={(v) => updateConfig({ selfRegistration: v })}
          />
        </div>
      </StepSection>

      <StepSection title={t('products.setup.login.integrationsTitle')}>
        <div className="divide-y rounded-lg border">
          {(['apiAccess', 'webhooks'] as const).map((k) => (
            <Switch
              key={k}
              id={`int-${k}`}
              className="px-3 py-3"
              label={t(`products.setup.login.${k}`)}
              description={t(`products.setup.login.${k}Hint`)}
              checked={config.integrations[k]}
              onCheckedChange={(v) => updateConfig({ integrations: { ...config.integrations, [k]: v } })}
            />
          ))}
        </div>
      </StepSection>
    </div>
  );
}

// ---------------------------------------------------------------- 6. Review & publish
function ReviewBlock({ title, onEdit, children }: { title: string; onEdit: () => void; children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b bg-muted/30 px-3 py-2">
        <h3 className="text-[13px] font-semibold text-foreground">{title}</h3>
        <Button type="button" variant="link" size="sm" className="text-xs" onClick={onEdit}>
          {t('products.setup.review.edit')}
        </Button>
      </div>
      <div className="space-y-2 px-3 py-3 text-[13px]">{children}</div>
    </div>
  );
}

function Chips({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) return <p className="text-muted-foreground/70">{empty}</p>;
  return (
    <div className="flex flex-wrap gap-1.5">
      {items.map((s, i) => (
        <span key={`${s}-${i}`} className="rounded-md border bg-muted/50 px-2 py-0.5 text-xs font-medium">
          {s}
        </span>
      ))}
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[140px_minmax(0,1fr)] sm:gap-3">
      <span className="text-xs font-medium text-muted-foreground sm:pt-0.5">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function ReviewStep({
  product,
  draft,
  publishErrors,
  goTo,
  publishable,
  publishing,
  onPublish,
  version
}: {
  product: ProductDetail;
  draft: ProductDraft;
  publishErrors: Record<string, string> | null;
  goTo: (s: SetupStep) => void;
  publishable: boolean;
  publishing: boolean;
  onPublish: () => void;
  version: number;
}) {
  const { t } = useTranslation();
  const c = draft.config;
  const none = t('products.overview.noneYet');
  const on = (v: boolean) => (v ? t('products.setup.review.on') : t('products.setup.review.off'));
  const moduleLabel = (key: string) => product.moduleCatalog.find((m) => m.key === key)?.label ?? key;
  const errors = publishErrors ? Object.entries(publishErrors) : [];

  return (
    <div className="space-y-4">
      {errors.length > 0 ? (
        <Alert tone="danger" title={t('products.setup.review.errorsTitle')}>
          <ul className="mt-1 space-y-1">
            {errors.map(([field, msg]) => (
              <li key={field} className="flex flex-wrap items-baseline gap-x-2">
                <span className="text-foreground">{msg}</span>
                <button type="button" className="text-xs font-medium text-primary hover:underline" onClick={() => goTo(stepForField(field))}>
                  {t('products.setup.review.fix', { step: t(`products.setup.steps.${stepForField(field)}.title`) })}
                </button>
              </li>
            ))}
          </ul>
        </Alert>
      ) : null}

      <ReviewBlock title={t('products.setup.steps.general.title')} onEdit={() => goTo('general')}>
        <div className="flex items-center gap-3">
          <ProductIcon icon={draft.icon} accent={c.accentColor} />
          <div className="min-w-0">
            <p className="font-medium text-foreground">{draft.name || t('products.setup.general.untitled')}</p>
            <p className="font-mono text-xs text-muted-foreground">{product.key}</p>
          </div>
        </div>
        {draft.description ? <p className="text-muted-foreground">{draft.description}</p> : null}
      </ReviewBlock>

      <ReviewBlock title={t('products.setup.steps.modules.title')} onEdit={() => goTo('modules')}>
        <Chips items={c.modules.map(moduleLabel)} empty={none} />
      </ReviewBlock>

      <ReviewBlock title={t('products.setup.steps.roles.title')} onEdit={() => goTo('roles')}>
        <Row label={t('products.setup.roles.rolesTitle')}>
          <Chips items={c.roles.filter((r) => r.enabled).map((r) => r.label || r.key)} empty={none} />
        </Row>
        <Row label={t('products.setup.roles.userTypesTitle')}>
          {c.userTypes.length === 0 ? (
            <p className="text-muted-foreground/70">{none}</p>
          ) : (
            <ul className="space-y-1">
              {c.userTypes.map((u, i) => (
                <li key={i}>
                  <span className="font-medium">{u.label || u.key || '—'}</span>{' '}
                  <span className="text-xs text-muted-foreground">
                    · {u.allowedRoles.map((k) => c.roles.find((r) => r.key === k)?.label ?? k).join(', ') || none}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Row>
      </ReviewBlock>

      <ReviewBlock title={t('products.setup.steps.pipeline.title')} onEdit={() => goTo('pipeline')}>
        <Row label={t('products.setup.pipeline.statusesTitle')}>
          <Chips items={c.leadStatuses.map((s) => s.label || s.value)} empty={none} />
        </Row>
        <Row label={t('products.setup.pipeline.stagesTitle')}>
          <Chips items={c.pipelineStages.map((s) => `${s.label || s.key} · ${s.probability}%`)} empty={none} />
        </Row>
        <Row label={t('products.setup.pipeline.conversionTitle')}>
          <span className="text-muted-foreground">
            {t('products.setup.pipeline.createContact')}: {on(c.conversion.createContact)} · {t('products.setup.pipeline.createOpportunity')}:{' '}
            {on(c.conversion.createOpportunity)} · {t('products.setup.pipeline.requireQualified')}: {on(c.conversion.requireQualified)}
          </span>
        </Row>
      </ReviewBlock>

      <ReviewBlock title={t('products.setup.steps.login.title')} onEdit={() => goTo('login')}>
        <Row label={t('products.setup.login.methodsTitle')}>
          <Chips
            items={(['password', 'otp', 'google', 'linkedin'] as const).filter((k) => c.loginMethods[k]).map((k) => t(`products.setup.login.${k}`))}
            empty={none}
          />
        </Row>
        <Row label={t('products.setup.login.selfRegistration')}>{on(c.selfRegistration)}</Row>
        <Row label={t('products.setup.login.integrationsTitle')}>
          <span className="text-muted-foreground">
            {t('products.setup.login.apiAccess')}: {on(c.integrations.apiAccess)} · {t('products.setup.login.webhooks')}: {on(c.integrations.webhooks)}
          </span>
        </Row>
      </ReviewBlock>

      <div className="flex flex-col gap-3 rounded-lg border border-primary/20 bg-primary-soft/60 p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0">
          <p className="flex items-center gap-2 text-[13px] font-semibold text-foreground">
            {t('products.setup.review.readyTitle', { version })}
            {product.currentVersion ? <Badge>{t('products.setup.review.currentIs', { version: product.currentVersion })}</Badge> : null}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {publishable ? t('products.setup.review.readyBody') : t('products.setup.review.nothingToPublish')}
          </p>
        </div>
        <Button type="button" onClick={onPublish} disabled={!publishable} loading={publishing} className="shrink-0">
          <Rocket /> {t('products.publish.button', { version })}
        </Button>
      </div>
    </div>
  );
}
