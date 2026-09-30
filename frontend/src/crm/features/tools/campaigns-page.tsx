import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Megaphone, Plus, Send, Trash2 } from 'lucide-react';
import type { Campaign } from '@crm/api/types-features';
import type { FilterGroup } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader, Tabs } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace, workspaceBase } from '@crm/features/workspace/workspace-context';
import { FilterBuilder } from '@crm/features/records/list/filter-builder';
import { cleanFilter, emptyGroup } from '@crm/features/records/list/filter-utils';
import { relativeTime } from '@crm/lib/utils';
import { errorText, formatDateTime, hasCap, StatusBadge, toolKeys, useObjectMeta, useTools, useWorkspaceObjects } from './tool-utils';

const EMAIL_OBJECTS = ['contacts', 'leads', 'accounts'];

/** /crm/w/:ws/campaigns — email campaigns (campaigns.manage). */
export function CampaignsPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('tools.camp.title'));
  const api = useTools();
  const navigate = useNavigate();
  const allowed = hasCap('campaigns.manage')(context);
  const q = useQuery({ queryKey: toolKeys.one(code, 'campaigns'), queryFn: () => api.campaigns(), enabled: allowed });
  const [open, setOpen] = useState(false);
  if (!allowed) return <NoAccessPage />;
  return (
    <PageContainer>
      <PageHeader
        title={t('tools.camp.title')}
        description={t('tools.camp.subtitle')}
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus /> {t('tools.camp.new')}
          </Button>
        }
      />
      {q.data && !q.data.senderReady ? <Alert tone="warning" className="mb-4" title={t('tools.camp.noSenderTitle')}>{t('tools.camp.noSenderBody')}</Alert> : null}
      {q.data ? <p className="mb-3 text-xs text-muted-foreground">{t('tools.camp.quota', { sent: q.data.sentToday, limit: q.data.dailyLimit })}</p> : null}
      <Card>
        {q.isPending ? (
          <Skeleton className="m-5 h-40" />
        ) : q.isError ? (
          <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />
        ) : q.data.data.length === 0 ? (
          <EmptyState
            icon={Megaphone}
            title={t('tools.camp.emptyTitle')}
            body={t('tools.camp.emptyBody')}
            action={
              <Button onClick={() => setOpen(true)}>
                <Plus /> {t('tools.camp.new')}
              </Button>
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-[13px]">
              <thead className="border-b bg-muted/40 text-left text-xs text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">{t('tools.camp.name')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.camp.statusCol')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.camp.sentCol')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.camp.failedCol')}</th>
                  <th className="px-4 py-2 font-medium">{t('tools.camp.when')}</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {q.data.data.map((c) => (
                  <tr key={c.id} className="hover:bg-muted/40">
                    <td className="px-4 py-2.5">
                      <Link to={`${workspaceBase(code)}/campaigns/${c.id}`} className="font-medium text-primary hover:underline">
                        {c.name}
                      </Link>
                      <p className="truncate text-xs text-muted-foreground">{c.subject || t('tools.camp.noSubject')}</p>
                    </td>
                    <td className="px-4 py-2.5">
                      <StatusBadge status={c.status} />
                    </td>
                    <td className="px-4 py-2.5 tabular-nums">{c.stats.sent ?? 0}</td>
                    <td className="px-4 py-2.5 tabular-nums">{c.stats.failed ?? 0}</td>
                    <td className="px-4 py-2.5 text-muted-foreground">
                      {c.sentAt ? formatDateTime(c.sentAt) : c.scheduledAt ? t('tools.camp.scheduledFor', { when: formatDateTime(c.scheduledAt) }) : relativeTime(c.updatedAt)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <NewCampaignDialog open={open} onOpenChange={setOpen} onCreated={(c) => navigate(`${workspaceBase(code)}/campaigns/${c.id}`)} />
    </PageContainer>
  );
}

function NewCampaignDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; onCreated: (c: Campaign) => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const objects = useWorkspaceObjects().filter((o) => EMAIL_OBJECTS.includes(o.key));
  const [name, setName] = useState('');
  const [object, setObject] = useState('contacts');
  const m = useMutation({
    mutationFn: () => api.createCampaign({ name: name.trim(), object, emailField: 'email' }),
    onSuccess: (c) => {
      onOpenChange(false);
      setName('');
      onCreated(c);
    }
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('tools.camp.new')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.camp.newBody')}</DialogDescription>
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) m.mutate();
          }}
        >
          {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
          <Field label={t('tools.camp.name')}>
            <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={120} placeholder={t('tools.camp.namePlaceholder')} />
          </Field>
          <Field label={t('tools.camp.sendTo')}>
            <Select value={object} onChange={(e) => setObject(e.target.value)} options={objects.map((o) => ({ value: o.key, label: o.label }))} />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={m.isPending} disabled={!name.trim()}>
              {t('tools.camp.create')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** /crm/w/:ws/campaigns/:id — write, pick the audience, test and send. */
export function CampaignEditorPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  const { id = '' } = useParams();
  const api = useTools();
  const allowed = hasCap('campaigns.manage')(context);
  const q = useQuery({ queryKey: toolKeys.one(code, 'campaign', id), queryFn: () => api.campaign(id), enabled: allowed });
  useDocumentTitle(q.data?.name ?? t('tools.camp.title'));
  if (!allowed) return <NoAccessPage />;
  if (q.isPending) return <PageContainer><Skeleton className="h-96 w-full" /></PageContainer>;
  if (q.isError)
    return (
      <PageContainer>
        <Card>
          <ErrorState title={t('tools.common.loadError')} message={errorText(q.error, t('common.genericError'))} onRetry={() => void q.refetch()} />
        </Card>
      </PageContainer>
    );
  return <CampaignEditor key={q.data.updatedAt} c={q.data} />;
}

function CampaignEditor({ c }: { c: Campaign }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const objects = useWorkspaceObjects().filter((o) => EMAIL_OBJECTS.includes(o.key));
  const [tab, setTab] = useState<'compose' | 'recipients'>(c.status === 'draft' ? 'compose' : 'recipients');
  const [draft, setDraft] = useState({ name: c.name, subject: c.subject, bodyHtml: c.bodyHtml, fromName: c.fromName, replyTo: c.replyTo, object: c.object, emailField: c.emailField });
  const [filter, setFilter] = useState<FilterGroup>(c.filter ?? emptyGroup());
  const [testTo, setTestTo] = useState('');
  const [sendOpen, setSendOpen] = useState(false);
  const [at, setAt] = useState('');
  const [deleting, setDeleting] = useState(false);
  const meta = useObjectMeta(draft.object);
  const editable = c.status === 'draft' || c.status === 'scheduled';
  const key = toolKeys.one(code, 'campaign', c.id);
  const byKey = useMemo(() => new Map((meta.data?.fields ?? []).map((f) => [f.key, f])), [meta.data]);
  const emailFields = (meta.data?.fields ?? []).filter((f) => f.type === 'email' || f.type === 'emails');
  const mergeFields = (meta.data?.fields ?? []).filter((f) => ['text', 'email', 'phone', 'select', 'url', 'fullName', 'number', 'currency', 'date'].includes(f.type) || f.key === 'ownerId');

  const saveBody = () => {
    const f = cleanFilter(filter, byKey);
    return { ...draft, filter: f, clearFilter: !f };
  };
  const save = useMutation({
    mutationFn: () => api.updateCampaign(c.id, saveBody()),
    onSuccess: (n) => {
      qc.setQueryData(key, n);
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'campaigns') });
      toast.success(t('tools.camp.saved'));
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const audience = useQuery({ queryKey: [...key, 'audience', c.updatedAt], queryFn: () => api.audience(c.id) });
  const test = useMutation({
    mutationFn: async () => {
      await api.updateCampaign(c.id, saveBody());
      return api.testCampaign(c.id, testTo.trim());
    },
    onSuccess: () => toast.success(t('tools.camp.testSent', { to: testTo.trim() })),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const send = useMutation({
    mutationFn: async () => {
      await api.updateCampaign(c.id, saveBody());
      return api.sendCampaign(c.id, at ? new Date(at).toISOString() : undefined);
    },
    onSuccess: (n) => {
      setSendOpen(false);
      qc.setQueryData(key, n);
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'campaigns') });
      toast.success(at ? t('tools.camp.scheduled') : t('tools.camp.sending'));
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const cancel = useMutation({
    mutationFn: () => api.cancelCampaign(c.id),
    onSuccess: (n) => qc.setQueryData(key, n),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const del = useMutation({
    mutationFn: () => api.deleteCampaign(c.id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: toolKeys.one(code, 'campaigns') });
      navigate(`${workspaceBase(code)}/campaigns`);
    }
  });
  const insert = (k: string) => setDraft((d) => ({ ...d, bodyHtml: `${d.bodyHtml}{{${k}}}` }));

  return (
    <PageContainer wide>
      <PageHeader
        crumbs={[{ label: t('tools.camp.title'), to: `${workspaceBase(code)}/campaigns` }, { label: c.name }]}
        title={c.name}
        badges={<StatusBadge status={c.status} />}
        actions={
          <>
            {c.status === 'draft' ? (
              <Button variant="subtle" onClick={() => setDeleting(true)} aria-label={t('tools.common.delete')}>
                <Trash2 />
              </Button>
            ) : null}
            {c.status === 'scheduled' || c.status === 'sending' ? (
              <Button variant="outline" onClick={() => cancel.mutate()} loading={cancel.isPending}>
                {t('tools.camp.cancel')}
              </Button>
            ) : null}
            {editable ? (
              <>
                <Button variant="outline" onClick={() => save.mutate()} loading={save.isPending}>
                  {t('tools.common.save')}
                </Button>
                <Button onClick={() => setSendOpen(true)}>
                  <Send /> {t('tools.camp.sendButton')}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      <div className="mb-4 flex flex-wrap gap-4 text-[13px]">
        {(['pending', 'sent', 'failed', 'unsubscribed', 'skipped'] as const).map((s) => (
          <span key={s} className="text-muted-foreground">
            {t(`tools.status.${s}`)}: <span className="font-semibold tabular-nums text-foreground">{c.stats[s] ?? 0}</span>
          </span>
        ))}
      </div>
      <Tabs
        className="mb-4"
        value={tab}
        onChange={setTab}
        items={[
          { value: 'compose', label: t('tools.camp.compose') },
          { value: 'recipients', label: t('tools.camp.recipients') }
        ]}
      />
      {tab === 'recipients' ? (
        <RecipientsTab id={c.id} />
      ) : (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
          <div className="space-y-4">
            <Card>
              <CardHeader title={t('tools.camp.message')} />
              <fieldset disabled={!editable} className="space-y-4 px-5 py-4">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('tools.camp.name')}>
                    <Input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} maxLength={120} />
                  </Field>
                  <Field label={t('tools.camp.fromName')} hint={t('tools.camp.fromNameHint')}>
                    <Input value={draft.fromName} onChange={(e) => setDraft({ ...draft, fromName: e.target.value })} maxLength={80} />
                  </Field>
                </div>
                <Field label={t('tools.camp.replyTo')} hint={t('tools.camp.replyToHint')}>
                  <Input value={draft.replyTo} onChange={(e) => setDraft({ ...draft, replyTo: e.target.value })} type="email" />
                </Field>
                <Field label={t('tools.camp.subject')}>
                  <Input value={draft.subject} onChange={(e) => setDraft({ ...draft, subject: e.target.value })} maxLength={200} placeholder={t('tools.camp.subjectPlaceholder', { ph: '{{firstName}}' })} />
                </Field>
                <div>
                  <div className="mb-1.5 flex flex-wrap items-center justify-between gap-2">
                    <p className="text-[13px] font-medium">{t('tools.camp.body')}</p>
                    <Select
                      className="h-8 w-52 text-xs"
                      value=""
                      onChange={(e) => e.target.value && insert(e.target.value)}
                      placeholder={t('tools.camp.insertField')}
                      options={[{ value: 'name', label: t('tools.camp.recordName') }, ...mergeFields.map((f) => ({ value: f.key, label: f.label }))]}
                    />
                  </div>
                  <Textarea value={draft.bodyHtml} onChange={(e) => setDraft({ ...draft, bodyHtml: e.target.value })} rows={14} className="font-mono text-xs" placeholder={'<p>Hi {{firstName}},</p>\n<p>…</p>'} spellCheck={false} />
                  <p className="mt-1 text-xs text-muted-foreground">{t('tools.camp.bodyHint', { ph: '{{firstName}}' })}</p>
                </div>
              </fieldset>
            </Card>
            <Card>
              <CardHeader title={t('tools.camp.audience')} description={t('tools.camp.audienceBody')} />
              <fieldset disabled={!editable} className="space-y-4 px-5 py-4">
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label={t('tools.camp.sendTo')}>
                    <Select
                      value={draft.object}
                      onChange={(e) => {
                        setDraft({ ...draft, object: e.target.value, emailField: 'email' });
                        setFilter(emptyGroup());
                      }}
                      options={objects.map((o) => ({ value: o.key, label: o.label }))}
                    />
                  </Field>
                  <Field label={t('tools.camp.emailField')}>
                    <Select value={draft.emailField} onChange={(e) => setDraft({ ...draft, emailField: e.target.value })} options={emailFields.map((f) => ({ value: f.key, label: f.label }))} />
                  </Field>
                </div>
                {meta.data ? <FilterBuilder meta={meta.data} value={filter} onChange={setFilter} /> : <Skeleton className="h-16" />}
                <div className="rounded-lg border bg-muted/30 px-3.5 py-3 text-[13px]">
                  {audience.isPending ? (
                    t('tools.camp.counting')
                  ) : audience.data ? (
                    <>
                      <p>
                        {t('tools.camp.audienceCount', { count: audience.data.total })}{' '}
                        <span className="text-muted-foreground">{t('tools.camp.audienceSaved')}</span>
                      </p>
                      {audience.data.sample.length ? (
                        <p className="mt-1 truncate text-xs text-muted-foreground">{audience.data.sample.map((s) => `${s.title} <${s.email}>`).join(', ')}</p>
                      ) : null}
                      {audience.data.unsubscribed ? <p className="mt-1 text-xs text-muted-foreground">{t('tools.camp.unsubscribedNote', { count: audience.data.unsubscribed })}</p> : null}
                    </>
                  ) : null}
                </div>
              </fieldset>
            </Card>
          </div>
          <div className="space-y-4">
            <Card>
              <CardHeader title={t('tools.camp.preview')} description={t('tools.camp.previewBody')} />
              <div className="px-5 py-4">
                <p className="mb-2 text-[13px]">
                  <span className="text-muted-foreground">{t('tools.camp.subject')}: </span>
                  <span className="font-medium">{draft.subject || '—'}</span>
                </p>
                <iframe title={t('tools.camp.preview')} sandbox="" srcDoc={draft.bodyHtml || '<p style="color:#888;font-family:sans-serif">…</p>'} className="h-[420px] w-full rounded-md border bg-white" />
              </div>
            </Card>
            {editable ? (
              <Card>
                <CardHeader title={t('tools.camp.test')} description={t('tools.camp.testBody')} />
                <form
                  className="flex gap-2 px-5 py-4"
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (testTo.trim()) test.mutate();
                  }}
                >
                  <Input value={testTo} onChange={(e) => setTestTo(e.target.value)} type="email" placeholder="you@company.com" aria-label={t('tools.camp.testTo')} />
                  <Button type="submit" variant="outline" loading={test.isPending} disabled={!testTo.trim()}>
                    {t('tools.camp.sendTest')}
                  </Button>
                </form>
              </Card>
            ) : null}
          </div>
        </div>
      )}
      <Dialog open={sendOpen} onOpenChange={setSendOpen}>
        <DialogContent className="max-w-md p-5">
          <DialogTitle className="pr-8 text-base font-semibold">{t('tools.camp.sendTitle')}</DialogTitle>
          <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.camp.sendBody', { count: audience.data?.total ?? 0 })}</DialogDescription>
          <div className="mt-4 space-y-4">
            <Field label={t('tools.camp.sendAt')} hint={t('tools.camp.sendAtHint')}>
              <Input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} />
            </Field>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setSendOpen(false)}>
                {t('common.cancel')}
              </Button>
              <Button onClick={() => send.mutate()} loading={send.isPending}>
                {at ? t('tools.camp.schedule') : t('tools.camp.sendNow')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={t('tools.camp.deleteTitle')}
        body={t('tools.camp.deleteBody')}
        confirmLabel={t('tools.common.delete')}
        tone="danger"
        loading={del.isPending}
        onConfirm={() => del.mutate()}
      />
    </PageContainer>
  );
}

function RecipientsTab({ id }: { id: string }) {
  const { t } = useTranslation();
  const { code } = useWorkspace();
  const api = useTools();
  const [status, setStatus] = useState('');
  const q = useQuery({ queryKey: toolKeys.one(code, 'campaign', id, 'recipients', status), queryFn: () => api.recipients(id, status || undefined), refetchInterval: 15_000 });
  return (
    <Card>
      <CardHeader
        title={t('tools.camp.recipients')}
        actions={
          <Select
            className="h-8 w-40 text-xs"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            options={[{ value: '', label: t('tools.camp.allStatuses') }, ...(['pending', 'sent', 'failed', 'unsubscribed', 'skipped'] as const).map((s) => ({ value: s, label: t(`tools.status.${s}`) }))]}
          />
        }
      />
      {q.isPending ? (
        <Skeleton className="m-5 h-32" />
      ) : (q.data ?? []).length === 0 ? (
        <p className="px-5 py-10 text-center text-[13px] text-muted-foreground">{t('tools.camp.noRecipients')}</p>
      ) : (
        <ul className="divide-y">
          {(q.data ?? []).map((r) => (
            <li key={r.recordId} className="flex flex-wrap items-center gap-2 px-5 py-2.5 text-[13px]">
              <span className="min-w-0 flex-1 truncate">
                <span className="font-medium">{r.name}</span> <span className="text-muted-foreground">&lt;{r.email}&gt;</span>
              </span>
              {r.error ? <span className="max-w-xs truncate text-xs text-danger">{r.error}</span> : null}
              {r.sentAt ? <span className="text-xs text-muted-foreground">{relativeTime(r.sentAt)}</span> : null}
              <StatusBadge status={r.status} />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
