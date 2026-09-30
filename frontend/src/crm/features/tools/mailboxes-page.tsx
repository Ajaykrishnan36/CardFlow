import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Mail, Plus, RefreshCw, Server, Trash2, X } from 'lucide-react';
import type { Mailbox, MailboxesResponse } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Alert, Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Field } from '@crm/components/ui/field';
import { Input } from '@crm/components/ui/input';
import { PasswordInput } from '@crm/components/ui/password-input';
import { Select, Switch } from '@crm/components/ui/form-controls';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { Skeleton } from '@crm/components/ui/spinner';
import { ConfirmDialog, PageContainer, PageHeader } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { NoAccessPage } from '@crm/features/system/pages';
import { useWorkspace } from '@crm/features/workspace/workspace-context';
import { relativeTime } from '@crm/lib/utils';
import { errorText, hasCap, SettingRow, StatusBadge, toolKeys, useTools } from './tool-utils';

/** /crm/w/:ws/settings/email — the member's connected mailboxes and calendars (email.send). */
export function MailboxesPage() {
  const { t } = useTranslation();
  const { code, context } = useWorkspace();
  useDocumentTitle(t('tools.mail.title'));
  const api = useTools();
  const qc = useQueryClient();
  const key = toolKeys.one(code, 'mailboxes');
  const allowed = hasCap('email.send')(context);
  const q = useQuery({ queryKey: key, queryFn: () => api.mailboxes(), enabled: allowed });
  const [sp, setSp] = useSearchParams();
  const [imapOpen, setImapOpen] = useState(false);
  const [removing, setRemoving] = useState<Mailbox | null>(null);
  const [block, setBlock] = useState('');
  const set = (d: MailboxesResponse) => qc.setQueryData(key, d);

  useEffect(() => {
    const err = sp.get('oauthError');
    if (err) {
      toast.error(err);
      sp.delete('oauthError');
      setSp(sp, { replace: true });
    } else if (sp.get('connected')) {
      toast.success(t('tools.mail.connected'));
      sp.delete('connected');
      setSp(sp, { replace: true });
    }
  }, [sp, setSp, t]);

  const connect = useMutation({
    mutationFn: (p: 'google' | 'microsoft') => api.connectMailbox(p),
    onSuccess: (r) => window.location.assign(r.url),
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const update = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Parameters<typeof api.updateMailbox>[1] }) => api.updateMailbox(id, body),
    onSuccess: set,
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const sync = useMutation({
    mutationFn: (id: string) => api.syncMailbox(id),
    onSuccess: (r) => {
      if (r.error) toast.error(r.error);
      else toast.success(t('tools.mail.synced', { emails: r.emails, events: r.events, contacts: r.contactsCreated }));
      void qc.invalidateQueries({ queryKey: key });
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteMailbox(id),
    onSuccess: () => {
      setRemoving(null);
      toast.success(t('tools.mail.removed'));
      void qc.invalidateQueries({ queryKey: key });
    }
  });
  const blocklist = useMutation({
    mutationFn: (body: { add?: string; remove?: string }) => api.blocklist(body),
    onSuccess: (d) => {
      set(d);
      setBlock('');
    },
    onError: (e) => toast.error(errorText(e, t('common.genericError')))
  });

  if (!allowed) return <NoAccessPage />;
  return (
    <PageContainer>
      <PageHeader title={t('tools.mail.title')} description={t('tools.mail.subtitle')} />
      {q.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : q.isError ? (
        <Card>
          <ErrorState title={t('tools.common.loadError')} onRetry={() => void q.refetch()} />
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
          <div className="space-y-4">
            <Card>
              <CardHeader title={t('tools.mail.accounts')} description={t('tools.mail.accountsBody')} />
              <div className="flex flex-wrap gap-2 border-b px-5 py-3">
                <Button variant="outline" size="sm" onClick={() => connect.mutate('google')} disabled={!q.data.providers.google} loading={connect.isPending && connect.variables === 'google'}>
                  <Plus /> {t('tools.mail.google')}
                </Button>
                <Button variant="outline" size="sm" onClick={() => connect.mutate('microsoft')} disabled={!q.data.providers.microsoft} loading={connect.isPending && connect.variables === 'microsoft'}>
                  <Plus /> {t('tools.mail.microsoft')}
                </Button>
                <Button variant="outline" size="sm" onClick={() => setImapOpen(true)}>
                  <Server /> {t('tools.mail.imap')}
                </Button>
              </div>
              {!q.data.providers.google || !q.data.providers.microsoft ? <p className="border-b px-5 py-2 text-xs text-muted-foreground">{t('tools.mail.providerOff')}</p> : null}
              {q.data.data.length === 0 ? (
                <EmptyState icon={Mail} title={t('tools.mail.emptyTitle')} body={t('tools.mail.emptyBody')} />
              ) : (
                <ul className="divide-y">
                  {q.data.data.map((m) => (
                    <li key={m.id} className="px-5 py-4">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="min-w-0 flex-1 truncate text-[14px] font-semibold">{m.email}</p>
                        <Badge tone="neutral">{t(`tools.mail.provider.${m.provider}`)}</Badge>
                        <StatusBadge status={m.status} />
                        <Button size="sm" variant="outline" onClick={() => sync.mutate(m.id)} loading={sync.isPending && sync.variables === m.id}>
                          <RefreshCw /> {t('tools.mail.syncNow')}
                        </Button>
                        <Button size="icon-sm" variant="subtle" onClick={() => setRemoving(m)} aria-label={t('tools.mail.remove')}>
                          <Trash2 />
                        </Button>
                      </div>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        {m.lastSyncedAt ? t('tools.mail.lastSync', { when: relativeTime(m.lastSyncedAt) }) : t('tools.mail.neverSynced')} · {t('tools.mail.messages', { count: m.messages })}
                      </p>
                      {m.error ? (
                        <Alert tone="danger" className="mt-2">
                          {m.error}
                        </Alert>
                      ) : null}
                      <div className="mt-2 divide-y rounded-lg border px-4">
                        <SettingRow title={t('tools.mail.syncEmail')} body={t('tools.mail.syncEmailHint')}>
                          <Switch checked={m.syncEmail} onCheckedChange={(v) => update.mutate({ id: m.id, body: { syncEmail: v } })} aria-label={t('tools.mail.syncEmail')} />
                        </SettingRow>
                        {m.provider !== 'imap' ? (
                          <SettingRow title={t('tools.mail.syncCalendar')} body={t('tools.mail.syncCalendarHint')}>
                            <Switch checked={m.syncCalendar} onCheckedChange={(v) => update.mutate({ id: m.id, body: { syncCalendar: v } })} aria-label={t('tools.mail.syncCalendar')} />
                          </SettingRow>
                        ) : null}
                        <SettingRow title={t('tools.mail.autoCreate')} body={t('tools.mail.autoCreateHint')}>
                          <Switch checked={m.autoCreateContacts} onCheckedChange={(v) => update.mutate({ id: m.id, body: { autoCreateContacts: v } })} aria-label={t('tools.mail.autoCreate')} />
                        </SettingRow>
                        <SettingRow title={t('tools.mail.visibility')} body={t('tools.mail.visibilityHint')}>
                          <Select
                            className="w-56"
                            value={m.visibility}
                            onChange={(e) => update.mutate({ id: m.id, body: { visibility: e.target.value } })}
                            options={(['share_everything', 'subject', 'metadata'] as const).map((v) => ({ value: v, label: t(`tools.mail.vis.${v}`) }))}
                          />
                        </SettingRow>
                        <SettingRow title={t('tools.mail.paused')} body={t('tools.mail.pausedHint')}>
                          <Switch checked={m.status === 'paused'} onCheckedChange={(v) => update.mutate({ id: m.id, body: { paused: v } })} aria-label={t('tools.mail.paused')} />
                        </SettingRow>
                      </div>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </div>
          <div className="space-y-4">
            <Card>
              <CardHeader title={t('tools.mail.sending')} />
              <div className="px-5 py-4 text-[13px] text-muted-foreground">
                {q.data.crmSender ? t('tools.mail.senderReady') : t('tools.mail.senderMissing')}
              </div>
            </Card>
            <Card>
              <CardHeader title={t('tools.mail.blocklist')} description={t('tools.mail.blocklistBody')} />
              <div className="space-y-3 px-5 py-4">
                <form
                  className="flex gap-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    if (block.trim()) blocklist.mutate({ add: block.trim() });
                  }}
                >
                  <Input value={block} onChange={(e) => setBlock(e.target.value)} placeholder={t('tools.mail.blockPlaceholder')} aria-label={t('tools.mail.blocklist')} />
                  <Button type="submit" size="md" variant="outline" loading={blocklist.isPending}>
                    {t('tools.mail.block')}
                  </Button>
                </form>
                {q.data.blocklist.length ? (
                  <ul className="flex flex-wrap gap-1.5">
                    {q.data.blocklist.map((b) => (
                      <li key={b} className="inline-flex items-center gap-1 rounded-full border bg-muted/50 py-0.5 pl-2.5 pr-1 text-xs">
                        {b}
                        <button type="button" className="grid size-5 place-items-center rounded-full hover:bg-muted" onClick={() => blocklist.mutate({ remove: b })} aria-label={t('tools.common.removeName', { name: b })}>
                          <X className="size-3" />
                        </button>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p className="text-xs text-muted-foreground">{t('tools.mail.blocklistEmpty')}</p>
                )}
              </div>
            </Card>
          </div>
        </div>
      )}
      <ImapDialog open={imapOpen} onOpenChange={setImapOpen} onSaved={set} />
      <ConfirmDialog
        open={Boolean(removing)}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={t('tools.mail.removeTitle', { email: removing?.email })}
        body={t('tools.mail.removeBody')}
        confirmLabel={t('tools.mail.remove')}
        tone="danger"
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing.id)}
      />
    </PageContainer>
  );
}

function ImapDialog({ open, onOpenChange, onSaved }: { open: boolean; onOpenChange: (o: boolean) => void; onSaved: (d: MailboxesResponse) => void }) {
  const { t } = useTranslation();
  const api = useTools();
  const blank = { email: '', imapHost: '', imapPort: '993', smtpHost: '', smtpPort: '587', username: '', password: '' };
  const [f, setF] = useState(blank);
  const m = useMutation({
    mutationFn: () =>
      api.connectImap({
        email: f.email.trim(),
        imapHost: f.imapHost.trim(),
        imapPort: Number(f.imapPort) || 993,
        smtpHost: f.smtpHost.trim() || undefined,
        smtpPort: Number(f.smtpPort) || undefined,
        username: f.username.trim() || undefined,
        password: f.password
      }),
    onSuccess: (d) => {
      toast.success(t('tools.mail.connected'));
      onSaved(d);
      setF(blank);
      onOpenChange(false);
    }
  });
  const on = (k: keyof typeof blank) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg p-5">
        <DialogTitle className="pr-8 text-base font-semibold">{t('tools.mail.imapTitle')}</DialogTitle>
        <DialogDescription className="mt-1 text-sm text-muted-foreground">{t('tools.mail.imapBody')}</DialogDescription>
        <form
          noValidate
          className="mt-4 space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (f.email && f.imapHost && f.password) m.mutate();
          }}
        >
          {m.isError ? <Alert tone="danger">{errorText(m.error, t('common.genericError'))}</Alert> : null}
          <Field label={t('tools.mail.email')}>
            <Input value={f.email} onChange={on('email')} type="email" autoFocus placeholder="you@company.com" />
          </Field>
          <div className="grid grid-cols-[1fr_96px] gap-3">
            <Field label={t('tools.mail.imapHost')}>
              <Input value={f.imapHost} onChange={on('imapHost')} placeholder="imap.company.com" />
            </Field>
            <Field label={t('tools.mail.port')}>
              <Input value={f.imapPort} onChange={on('imapPort')} inputMode="numeric" />
            </Field>
          </div>
          <div className="grid grid-cols-[1fr_96px] gap-3">
            <Field label={t('tools.mail.smtpHost')} hint={t('tools.mail.smtpHint')}>
              <Input value={f.smtpHost} onChange={on('smtpHost')} placeholder="smtp.company.com" />
            </Field>
            <Field label={t('tools.mail.port')}>
              <Input value={f.smtpPort} onChange={on('smtpPort')} inputMode="numeric" />
            </Field>
          </div>
          <Field label={t('tools.mail.username')} hint={t('tools.mail.usernameHint')}>
            <Input value={f.username} onChange={on('username')} autoComplete="off" />
          </Field>
          <Field label={t('tools.mail.password')} hint={t('tools.mail.passwordHint')}>
            <PasswordInput value={f.password} onChange={on('password')} autoComplete="new-password" />
          </Field>
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={m.isPending} disabled={!f.email || !f.imapHost || !f.password}>
              {t('tools.mail.connect')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
