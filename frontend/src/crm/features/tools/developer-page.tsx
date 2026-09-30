import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Download, KeyRound, Pause, Play, Plus, RefreshCw, Send, Trash2, Webhook as WebhookIcon } from 'lucide-react';
import type { ApiKey, Webhook } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Checkbox, Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { relativeTime } from '@crm/lib/utils';
import { CopyField, errorText, formatDateTime, hasCap, JsonBlock, SecretOnce, StatusBadge, toolKeys, useTools, useWorkspaceObjects } from './tool-utils';

type Tab = 'keys' | 'webhooks' | 'reference';

/** /crm/w/:ws/settings/developer — API keys, webhooks and the API reference (developer.manage). */
export function DeveloperPage() {
  const { t } = useTranslation();
  const { context } = useWorkspace();
  useDocumentTitle(t('tools.dev.title'));
  const [sp, setSp] = useSearchParams();
  if (!hasCap('developer.manage')(context)) return <NoAccessPage />;
  const tab = (['keys', 'webhooks', 'reference'].includes(sp.get('tab') ?? '') ? sp.get('tab') : 'keys') as Tab;
  return (
    <PageContainer>
      <PageHeader title={t('tools.dev.title')} description={t('tools.dev.subtitle')} />
      <Tabs
        className="mb-5"
        value={tab}
        onChange={(v) => setSp({ tab: v }, { replace: true })}
        items={[
          { value: 'keys', label: t('tools.dev.tabs.keys') },
          { value: 'webhooks', label: t('tools.dev.tabs.webhooks') },
          { value: 'reference', label: t('tools.dev.tabs.reference') }
        ]}
      />
      {tab === 'keys' ? <ApiKeysTab /> : tab === 'webhooks' ? <WebhooksTab /> : <ReferenceTab />}
    </PageContainer>
  );
}

function SetupOff({ what }: { what: 'api' | 'webhooks' }) {
  const { t } = useTranslation();
  return (
    <Alert tone="warning" title={t(`tools.dev.off.${what}Title`)} className="mb-4">
      {t('tools.dev.off.body')}
    </Alert>
  );
}

// ---- API keys ----

