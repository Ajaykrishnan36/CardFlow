import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { TFunction } from 'i18next';
import { AlertCircle, CircleCheck, Plus } from 'lucide-react';
import { accessApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessWorkspaceOption, GiveLoginBody, GiveLoginResult, ObjectKey, RoleKey } from '@crm/api/types';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Checkbox, Select, Switch } from '@crm/components/ui/form-controls';
import { PasswordInput, StrengthMeter } from '@crm/components/ui/password-input';
import { Skeleton } from '@crm/components/ui/spinner';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { DetailItem, DevLink, SegmentedFilter } from '@crm/components/page';
import { cn } from '@crm/lib/utils';
import { slugCode, WORKSPACE_CODE_RE } from '@crm/features/workspaces/workspace-ui';
import { PermissionSetDialog } from './permission-set-dialog';
import { RoleField, useRoleName } from './role-permissions';
import { ROLE_KEYS, useAccessWorkspaces, useActiveProducts, workspaceLabel } from './use-access';

// ---------------------------------------------------------------------------
// Helpers shared with the convert dialog and the "New user" dialog
// ---------------------------------------------------------------------------

type WorkspaceMode = GiveLoginBody['workspace']['mode'];

/** New customer workspace (prefilled) when a workspace name is suggested, else the owner's own team. */
export function defaultGiveLoginBody(defaultWorkspaceName?: string): GiveLoginBody {
  const name = defaultWorkspaceName?.trim();
  if (name) {
    return { workspace: { mode: 'new', name, code: slugCode(name), productIds: [] }, roleKey: 'SUPER_ADMIN', method: 'invite' };
  }
  return { workspace: { mode: 'platform' }, roleKey: 'STAFF', permissionSetIds: [], method: 'invite' };
}

/** Client-side checks mirroring the server's 422 keys. Empty object = OK to submit. */
export function validateGiveLogin(body: GiveLoginBody, t: TFunction): Record<string, string> {
  const e: Record<string, string> = {};
  const ws = body.workspace;
  if (ws.mode === 'existing' && !ws.workspaceId) e['workspace.workspaceId'] = t('access.login.workspaceRequired');
  if (ws.mode === 'new') {
    if (!ws.name.trim()) e['workspace.name'] = t('access.login.nameRequired');
    if (!WORKSPACE_CODE_RE.test(ws.code)) e['workspace.code'] = t('access.login.codeInvalid');
    if (ws.productIds.length === 0) e['workspace.productIds'] = t('access.login.productsRequired');
  }
  if (body.method === 'password' && (body.password ?? '').length < 8) e.password = t('access.login.passwordShort');
  return e;
}

/** Accepts keys with or without the "access." prefix used inside the convert body. */
function normalizeErrors(errors?: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(errors ?? {})) out[k.startsWith('access.') ? k.slice(7) : k] = v;
  return out;
}

const FIELD_KEYS = ['password', 'roleKey', 'productIds', 'permissionSetIds', 'workspace', 'workspace.name', 'workspace.code', 'workspace.productIds', 'workspace.workspaceId', 'method'];
export function isGiveLoginField(key: string): boolean {
  return FIELD_KEYS.includes(key.startsWith('access.') ? key.slice(7) : key);
}

function firstName(name: string): string {
  return name.trim().split(/\s+/)[0] || name;
}

// ---------------------------------------------------------------------------
// Fields
// ---------------------------------------------------------------------------

export interface GiveLoginFieldsProps {
  value: GiveLoginBody;
  onChange: (value: GiveLoginBody) => void;
  personName: string;
  email?: string;
  defaultWorkspaceName?: string;
  errors?: Record<string, string>;
  disabled?: boolean;
}

function FieldError({ children }: { children?: string }) {
  if (!children) return null;
  return (
    <p className="mt-1.5 flex items-start gap-1.5 text-[13px] text-danger animate-fade-in">
      <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
      {children}
    </p>
  );
}

function GroupLabel({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="mb-2 flex items-center justify-between gap-2">
      <legend className="text-[13px] font-medium text-foreground">{children}</legend>
      {aside}
    </div>
  );
}

