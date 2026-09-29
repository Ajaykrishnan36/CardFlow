import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Code2, Eye, EyeOff, Lock, Minus, Pencil, Share2 } from 'lucide-react';
import { accessApi, recordsApiFor, workspaceAdminApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { AccessRules, FieldCatalogObject, FieldLevel, RecordRow, WorkspaceRole } from '@crm/api/types';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Tooltip } from '@crm/components/ui/menu';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn } from '@crm/lib/utils';
import { accessKeys } from './use-access';

type Source = { kind: 'owner'; workspaceId: string } | { kind: 'member' };

const LEVELS: FieldLevel[] = ['edit', 'read', 'hidden'];
const levelIcon = { edit: Pencil, read: Eye, hidden: EyeOff };

/**
 * API & sharing (D-47): every API of the workspace, a live response with all its keys and
 * values, and which roles see or edit each key. The owner and Super Admin always see
 * everything; the other roles are set here (owner, Super Admin, or anyone with
 * "Manage roles & permission sets" — never beyond their own access).
 */
export function ApiSharing({ code, source }: { code: string; source: Source }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const owner = source.kind === 'owner';
  const workspaceId = owner ? source.workspaceId : '';

  const fields = useQuery({
    queryKey: ['access', 'fields', owner ? workspaceId : code],
    queryFn: () => (owner ? accessApi.fieldCatalog(workspaceId) : workspaceAdminApi(code).fieldCatalog()),
    staleTime: 60_000
  });
  const ownerRoles = useQuery({ queryKey: accessKeys.roles(workspaceId), queryFn: () => accessApi.roles(workspaceId), enabled: owner });
  const options = useQuery({ queryKey: ['workspace', code, 'admin', 'options'], queryFn: () => workspaceAdminApi(code).options(), enabled: !owner });
  const roles = useMemo(
    () => [...((owner ? ownerRoles.data : options.data?.roles) ?? [])].sort((a, b) => b.rank - a.rank),
    [owner, ownerRoles.data, options.data?.roles]
  );
  const limit = owner ? undefined : options.data?.grantable;
  const canEdit = owner || Boolean(options.data?.canManageAccess);

  const [picked, setPicked] = useState<string | null>(null);
  const objects = fields.data ?? [];
  const current = objects.find((o) => o.key === picked) ?? objects[0];
  const [drafts, setDrafts] = useState<Record<string, AccessRules>>({});
  const rulesOf = (r: WorkspaceRole) => drafts[r.id] ?? r.rules;
  const dirtyRoles = roles.filter((r) => drafts[r.id] && JSON.stringify(drafts[r.id]) !== JSON.stringify(r.rules));

  const setLevel = (role: WorkspaceRole, obj: string, field: string, level: FieldLevel) => {
    const base = rulesOf(role);
    const next = { ...(base.fields ?? {}) };
    const map = { ...(next[obj] ?? {}) };
    if (level === 'edit') delete map[field];
    else map[field] = level;
    if (Object.keys(map).length) next[obj] = map;
    else delete next[obj];
    setDrafts((d) => ({ ...d, [role.id]: { ...base, fields: next } }));
  };

  const save = useMutation({
    mutationFn: async () => {
      for (const r of dirtyRoles) {
        const body = { rules: drafts[r.id]! };
        if (owner) await accessApi.updateRole(r.id, body);
        else await workspaceAdminApi(code).updateRole(r.id, body);
      }
    },
    onSuccess: () => {
      toast.success(t('access.sharing.saved', { count: dirtyRoles.length }));
      setDrafts({});
      void qc.invalidateQueries({ queryKey: accessKeys.all });
      void qc.invalidateQueries({ queryKey: ['workspace', code] });
      void qc.invalidateQueries({ queryKey: ['records'] });
    },
    onError: (e) => {
      toast.error(isApiError(e) ? (Object.values(e.fieldErrors)[0] ?? e.message) : t('common.genericError'));
      void qc.invalidateQueries({ queryKey: accessKeys.all });
    }
  });

  const loading = fields.isPending || (owner ? ownerRoles.isPending : options.isPending);
  const error = fields.error ?? (owner ? ownerRoles.error : options.error);
  if (error) {
    return <ErrorState title={t('access.sharing.loadError')} message={isApiError(error) ? error.message : undefined} onRetry={() => void fields.refetch()} />;
  }
  if (loading) return <Skeleton className="h-96 w-full" />;
  if (!current) {
    return (
      <Card>
        <EmptyState icon={Code2} title={t('access.sharing.emptyTitle')} body={t('access.sharing.emptyBody')} />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      <Alert tone="info">{t('access.sharing.intro')}</Alert>
      <div className="grid gap-4 xl:grid-cols-[15rem_minmax(0,1fr)]">
        <Card className="h-fit overflow-hidden">
          <CardHeader title={t('access.sharing.apis', { count: objects.length })} />
          <ul className="border-t py-1">
            {objects.map((o) => {
              const restricted = roles.reduce((n, r) => n + Object.keys(rulesOf(r).fields?.[o.key] ?? {}).length, 0);
              return (
                <li key={o.key}>
                  <button
                    type="button"
                    onClick={() => setPicked(o.key)}
                    aria-current={o.key === current.key || undefined}
                    className={cn(
                      'flex w-full flex-col px-4 py-2 text-left text-[13px] transition-colors',
                      o.key === current.key ? 'bg-primary-soft text-primary' : 'hover:bg-muted'
                    )}
                  >
                    <span className="flex items-center justify-between gap-2 font-medium">
                      {o.label}
                      {restricted ? <Badge tone="warning">{restricted}</Badge> : null}
                    </span>
                    <span className="truncate font-mono text-[11px] text-muted-foreground">/crm/{o.object}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </Card>

        <div className="min-w-0 space-y-4">
          <ResponsePreview code={code} object={current} roles={roles} rulesOf={rulesOf} />
          <Card className="overflow-hidden">
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <Share2 className="size-4 text-muted-foreground" aria-hidden /> {t('access.sharing.matrixTitle', { name: current.label })}
                </span>
              }
              description={t('access.sharing.matrixBody')}
              actions={
                canEdit ? (
                  <div className="flex gap-2">
                    {dirtyRoles.length ? (
                      <Button size="sm" variant="outline" onClick={() => setDrafts({})} disabled={save.isPending}>
                        {t('access.sharing.discard')}
                      </Button>
                    ) : null}
                    <Button size="sm" disabled={!dirtyRoles.length} loading={save.isPending} onClick={() => save.mutate()}>
                      {t('access.sharing.save')}
                    </Button>
                  </div>
                ) : null
              }
            />
            <div className="overflow-x-auto border-t">
              <table className="w-full text-left text-xs">
                <thead className="bg-muted/40 text-muted-foreground">
                  <tr>
                    <th className="sticky left-0 z-10 min-w-[12rem] bg-muted px-4 py-2 font-medium">{t('access.sharing.key')}</th>
                    <th className="px-3 py-2 text-center font-medium">{t('access.sharing.ownerColumn')}</th>
                    {roles.map((r) => (
                      <th key={r.id} className="px-3 py-2 text-center font-medium">
                        {r.name}
                        {drafts[r.id] && JSON.stringify(drafts[r.id]) !== JSON.stringify(r.rules) ? <span className="text-warning"> •</span> : null}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {current.fields.map((f) => (
                    <tr key={f.key} className="hover:bg-muted/30">
                      <td className="sticky left-0 z-10 bg-background px-4 py-2">
                        <p className="font-medium text-foreground">{f.label}</p>
                        <p className="font-mono text-[11px] text-muted-foreground">{f.key}</p>
                      </td>
                      <td className="px-3 py-2 text-center">
                        <Always />
                      </td>
                      {roles.map((r) => (
                        <td key={r.id} className="px-3 py-2 text-center">
                          <Cell
                            role={r}
                            object={current}
                            field={f}
                            rules={rulesOf(r)}
                            limit={limit}
                            disabled={!canEdit || save.isPending}
                            onChange={(l) => setLevel(r, current.key, f.key, l)}
                          />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="border-t px-4 py-3 text-xs text-muted-foreground">{t('access.sharing.footer')}</p>
          </Card>
        </div>
      </div>
    </div>
  );
}

function Always() {
  const { t } = useTranslation();
  return (
    <Tooltip content={t('access.sharing.alwaysHint')} side="top">
      <span tabIndex={0} className="inline-flex items-center gap-1 rounded-md bg-success-soft px-2 py-1 text-[11px] font-medium text-success">
        <Lock className="size-3" aria-hidden /> {t('access.sharing.always')}
      </span>
    </Tooltip>
  );
}

function Cell({
  role,
  object,
  field,
  rules,
  limit,
  disabled,
  onChange
}: {
  role: WorkspaceRole;
  object: FieldCatalogObject;
  field: FieldCatalogObject['fields'][number];
  rules: AccessRules;
  limit?: AccessRules;
  disabled: boolean;
  onChange: (l: FieldLevel) => void;
}) {
  const { t } = useTranslation();
  if (role.key === 'SUPER_ADMIN' || field.locked) return <Always />;
  if (!(rules.objects[object.key] ?? []).length) {
    return (
      <Tooltip content={t('access.sharing.noObjectAccess', { role: role.name, object: object.label })} side="top">
        <span tabIndex={0} className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
          <Minus className="size-3" aria-hidden /> {t('access.sharing.noAccess')}
        </span>
      </Tooltip>
    );
  }
  const level: FieldLevel = rules.fields?.[object.key]?.[field.key] ?? 'edit';
  const rank = { hidden: 0, read: 1, edit: 2 };
  const cap: FieldLevel = limit ? (limit.objects[object.key]?.length ? (limit.fields?.[object.key]?.[field.key] ?? 'edit') : 'hidden') : 'edit';
  return (
    <div role="radiogroup" aria-label={`${field.label} · ${role.name}`} className="inline-flex rounded-md border bg-muted/60 p-0.5">
      {LEVELS.map((l) => {
        const Icon = levelIcon[l];
        const blocked = rank[l] > rank[cap] && rank[l] > rank[level];
        return (
          <Tooltip key={l} content={blocked ? t('access.fields.notYours') : t(`access.fields.level.${l}`)} side="top">
            <button
              type="button"
              role="radio"
              aria-checked={level === l}
              aria-label={t(`access.fields.level.${l}`)}
              disabled={disabled || blocked}
              onClick={() => onChange(l)}
              className={cn(
                'grid size-6 place-items-center rounded transition-colors disabled:cursor-not-allowed disabled:opacity-40',
                level === l
                  ? l === 'hidden'
                    ? 'bg-danger-soft text-danger shadow-sm'
                    : l === 'read'
                      ? 'bg-warning-soft text-warning shadow-sm'
                      : 'bg-background text-success shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              <Icon className="size-3.5" aria-hidden />
            </button>
          </Tooltip>
        );
      })}
    </div>
  );
}

/** A real record from this API, with every key and value; "View as" shows exactly what a role's API calls return. */
function ResponsePreview({
  code,
  object,
  roles,
  rulesOf
}: {
  code: string;
  object: FieldCatalogObject;
  roles: WorkspaceRole[];
  rulesOf: (r: WorkspaceRole) => AccessRules;
}) {
  const { t } = useTranslation();
  const [as, setAs] = useState('owner');
  const api = recordsApiFor(`/w/${encodeURIComponent(code)}`);
  const sample = useQuery({
    queryKey: ['records', `/w/${code}`, object.object, 'sample'],
    queryFn: () => api.list(object.object, { limit: 1 }),
    staleTime: 30_000
  });
  const record: RecordRow | undefined = sample.data?.data[0];
  const role = roles.find((r) => r.id === as);
  const view = useMemo(() => {
    const base: Record<string, unknown> = record
      ? Object.fromEntries(object.fields.map((f) => [f.key, record.values[f.key] ?? null]))
      : Object.fromEntries(object.fields.map((f) => [f.key, `<${f.type}>`]));
    if (!role || role.key === 'SUPER_ADMIN') return { values: base, hidden: [] as string[], readOnly: [] as string[], noAccess: false };
    const rules = rulesOf(role);
    if (!(rules.objects[object.key] ?? []).length) return { values: {}, hidden: [], readOnly: [], noAccess: true };
    const map = rules.fields?.[object.key] ?? {};
    const locked = new Set(object.fields.filter((f) => f.locked).map((f) => f.key));
    const hidden = Object.keys(map).filter((k) => map[k] === 'hidden' && !locked.has(k));
    const readOnly = Object.keys(map).filter((k) => map[k] === 'read' && !locked.has(k));
    return { values: Object.fromEntries(Object.entries(base).filter(([k]) => !hidden.includes(k))), hidden, readOnly, noAccess: false };
  }, [record, object, role, rulesOf]);

  const body = record
    ? { id: record.id, code: record.code, title: record.title, version: record.version, values: view.values }
    : { values: view.values };

  return (
    <Card className="overflow-hidden">
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <Code2 className="size-4 text-muted-foreground" aria-hidden /> {t('access.sharing.responseTitle', { name: object.label })}
          </span>
        }
        description={
          <span className="font-mono text-[11px]">
            GET /api/crm/v1/w/{code}/crm/{object.object}/{'{id}'}
          </span>
        }
        actions={
          <Select
            value={as}
            onChange={(e) => setAs(e.target.value)}
            aria-label={t('access.sharing.viewAs')}
            className="w-52"
            options={[{ value: 'owner', label: t('access.sharing.viewAsOwner') }, ...roles.map((r) => ({ value: r.id, label: t('access.sharing.viewAsRole', { role: r.name }) }))]}
          />
        }
      />
      <div className="border-t p-4">
        {sample.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : view.noAccess ? (
          <p className="rounded-md bg-muted px-3 py-6 text-center text-[13px] text-muted-foreground">
            {t('access.sharing.previewNoAccess', { role: role?.name, object: object.label })}
          </p>
        ) : (
          <>
            {!record ? <p className="mb-2 text-xs text-muted-foreground">{t('access.sharing.noRecords')}</p> : null}
            <pre className="max-h-80 overflow-auto rounded-md bg-muted px-3 py-2 font-mono text-[11px] leading-5">{JSON.stringify(body, null, 2)}</pre>
            {view.hidden.length || view.readOnly.length ? (
              <div className="mt-2 flex flex-wrap gap-1.5 text-[11px]">
                {view.hidden.map((k) => (
                  <Badge key={k} tone="danger" className="font-mono">
                    <EyeOff className="mr-1 size-3" aria-hidden />
                    {k}
                  </Badge>
                ))}
                {view.readOnly.map((k) => (
                  <Badge key={k} tone="warning" className="font-mono">
                    <Eye className="mr-1 size-3" aria-hidden />
                    {k}
                  </Badge>
                ))}
              </div>
            ) : null}
          </>
        )}
      </div>
    </Card>
  );
}