function ApiKeysTab() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'api-keys');
  const q = useQuery({ queryKey: key, queryFn: () => api.apiKeys() });
  const [open, setOpen] = useState(false);
  const [revoke, setRevoke] = useState<ApiKey | null>(null);
  const revokeM = useMutation({
    mutationFn: (id: string) => api.revokeApiKey(id),
    onSuccess: () => {
      toast.success(t('tools.dev.keys.revoked'));
      setRevoke(null);
      void qc.invalidateQueries({ queryKey: key });
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  if (q.isPending) return <Skeleton className="h-48 w-full" />;
  if (q.isError) return <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />;
  const d = q.data;
  return (
    <>
      {!d.apiAccess ? <SetupOff what="api" /> : null}
      <Card>
        <CardHeader
          title={t('tools.dev.keys.title')}
          description={t('tools.dev.keys.body')}
          actions={
            <Button size="sm" onClick={() => setOpen(true)} disabled={!d.apiAccess}>
              <Plus /> {t('tools.dev.keys.new')}
            </Button>
          }
        />
        {d.data.length === 0 ? (
          <EmptyState icon={KeyRound} title={t('tools.dev.keys.emptyTitle')} body={t('tools.dev.keys.emptyBody')} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[13px]">
              <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">{t('tools.dev.keys.name')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.dev.keys.key')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.dev.keys.access')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.dev.keys.lastUsed')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.dev.keys.expires')}</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody className="divide-y">
                {d.data.map((k) => (
                  <tr key={k.id} className={k.revokedAt ? 'opacity-60' : undefined}>
                    <td className="px-4 py-2.5">
                      <p className="font-medium text-foreground">{k.name}</p>
                      <p className="text-xs text-muted-foreground">{t('tools.dev.keys.createdBy', { name: k.createdBy || '—', when: relativeTime(k.createdAt) })}</p>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs">{k.prefix}_…</td>
                    <td className="px-4 py-2.5">{k.access === 'full' ? t('tools.dev.keys.full') : k.permissionSet || t('tools.dev.keys.set')}</td>
                    <td className="px-4 py-2.5 text-muted-foreground">{k.lastUsedAt ? relativeTime(k.lastUsedAt) : t('tools.dev.keys.never')}</td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {k.revokedAt ? <Badge tone="danger">{t('tools.dev.keys.revokedBadge')}</Badge> : k.expiresAt ? formatDateTime(k.expiresAt) : t('tools.dev.keys.noExpiry')}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      {!k.revokedAt ? (
                        <Button size="sm" variant="danger-outline" onClick={() => setRevoke(k)}>
                          {t('tools.dev.keys.revoke')}
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <NewKeyDialog open={open} onOpenChange={setOpen} sets={d.permissionSets ?? []} onCreated={() => void qc.invalidateQueries({ queryKey: key })} />
      <ConfirmDialog
        open={Boolean(revoke)}
        onOpenChange={(o) => !o && setRevoke(null)}
        title={t('tools.dev.keys.revokeTitle', { name: revoke?.name })}
        body={t('tools.dev.keys.revokeBody')}
        confirmLabel={t('tools.dev.keys.revoke')}
        tone="danger"
        loading={revokeM.isPending}
        onConfirm={() => revoke && revokeM.mutate(revoke.id)}
      />
    </>
  );
}

function NewKeyDialog({ open, onOpenChange, sets, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; sets: Array<{ id: string; name: string }>; onCreated: () => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const [name, setName] = useState('');
  const [access, setAccess] = useState<'full' | 'permission_set'>('full');
  const [setId, setSetId] = useState('');
  const [days, setDays] = useState('365');
  const [created, setCreated] = useState<ApiKey | null>(null);
  const m = useMutation({
    mutationFn: () =>
      api.createApiKey({ name: name.trim(), access, permissionSetId: access === 'permission_set' ? setId : undefined, expiresInDays: days ? Number(days) : undefined }),
    onSuccess: (k) => {
      setCreated(k);
      onCreated();
    }
  });
  const close = (o: boolean) => {
    if (!o) {
      setName('');
      setAccess('full');
      setSetId('');
      setDays('365');
      setCreated(null);
      m.reset();
    }
    onOpenChange(o);
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim() || (access === 'permission_set' && !setId)) return;
    m.mutate();
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('tools.dev.keys.new')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.dev.keys.newBody')}</DialogDescription>
        {created?.secret ? (
          <div className="mt-4 space-y-4">
            <SecretOnce title={t('tools.dev.keys.secretTitle')} body={t('tools.dev.keys.secretBody')} secret={created.secret} />
            <div className="flex justify-end">
              <Button onClick={() => close(false)}>{t('tools.common.done')}</Button>
            </div>
          </div>
        ) : (
          <form onSubmit={submit} noValidate className="mt-4 space-y-4">
            {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
            <Field label={t('tools.dev.keys.name')} hint={t('tools.dev.keys.nameHint')}>
              <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus placeholder={t('tools.dev.keys.namePlaceholder')} />
            </Field>
            <Field label={t('tools.dev.keys.access')}>
              <Select
                value={access}
                onChange={(e) => setAccess(e.target.value as 'full' | 'permission_set')}
                options={[
                  { value: 'full', label: t('tools.dev.keys.fullOption') },
                  { value: 'permission_set', label: t('tools.dev.keys.setOption') }
                ]}
              />
            </Field>
            {access === 'permission_set' ? (
              <Field label={t('tools.dev.keys.permissionSet')} hint={t('tools.dev.keys.permissionSetHint')}>
                <Select value={setId} onChange={(e) => setSetId(e.target.value)} placeholder={t('tools.dev.keys.pickSet')} options={sets.map((s) => ({ value: s.id, label: s.name }))} />
              </Field>
            ) : null}
            <Field label={t('tools.dev.keys.expires')}>
              <Select
                value={days}
                onChange={(e) => setDays(e.target.value)}
                options={[
                  { value: '30', label: t('tools.dev.keys.days', { count: 30 }) },
                  { value: '90', label: t('tools.dev.keys.days', { count: 90 }) },
                  { value: '365', label: t('tools.dev.keys.oneYear') },
                  { value: '', label: t('tools.dev.keys.noExpiry') }
                ]}
              />
            </Field>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => close(false)}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" loading={m.isPending} disabled={!name.trim() || (access === 'permission_set' && !setId)}>
                {t('tools.dev.keys.create')}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ---- Webhooks ----

function WebhooksTab() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'webhooks');
  const q = useQuery({ queryKey: key, queryFn: () => api.webhooks() });
  const [editing, setEditing] = useState<Webhook | 'new' | null>(null);
  const [secret, setSecret] = useState<{ url: string; secret: string } | null>(null);
  const [deliveriesFor, setDeliveriesFor] = useState<Webhook | null>(null);
  const [deleting, setDeleting] = useState<Webhook | null>(null);
  const refresh = () => void qc.invalidateQueries({ queryKey: key });
  const update = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'active' | 'paused' }) => api.updateWebhook(id, { status }),
    onSuccess: refresh,
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const test = useMutation({
    mutationFn: (id: string) => api.testWebhook(id),
    onSuccess: (r) => (r.ok ? toast.success(t('tools.dev.hooks.testOk', { status: r.status })) : toast.error(t('tools.dev.hooks.testFailed', { status: r.status || '—' }))),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const rotate = useMutation({
    mutationFn: (w: Webhook) => api.rotateWebhook(w.id),
    onSuccess: (w) => w.secret && setSecret({ url: w.url, secret: w.secret }),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => {
      setDeleting(null);
      toast.success(t('tools.dev.hooks.deleted'));
      refresh();
    }
  });
  if (q.isPending) return <Skeleton className="h-48 w-full" />;
  if (q.isError) return <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />;
  const d = q.data;
  return (
    <>
      {!d.enabled ? <SetupOff what="webhooks" /> : null}
      {secret ? <SecretOnce title={t('tools.dev.hooks.secretTitle', { url: secret.url })} body={t('tools.dev.hooks.secretBody')} secret={secret.secret} /> : null}
      <Card className="mt-4">
        <CardHeader
          title={t('tools.dev.hooks.title')}
          description={t('tools.dev.hooks.body')}
          actions={
            <Button size="sm" onClick={() => setEditing('new')} disabled={!d.enabled}>
              <Plus /> {t('tools.dev.hooks.new')}
            </Button>
          }
        />
        {d.data.length === 0 ? (
          <EmptyState icon={WebhookIcon} title={t('tools.dev.hooks.emptyTitle')} body={t('tools.dev.hooks.emptyBody')} />
        ) : (
          <ul className="divide-y">
            {d.data.map((w) => (
              <li key={w.id} className="flex flex-col gap-3 px-5 py-3.5 lg:flex-row lg:items-center">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate font-mono text-[13px] text-foreground">{w.url}</p>
                    <StatusBadge status={w.status} />
                    {w.failing ? <Badge tone="danger">{t('tools.dev.hooks.failing', { count: w.failing })}</Badge> : null}
                  </div>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {w.description ? `${w.description} · ` : ''}
                    {w.events.map((e) => t(`tools.events.${e}`, { defaultValue: e })).join(', ')}
                    {w.objects.length ? ` · ${w.objects.join(', ')}` : ` · ${t('tools.dev.hooks.allObjects')}`}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {w.lastDeliveryAt ? t('tools.dev.hooks.lastDelivery', { when: relativeTime(w.lastDeliveryAt), status: w.lastStatus ?? '—' }) : t('tools.dev.hooks.noDeliveries')}
                  </p>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => test.mutate(w.id)} loading={test.isPending && test.variables === w.id}>
                    <Send /> {t('tools.dev.hooks.test')}
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setDeliveriesFor(w)}>
                    {t('tools.dev.hooks.deliveries')}
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => setEditing(w)}>
                    {t('tools.common.edit')}
                  </Button>
                  <Button size="sm" variant="subtle" onClick={() => update.mutate({ id: w.id, status: w.status === 'active' ? 'paused' : 'active' })} aria-label={w.status === 'active' ? t('tools.dev.hooks.pause') : t('tools.dev.hooks.resume')}>
                    {w.status === 'active' ? <Pause /> : <Play />}
                  </Button>
                  <Button size="sm" variant="subtle" onClick={() => rotate.mutate(w)} aria-label={t('tools.dev.hooks.rotate')} title={t('tools.dev.hooks.rotate')}>
                    <RefreshCw />
                  </Button>
                  <Button size="sm" variant="subtle" onClick={() => setDeleting(w)} aria-label={t('tools.common.delete')}>
                    <Trash2 />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <WebhookDialog
        hook={editing}
        events={d.events}
        onClose={() => setEditing(null)}
        onSaved={(w) => {
          if (w.secret) setSecret({ url: w.url, secret: w.secret });
          refresh();
        }}
      />
      <DeliveriesDialog hook={deliveriesFor} onClose={() => setDeliveriesFor(null)} />
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('tools.dev.hooks.deleteTitle')}
        body={t('tools.dev.hooks.deleteBody', { url: deleting?.url })}
        confirmLabel={t('tools.common.delete')}
        tone="danger"
        loading={del.isPending}
        onConfirm={() => deleting && del.mutate(deleting.id)}
      />
    </>
  );
}

function WebhookDialog({ hook, events, onClose, onSaved }: { hook: Webhook | 'new' | null; events: string[]; onClose: () => void; onSaved: (w: Webhook) => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const objects = useWorkspaceObjects();
  const editing = hook && hook !== 'new' ? hook : null;
  const [url, setUrl] = useState('');
  const [description, setDescription] = useState('');
  const [picked, setPicked] = useState<string[]>([]);
  const [objs, setObjs] = useState<string[]>([]);
  const [lastHook, setLastHook] = useState<typeof hook>(null);
  if (hook !== lastHook) {
    setLastHook(hook);
    setUrl(editing?.url ?? '');
    setDescription(editing?.description ?? '');
    setPicked(editing?.events ?? events.slice(0, 3));
    setObjs(editing?.objects ?? []);
  }
  const m = useMutation({
    mutationFn: () => {
      const body = { url: url.trim(), description: description.trim(), events: picked, objects: objs };
      return editing ? api.updateWebhook(editing.id, body) : api.createWebhook(body);
    },
    onSuccess: (w) => {
      toast.success(editing ? t('tools.dev.hooks.saved') : t('tools.dev.hooks.created'));
      onSaved(w);
      onClose();
    }
  });
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  return (
    <Dialog open={hook !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{editing ? t('tools.dev.hooks.editTitle') : t('tools.dev.hooks.new')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.dev.hooks.dialogBody')}</DialogDescription>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (url.trim() && picked.length) m.mutate();
          }}
          noValidate
          className="mt-4 space-y-4"
        >
          {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
          <Field label={t('tools.dev.hooks.url')} hint={t('tools.dev.hooks.urlHint')}>
            <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/crm-events" autoFocus inputMode="url" />
          </Field>
          <Field label={t('tools.dev.hooks.description')}>
            <Input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={200} />
          </Field>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">{t('tools.dev.hooks.events')}</legend>
            <div className="grid gap-1.5 sm:grid-cols-2">
              {events.map((ev) => (
                <Checkbox key={ev} checked={picked.includes(ev)} onCheckedChange={() => setPicked((p) => toggle(p, ev))} label={t(`tools.events.${ev}`, { defaultValue: ev })} />
              ))}
            </div>
          </fieldset>
          <fieldset>
            <legend className="mb-1.5 text-[13px] font-medium">{t('tools.dev.hooks.objects')}</legend>
            <p className="mb-1.5 text-xs text-muted-foreground">{t('tools.dev.hooks.objectsHint')}</p>
            <div className="grid gap-1.5 sm:grid-cols-2">
              {objects.map((o) => (
                <Checkbox key={o.key} checked={objs.includes(o.key)} onCheckedChange={() => setObjs((p) => toggle(p, o.key))} label={o.label} />
              ))}
            </div>
          </fieldset>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={m.isPending} disabled={!url.trim() || !picked.length}>
              {editing ? t('tools.common.save') : t('tools.dev.hooks.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DeliveriesDialog({ hook, onClose }: { hook: Webhook | null; onClose: () => void }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'deliveries', hook?.id ?? '');
  const q = useQuery({ queryKey: key, queryFn: () => api.deliveries(hook!.id), enabled: Boolean(hook) });
  const [openId, setOpenId] = useState<number | null>(null);
  const retry = useMutation({
    mutationFn: (id: number) => api.retryDelivery(hook!.id, id),
    onSuccess: () => {
      toast.success(t('tools.dev.hooks.retried'));
      void qc.invalidateQueries({ queryKey: key });
    }
  });
  return (
    <Dialog open={Boolean(hook)} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col p-0">
        <div className="border-b px-5 py-4 pr-12">
          <DialogTitle className="text-base font-semibold">{t('tools.dev.hooks.deliveries')}</DialogTitle>
          <DialogDescription className="truncate font-mono text-xs text-muted-foreground">{hook?.url}</DialogDescription>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {q.isPending ? (
            <Skeleton className="m-5 h-32" />
          ) : (q.data ?? []).length === 0 ? (
            <p className="px-5 py-10 text-center text-[13px] text-muted-foreground">{t('tools.dev.hooks.noDeliveries')}</p>
          ) : (
            <ul className="divide-y">
              {(q.data ?? []).map((d) => (
                <li key={d.id} className="px-5 py-2.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setOpenId(openId === d.id ? null : d.id)}>
                      <span className="text-[13px] font-medium">{t(`tools.events.${d.event}`, { defaultValue: d.event })}</span>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {relativeTime(d.createdAt)} · {t('tools.dev.hooks.attempts', { count: d.attempts })}
                        {d.responseStatus ? ` · HTTP ${d.responseStatus}` : ''}
                        {d.nextAttemptAt && d.status === 'pending' ? ` · ${t('tools.dev.hooks.nextTry', { when: formatDateTime(d.nextAttemptAt) })}` : ''}
                      </span>
                    </button>
                    <StatusBadge status={d.status} />
                    {d.status !== 'delivered' ? (
                      <Button size="sm" variant="outline" onClick={() => retry.mutate(d.id)} loading={retry.isPending && retry.variables === d.id}>
                        {t('tools.dev.hooks.retry')}
                      </Button>
                    ) : null}
                  </div>
                  {openId === d.id ? (
                    <div className="mt-2 space-y-2">
                      <JsonBlock value={d.payload ?? {}} />
                      {d.responseBody ? <JsonBlock value={d.responseBody} className="max-h-40" /> : null}
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---- Reference & playground ----

const SAMPLE_QUERY = `query {
  leads(first: 5) {
    totalCount
    nodes { id code displayName status }
  }
}`;

function ReferenceTab() {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const keys = useQuery({ queryKey: toolKeys.one(code, 'api-keys'), queryFn: () => api.apiKeys() });
  const [query, setQuery] = useState(SAMPLE_QUERY);
  const [vars, setVars] = useState('');
  const [result, setResult] = useState<unknown>(null);
  const run = useMutation({
    mutationFn: () => {
      let v: Record<string, unknown> | undefined;
      if (vars.trim()) v = JSON.parse(vars) as Record<string, unknown>;
      return api.graphql(query, v);
    },
    onSuccess: setResult,
    onError: (e) => setResult({ error: e instanceof SyntaxError ? t('tools.dev.ref.badVars') : errorText(e, t('common.genericError')) })
  });
  const download = useMutation({
    mutationFn: () => api.openapi(),
    onSuccess: (spec) => {
      const blob = new Blob([JSON.stringify(spec, null, 2)], { type: 'application/json' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = `${code}-openapi.json`;
      a.click();
      URL.revokeObjectURL(a.href);
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const base = keys.data?.baseUrl ?? `${window.location.origin}/api/crm/v1/w/${code}`;
  return (
    <div className="space-y-4">
      {keys.data && !keys.data.apiAccess ? <SetupOff what="api" /> : null}
      <Card>
        <CardHeader
          title={t('tools.dev.ref.title')}
          description={t('tools.dev.ref.body')}
          actions={
            <Button size="sm" variant="outline" onClick={() => download.mutate()} loading={download.isPending}>
              <Download /> {t('tools.dev.ref.openapi')}
            </Button>
          }
        />
        <div className="grid gap-4 px-5 py-4 lg:grid-cols-2">
          <CopyField label={t('tools.dev.ref.restBase')} value={`${base}/crm/{object}`} hint={t('tools.dev.ref.restHint')} />
          <CopyField label={t('tools.dev.ref.graphqlUrl')} value={`${base}/graphql`} hint={t('tools.dev.ref.graphqlHint')} />
        </div>
        <div className="border-t px-5 py-4">
          <p className="mb-1.5 text-xs font-medium text-muted-foreground">{t('tools.dev.ref.example')}</p>
          <JsonBlock
            value={`curl ${base}/crm/leads?limit=5 \\\n  -H "Authorization: Bearer crm_xxxxxxxx_your-secret"\n\ncurl -X POST ${base}/crm/leads \\\n  -H "Authorization: Bearer crm_xxxxxxxx_your-secret" \\\n  -H "Content-Type: application/json" \\\n  -d '{"values": {"name": "Priya Sharma", "email": "priya@example.com"}}'`}
          />
          <p className="mt-2 text-xs text-muted-foreground">{t('tools.dev.ref.limits')}</p>
        </div>
      </Card>
      <Card>
        <CardHeader title={t('tools.dev.ref.playground')} description={t('tools.dev.ref.playgroundBody')} />
        <div className="grid gap-4 px-5 py-4 lg:grid-cols-2">
          <div className="space-y-3">
            <Field label={t('tools.dev.ref.query')}>
              <Textarea value={query} onChange={(e) => setQuery(e.target.value)} rows={12} className="font-mono text-xs" spellCheck={false} />
            </Field>
            <Field label={t('tools.dev.ref.variables')}>
              <Textarea value={vars} onChange={(e) => setVars(e.target.value)} rows={3} className="font-mono text-xs" placeholder='{"first": 10}' spellCheck={false} />
            </Field>
            <Button onClick={() => run.mutate()} loading={run.isPending}>
              <Play /> {t('tools.dev.ref.run')}
            </Button>
          </div>
          <div>
            <p className="mb-1.5 text-[13px] font-medium">{t('tools.dev.ref.result')}</p>
            {result ? <JsonBlock value={result} className="max-h-[420px]" /> : <p className="rounded-md border border-dashed px-3 py-10 text-center text-xs text-muted-foreground">{t('tools.dev.ref.noResult')}</p>}
          </div>
        </div>
      </Card>
    </div>
  );
}