/** Workspace → role → products → permission sets → invitation or temporary password. */
export function GiveLoginFields({ value, onChange, personName, email, defaultWorkspaceName, errors: rawErrors, disabled: disabledProp }: GiveLoginFieldsProps) {
  const { t } = useTranslation();
  const uid = useId();
  const errors = normalizeErrors(rawErrors);
  const noEmail = !email?.trim();
  const disabled = disabledProp || noEmail;
  const workspaces = useAccessWorkspaces();
  const mode = value.workspace.mode;
  const products = useActiveProducts(mode === 'new');

  const [codeEdited, setCodeEdited] = useState(() => value.workspace.mode === 'new' && value.workspace.code !== slugCode(value.workspace.name));
  const [roleTouched, setRoleTouched] = useState(false);
  const [newSetFor, setNewSetFor] = useState<AccessWorkspaceOption | null>(null);
  const [extraSets, setExtraSets] = useState<Array<{ id: string; name: string; workspaceId: string }>>([]);
  const autoPicked = useRef(false);

  const all = workspaces.data ?? [];
  const platform = all.find((w) => w.isPlatform);
  const customers = all.filter((w) => !w.isPlatform);
  const activeProducts = products.data?.data ?? [];
  const selectedWs: AccessWorkspaceOption | undefined =
    value.workspace.mode === 'platform' ? platform : value.workspace.mode === 'existing' ? all.find((w) => w.id === (value.workspace as { workspaceId: string }).workspaceId) : undefined;

  // A single active product is the obvious choice for a new workspace.
  useEffect(() => {
    if (autoPicked.current || value.workspace.mode !== 'new' || value.workspace.productIds.length > 0) return;
    if (activeProducts.length === 1 && activeProducts[0]) {
      autoPicked.current = true;
      onChange({ ...value, workspace: { ...value.workspace, productIds: [activeProducts[0].id] } });
    }
  }, [activeProducts, value, onChange]);

  // Built-in roles exist in every workspace; a custom role only in its own, so moving resets it.
  const isBuiltIn = (k: RoleKey) => (ROLE_KEYS as string[]).includes(k);
  const withRole = (next: GiveLoginBody, suggested: RoleKey): GiveLoginBody =>
    roleTouched && isBuiltIn(next.roleKey) ? next : { ...next, roleKey: roleTouched ? 'STAFF' : suggested };

  const setMode = (m: WorkspaceMode) => {
    if (m === mode) return;
    const base = { roleKey: value.roleKey, method: value.method, password: value.password };
    if (m === 'platform') {
      onChange(withRole({ ...base, workspace: { mode: 'platform' }, permissionSetIds: [] }, 'STAFF'));
    } else if (m === 'existing') {
      const first = customers[0];
      onChange(
        withRole(
          { ...base, workspace: { mode: 'existing', workspaceId: first?.id ?? '' }, productIds: first?.products.map((p) => p.id), permissionSetIds: [] },
          'STAFF'
        )
      );
    } else {
      const name = defaultWorkspaceName?.trim() ?? '';
      setCodeEdited(false);
      onChange(withRole({ ...base, workspace: { mode: 'new', name, code: slugCode(name), productIds: [] } }, 'SUPER_ADMIN'));
    }
  };

  const setExisting = (workspaceId: string) => {
    const ws = all.find((w) => w.id === workspaceId);
    onChange({
      ...value,
      roleKey: isBuiltIn(value.roleKey) ? value.roleKey : 'STAFF',
      workspace: { mode: 'existing', workspaceId },
      productIds: ws?.products.map((p) => p.id),
      permissionSetIds: []
    });
  };

  const patchNew = (patch: Partial<{ name: string; code: string; productIds: string[] }>) => {
    if (value.workspace.mode !== 'new') return;
    onChange({ ...value, workspace: { ...value.workspace, ...patch } });
  };

  const toggleId = (list: string[] | undefined, id: string, on: boolean) => {
    const cur = list ?? [];
    return on ? Array.from(new Set([...cur, id])) : cur.filter((x) => x !== id);
  };

  const setMethod = (m: GiveLoginBody['method']) => {
    if (m === value.method) return;
    const { password: _pw, ...rest } = value;
    onChange(m === 'password' ? { ...rest, method: 'password', password: '' } : { ...rest, method: 'invite' });
  };

  const modeOptions: Array<{ value: WorkspaceMode; title: string; hint: string; unavailable?: string }> = [
    { value: 'platform', title: t('access.login.wsPlatform'), hint: t('access.login.wsPlatformHint') },
    {
      value: 'existing',
      title: t('access.login.wsExisting'),
      hint: t('access.login.wsExistingHint'),
      unavailable: workspaces.data && customers.length === 0 ? t('access.login.wsNone') : undefined
    },
    { value: 'new', title: t('access.login.wsNew'), hint: t('access.login.wsNewHint') }
  ];

  const wsSets = selectedWs
    ? [...selectedWs.permissionSets, ...extraSets.filter((x) => x.workspaceId === selectedWs.id && !selectedWs.permissionSets.some((s) => s.id === x.id))]
    : [];
  const wsProductIds = value.productIds ?? selectedWs?.products.map((p) => p.id) ?? [];
  const endUserWarning = value.roleKey === 'END_USER' && (mode === 'new' || (value.permissionSetIds ?? []).length === 0);

  return (
    <div className="space-y-5">
      {noEmail ? <Alert tone="warning">{t('access.login.noEmail', { name: personName })}</Alert> : null}
      {errors.email ? <Alert tone="danger">{errors.email}</Alert> : null}
      {workspaces.isError ? <Alert tone="danger">{isApiError(workspaces.error) ? workspaces.error.message : t('access.login.loadError')}</Alert> : null}

      {/* Workspace */}
      <fieldset disabled={disabled}>
        <GroupLabel>{t('access.login.workspace')}</GroupLabel>
        <div className="grid gap-2 sm:grid-cols-3">
          {modeOptions.map((o) => {
            const checked = mode === o.value;
            const off = disabled || Boolean(o.unavailable);
            return (
              <label
                key={o.value}
                className={cn(
                  'flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 transition-colors',
                  checked ? 'border-primary bg-primary-soft/60 ring-1 ring-primary' : 'hover:bg-muted/50',
                  off && 'cursor-not-allowed opacity-60 hover:bg-transparent'
                )}
              >
                <input
                  type="radio"
                  name={`${uid}-ws`}
                  value={o.value}
                  checked={checked}
                  disabled={off}
                  onChange={() => setMode(o.value)}
                  className="mt-0.5 size-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
                />
                <span className="min-w-0 leading-5">
                  <span className="block text-[13px] font-medium text-foreground">{o.title}</span>
                  <span className="block text-xs text-muted-foreground">{o.unavailable ?? o.hint}</span>
                </span>
              </label>
            );
          })}
        </div>
        <FieldError>{errors.workspace}</FieldError>

        {value.workspace.mode === 'existing' ? (
          <Field label={t('access.login.chooseWorkspace')} error={errors['workspace.workspaceId']} className="mt-3">
            <Select
              value={value.workspace.workspaceId}
              onChange={(e) => setExisting(e.target.value)}
              placeholder={t('access.login.chooseWorkspace')}
              options={customers.map((w) => ({ value: w.id, label: `${w.name} · ${w.code}` }))}
            />
          </Field>
        ) : null}

        {value.workspace.mode === 'new' ? (
          <div className="mt-3 space-y-3 rounded-lg border bg-muted/20 p-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label={t('access.login.wsName')} error={errors['workspace.name']}>
                <Input
                  value={value.workspace.name}
                  onChange={(e) => patchNew(codeEdited ? { name: e.target.value } : { name: e.target.value, code: slugCode(e.target.value) })}
                  autoComplete="off"
                />
              </Field>
              <Field label={t('access.login.wsCode')} error={errors['workspace.code']} hint={t('access.login.wsCodeHint')}>
                <Input
                  className="font-mono"
                  value={value.workspace.code}
                  onChange={(e) => {
                    setCodeEdited(true);
                    patchNew({ code: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '').slice(0, 40) });
                  }}
                  autoComplete="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </Field>
            </div>
            <div>
              <p className="mb-2 text-[13px] font-medium text-foreground">{t('access.login.wsProducts')}</p>
              {products.isLoading ? (
                <div className="grid gap-2 sm:grid-cols-2">
                  <Skeleton className="h-8" />
                  <Skeleton className="h-8" />
                </div>
              ) : activeProducts.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('access.login.wsProductsNone')}</p>
              ) : (
                <div className="grid gap-2 sm:grid-cols-2">
                  {activeProducts.map((p) => (
                    <Checkbox
                      key={p.id}
                      label={p.name}
                      description={p.description}
                      checked={value.workspace.mode === 'new' && value.workspace.productIds.includes(p.id)}
                      onCheckedChange={(on) => value.workspace.mode === 'new' && patchNew({ productIds: toggleId(value.workspace.productIds, p.id, on) })}
                    />
                  ))}
                </div>
              )}
              <FieldError>{errors['workspace.productIds']}</FieldError>
            </div>
          </div>
        ) : null}
      </fieldset>

      {/* Role */}
      <RoleField
        workspaceId={mode === 'new' ? undefined : selectedWs?.id}
        label={t('access.login.role')}
        value={value.roleKey}
        error={errors.roleKey}
        disabled={disabled}
        onChange={(roleKey) => {
          setRoleTouched(true);
          onChange({ ...value, roleKey });
        }}
      />

      {/* Products (existing workspace only: platform has every module, new uses the workspace's products) */}
      {value.workspace.mode === 'existing' && selectedWs ? (
        <fieldset disabled={disabled}>
          <GroupLabel>{t('access.login.products')}</GroupLabel>
          {selectedWs.products.length === 0 ? (
            <p className="text-xs text-muted-foreground">{t('access.login.noWsProducts')}</p>
          ) : (
            <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
              {selectedWs.products.map((p) => (
                <Checkbox
                  key={p.id}
                  label={p.name}
                  checked={wsProductIds.includes(p.id)}
                  onCheckedChange={(on) => onChange({ ...value, productIds: toggleId(wsProductIds, p.id, on) })}
                />
              ))}
            </div>
          )}
          {errors.productIds ? <FieldError>{errors.productIds}</FieldError> : <p className="mt-1.5 text-xs text-muted-foreground">{t('access.login.productsHint')}</p>}
        </fieldset>
      ) : null}

      {/* Permission sets */}
      <fieldset disabled={disabled}>
        <GroupLabel
          aside={
            selectedWs && !disabled ? (
              <button
                type="button"
                onClick={() => setNewSetFor(selectedWs)}
                className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Plus className="size-3.5" aria-hidden /> {t('access.sets.new')}
              </button>
            ) : null
          }
        >
          {t('access.login.permissionSets')}
        </GroupLabel>
        {mode === 'new' ? (
          <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">{t('access.login.setsAfterCreate')}</p>
        ) : !workspaces.data ? (
          <Skeleton className="h-10" />
        ) : !selectedWs ? null : wsSets.length === 0 ? (
          <p className="rounded-lg border border-dashed px-3 py-2.5 text-xs text-muted-foreground">{t('access.login.noSets')}</p>
        ) : (
          <div className="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
            {wsSets.map((s) => (
              <Checkbox
                key={s.id}
                label={s.name}
                checked={(value.permissionSetIds ?? []).includes(s.id)}
                onCheckedChange={(on) => onChange({ ...value, permissionSetIds: toggleId(value.permissionSetIds, s.id, on) })}
              />
            ))}
          </div>
        )}
        {errors.permissionSetIds ? (
          <FieldError>{errors.permissionSetIds}</FieldError>
        ) : mode !== 'new' ? (
          <p className="mt-1.5 text-xs text-muted-foreground">{t('access.login.permissionSetsHint')}</p>
        ) : null}
        {endUserWarning ? <p className="mt-1.5 text-xs font-medium text-warning">{t('access.login.endUserWarning')}</p> : null}
      </fieldset>

      {/* Sign-in method */}
      <fieldset disabled={disabled}>
        <GroupLabel>{t('access.login.method')}</GroupLabel>
        <SegmentedFilter<GiveLoginBody['method']>
          value={value.method}
          onChange={(m) => !disabled && setMethod(m)}
          options={[
            { value: 'invite', label: t('access.login.methodInvite') },
            { value: 'password', label: t('access.login.methodPassword') }
          ]}
        />
        {value.method === 'invite' ? (
          <p className="mt-2 text-xs text-muted-foreground">{t('access.login.inviteHint', { email: email || '—' })}</p>
        ) : (
          <div className="mt-3 max-w-sm space-y-2">
            <Field label={t('access.login.password')} error={errors.password} hint={t('access.login.passwordHint')}>
              <PasswordInput value={value.password ?? ''} onChange={(e) => onChange({ ...value, password: e.target.value })} autoComplete="new-password" disabled={disabled} />
            </Field>
            <StrengthMeter password={value.password ?? ''} />
          </div>
        )}
      </fieldset>

      {newSetFor ? (
        <PermissionSetDialog
          open
          onOpenChange={(o) => !o && setNewSetFor(null)}
          workspaceId={newSetFor.id}
          workspaceName={workspaceLabel(newSetFor, t('access.workspace.platformLabel'))}
          onSaved={(set) => {
            setExtraSets((xs) => [...xs, { id: set.id, name: set.name, workspaceId: set.workspaceId }]);
            onChange({ ...value, permissionSetIds: toggleId(value.permissionSetIds, set.id, true) });
          }}
        />
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Embeddable section (convert dialog)
// ---------------------------------------------------------------------------

export interface GiveLoginSectionProps {
  /** null = "don't give a login". */
  value: GiveLoginBody | null;
  onChange: (value: GiveLoginBody | null) => void;
  personName: string;
  email?: string;
  /** Prefill for a new customer workspace (e.g. the account name). */
  defaultWorkspaceName?: string;
  /** Server fieldErrors (keys like "password", "workspace.code"; an "access." prefix is tolerated). */
  errors?: Record<string, string>;
  disabled?: boolean;
}

/** Form section: workspace (platform / existing / new) → role → products → permission sets → invite or temporary password. */
export function GiveLoginSection({ value, onChange, personName, email, defaultWorkspaceName, errors, disabled }: GiveLoginSectionProps) {
  const { t } = useTranslation();
  const id = useId();
  const noEmail = !email?.trim();
  const name = firstName(personName);
  return (
    <div className={cn('rounded-lg border', value && 'border-primary/40')}>
      <Switch
        id={id}
        className="px-4 py-3"
        label={t('access.login.toggle', { name })}
        description={noEmail ? t('access.login.noEmail', { name: personName }) : t('access.login.toggleHint')}
        checked={value !== null}
        disabled={disabled || (noEmail && value === null)}
        onCheckedChange={(on) => onChange(on ? defaultGiveLoginBody(defaultWorkspaceName) : null)}
      />
      {value ? (
        <div className="border-t px-4 py-4">
          <GiveLoginFields
            value={value}
            onChange={onChange}
            personName={personName}
            email={email}
            defaultWorkspaceName={defaultWorkspaceName}
            errors={errors}
            disabled={disabled}
          />
        </div>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Result summary (shared with "New user")
// ---------------------------------------------------------------------------

/** After a login was created: who, where, which role, and how they get in. */
export function LoginCreatedSummary({
  personName,
  email,
  body,
  result,
  actions
}: {
  personName: string;
  email?: string;
  body: GiveLoginBody;
  result: GiveLoginResult;
  actions: ReactNode;
}) {
  const { t } = useTranslation();
  const roleName = useRoleName(result.workspaceId);
  const workspaces = useAccessWorkspaces();
  const ws = body.workspace;
  const wsName =
    ws.mode === 'platform'
      ? t('access.workspace.platformLabel')
      : ws.mode === 'new'
        ? ws.name
        : (workspaces.data?.find((w) => w.id === ws.workspaceId)?.name ?? '—');

  const title = result.existingLogin
    ? t('access.login.doneExistingTitle', { name: personName })
    : result.invitation
      ? t('access.login.doneInviteTitle', { email: email ?? personName })
      : t('access.login.donePasswordTitle', { name: personName });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex flex-col items-center px-6 pb-4 pt-8 text-center">
          <span className="grid size-11 place-items-center rounded-full bg-success-soft text-success">
            <CircleCheck className="size-5" aria-hidden />
          </span>
          <DialogTitle className="mt-3 text-base font-semibold">{title}</DialogTitle>
          <DialogDescription className="mt-1 max-w-md text-[13px] text-muted-foreground">
            {result.existingLogin
              ? t('access.login.doneExisting')
              : result.invitation
                ? t('access.login.doneInviteBody')
                : t('access.login.donePasswordBody', { email: email ?? '' })}
          </DialogDescription>
        </div>
        <dl className="mx-5 mb-4 grid grid-cols-2 gap-3 rounded-lg border bg-muted/30 px-4 py-3">
          <DetailItem label={t('access.login.summaryWorkspace')}>{wsName}</DetailItem>
          <DetailItem label={t('access.login.summaryRole')}>{roleName(body.roleKey)}</DetailItem>
        </dl>
        {result.invitation?.devAcceptUrl ? (
          <div className="px-5 pb-5">
            <DevLink url={result.invitation.devAcceptUrl} label={t('access.login.devInviteLink')} />
          </div>
        ) : null}
      </div>
      <div className="flex flex-wrap justify-end gap-2 border-t px-5 py-3">{actions}</div>
    </div>
  );
}

/** Everything that shows users, records, workspaces or dashboard counts may have changed. */
export function useInvalidateAfterLogin() {
  const qc = useQueryClient();
  return () => {
    for (const key of [['users'], ['platform', 'users'], ['platform', 'user'], ['records'], ['platform', 'dashboard'], ['access'], ['workspaces'], ['workspace']]) {
      void qc.invalidateQueries({ queryKey: key });
    }
  };
}

// ---------------------------------------------------------------------------
// Dialog (record pages)
// ---------------------------------------------------------------------------

export interface GiveLoginDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  object: ObjectKey;
  recordId: string;
  personName: string;
  email?: string;
  defaultWorkspaceName?: string;
  onDone?: (result: GiveLoginResult) => void;
}

/** Dialog wrapper around GiveLoginSection that calls accessApi.giveLogin and shows the result (invite link in local dev). */
export function GiveLoginDialog(props: GiveLoginDialogProps) {
  const { open, onOpenChange } = props;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[4vh] flex max-h-[92vh] max-w-2xl flex-col p-0">{open ? <GiveLoginForm {...props} /> : null}</DialogContent>
    </Dialog>
  );
}

function GiveLoginForm({ onOpenChange, object, recordId, personName, email, defaultWorkspaceName, onDone }: GiveLoginDialogProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const invalidate = useInvalidateAfterLogin();
  const [body, setBody] = useState<GiveLoginBody>(() => defaultGiveLoginBody(defaultWorkspaceName));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [done, setDone] = useState<{ result: GiveLoginResult; body: GiveLoginBody } | null>(null);

  const give = useMutation({
    mutationFn: (b: GiveLoginBody) => accessApi.giveLogin(object, recordId, b),
    onSuccess: (result, b) => {
      invalidate();
      setDone({ result, body: b });
      onDone?.(result);
    },
    onError: (e) => {
      if (isApiError(e)) {
        const fe = normalizeErrors(e.fieldErrors);
        setErrors(fe);
        const shown = Object.keys(fe).some(isGiveLoginField) || Boolean(fe.email);
        setFormError(e.code === 'no_email' || !shown ? e.message : t('access.login.fixErrors'));
        return;
      }
      setFormError(t('common.genericError'));
    }
  });

  if (done) {
    return (
      <LoginCreatedSummary
        personName={personName}
        email={email}
        body={done.body}
        result={done.result}
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => {
                onOpenChange(false);
                navigate(`/crm/owner/users/${done.result.identityId}`);
              }}
            >
              {t('access.login.openUser')}
            </Button>
            <Button onClick={() => onOpenChange(false)}>{t('access.login.done')}</Button>
          </>
        }
      />
    );
  }

  const onSubmit = (ev: FormEvent) => {
    ev.preventDefault();
    const e = validateGiveLogin(body, t);
    setErrors(e);
    setFormError(null);
    if (Object.keys(e).length) return;
    give.mutate(body);
  };

  const noEmail = !email?.trim();

  return (
    <form onSubmit={onSubmit} noValidate className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-5 py-4">
        <DialogTitle className="pr-8 text-base font-semibold">{t('access.login.dialogTitle', { name: personName })}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
          {email ? `${email} · ` : ''}
          {t('access.login.dialogDescription')}
        </DialogDescription>
      </div>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {formError ? <Alert tone="danger">{formError}</Alert> : null}
        <GiveLoginFields
          value={body}
          onChange={setBody}
          personName={personName}
          email={email}
          defaultWorkspaceName={defaultWorkspaceName}
          errors={errors}
          disabled={give.isPending}
        />
      </div>
      <div className="flex justify-end gap-2 border-t px-5 py-3">
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={give.isPending}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" loading={give.isPending} disabled={noEmail}>
          {body.method === 'password' ? t('access.login.submitPassword') : t('access.login.submitInvite')}
        </Button>
      </div>
    </form>
  );
}
