import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowLeft, ChevronRight, EyeOff, Mail, MailOpen, Reply, ReplyAll, Send } from 'lucide-react';
import { isApiError } from '@crm/api/client';
import { workspaceToolsApi } from '@crm/api/endpoints';
import type { ObjectKey } from '@crm/api/types';
import type { EmailThread, ThreadMessage } from '@crm/api/types-features';
import { Alert, Badge } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { EmptyState, ErrorState } from '@crm/components/states';
import { RichTextEditor, RichTextView } from '@crm/components/rich-text';
import { cn, relativeTime } from '@crm/lib/utils';
import { recordKeys } from './use-object-meta';
import { useRecordScope } from './record-scope';

export const emailThreadsKey = (prefix: string, object: ObjectKey, id: string) => [...recordKeys.detail(prefix, object, id), 'emails'] as const;

const splitList = (s: string) =>
  s
    .split(/[,;\s]+/)
    .map((x) => x.trim())
    .filter(Boolean);

/** Who a reply goes to: the last person who wrote to us, else whoever we last wrote to. */
function replyTargets(thread: EmailThread, all: boolean) {
  const ours = new Set(thread.messages.filter((m) => m.direction === 'outbound').map((m) => m.from.toLowerCase()));
  const lastIn = [...thread.messages].reverse().find((m) => m.direction === 'inbound');
  const last = thread.messages[thread.messages.length - 1];
  const to = lastIn ? [lastIn.from.toLowerCase()] : (last?.to ?? []).map((a) => a.toLowerCase());
  const cc = all ? thread.participants.filter((a) => !to.includes(a) && !ours.has(a)) : [];
  return { to, cc, parent: lastIn ?? last };
}

/** The Emails tab: a record's conversations, each opening into its messages with Reply. */
export function RecordEmails({ object, id, canSend, onNewEmail }: { object: ObjectKey; id: string; canSend: boolean; onNewEmail?: () => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const q = useQuery({ queryKey: emailThreadsKey(scope.prefix, object, id), queryFn: () => scope.api.emailThreads(object, id) });
  const [openId, setOpenId] = useState<string | null>(null);
  const open = q.data?.find((th) => th.id === openId || th.messages.some((m) => m.id === openId));

  if (q.isError) return <ErrorState title={t('emails.loadError')} message={isApiError(q.error) ? q.error.message : undefined} onRetry={() => void q.refetch()} />;
  if (!q.data)
    return (
      <div className="space-y-2 p-4">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-14 w-full" />
        ))}
      </div>
    );
  if (open) return <ThreadView object={object} id={id} thread={open} canSend={canSend} onBack={() => setOpenId(null)} />;
  return (
    <div className="p-3 sm:p-4">
      <div className="mb-3 flex items-center justify-between gap-2">
        <p className="text-[13px] text-muted-foreground">{t('emails.count', { count: q.data.length })}</p>
        {canSend && onNewEmail ? (
          <Button size="sm" variant="outline" onClick={onNewEmail}>
            <Mail /> {t('emails.new')}
          </Button>
        ) : null}
      </div>
      {q.data.length === 0 ? (
        <EmptyState icon={Mail} title={t('emails.noneTitle')} body={t('emails.noneBody')} />
      ) : (
        <ul className="divide-y rounded-lg border">
          {q.data.map((th) => {
            const last = th.messages[th.messages.length - 1];
            return (
              <li key={th.id}>
                <button type="button" onClick={() => setOpenId(th.id)} className="group flex w-full items-start gap-3 px-3 py-2.5 text-left hover:bg-muted/50">
                  <span className={cn('mt-0.5 grid size-8 shrink-0 place-items-center rounded-full', last?.direction === 'inbound' ? 'bg-primary-soft text-primary' : 'bg-muted text-muted-foreground')}>
                    {last?.direction === 'inbound' ? <Mail className="size-4" /> : <MailOpen className="size-4" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="truncate text-[13px] font-semibold group-hover:text-primary">{th.subject || t('emails.private')}</span>
                      {th.count > 1 ? <Badge tone="neutral">{th.count}</Badge> : null}
                    </span>
                    <span className="block truncate text-xs text-muted-foreground">{th.participants.join(', ')}</span>
                    {th.snippet ? <span className="mt-0.5 block truncate text-xs text-foreground/80">{th.snippet}</span> : null}
                  </span>
                  <span className="shrink-0 text-[11px] text-muted-foreground">{relativeTime(th.lastAt)}</span>
                  <ChevronRight className="mt-1 size-4 shrink-0 text-muted-foreground" aria-hidden />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function ThreadView({ object, id, thread, canSend, onBack }: { object: ObjectKey; id: string; thread: EmailThread; canSend: boolean; onBack: () => void }) {
  const { t } = useTranslation();
  // The latest message starts open; clicking a message flips it.
  const [toggled, setToggled] = useState<Set<string>>(() => new Set());
  const lastId = thread.messages[thread.messages.length - 1]?.id;
  const [reply, setReply] = useState<null | 'one' | 'all'>(null);
  const hidden = thread.messages.every((m) => m.hidden === 'all');
  return (
    <div className="p-3 sm:p-4">
      <div className="mb-3 flex items-start gap-2">
        <Button size="icon" variant="ghost" onClick={onBack} aria-label={t('emails.back')}>
          <ArrowLeft />
        </Button>
        <div className="min-w-0 flex-1">
          <h3 className="text-[15px] font-semibold">{thread.subject || t('emails.private')}</h3>
          <p className="text-xs text-muted-foreground">{t('emails.messages', { count: thread.count })}</p>
        </div>
      </div>
      <ol className="space-y-2">
        {thread.messages.map((m) => (
          <MessageCard
            key={m.id}
            m={m}
            open={toggled.has(m.id) !== (m.id === lastId)}
            onToggle={() =>
              setToggled((s) => {
                const n = new Set(s);
                if (n.has(m.id)) n.delete(m.id);
                else n.add(m.id);
                return n;
              })
            }
          />
        ))}
      </ol>
      {canSend && !hidden ? (
        reply ? (
          <ReplyBox key={reply} object={object} id={id} thread={thread} all={reply === 'all'} onDone={() => setReply(null)} />
        ) : (
          <div className="mt-3 flex gap-2">
            <Button size="sm" onClick={() => setReply('one')}>
              <Reply /> {t('emails.reply')}
            </Button>
            {thread.participants.length > 2 ? (
              <Button size="sm" variant="outline" onClick={() => setReply('all')}>
                <ReplyAll /> {t('emails.replyAll')}
              </Button>
            ) : null}
          </div>
        )
      ) : null}
    </div>
  );
}

function MessageCard({ m, open, onToggle }: { m: ThreadMessage; open: boolean; onToggle: () => void }) {
  const { t } = useTranslation();
  const preview = m.body || (m.html ? m.html.replace(/<[^>]+>/g, ' ') : '');
  return (
    <li className={cn('rounded-lg border', m.direction === 'outbound' ? 'bg-muted/30' : 'bg-background')}>
      <button type="button" onClick={onToggle} aria-expanded={open} className="flex w-full items-start gap-2 px-3 py-2 text-left">
        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-x-2 text-[13px]">
            <span className="font-semibold">{m.fromName || m.from}</span>
            {m.fromName ? <span className="text-xs text-muted-foreground">{m.from}</span> : null}
            {m.status === 'failed' ? <Badge tone="danger">{t('emails.failed')}</Badge> : null}
          </span>
          {open ? (
            <span className="block text-xs text-muted-foreground">
              {t('emails.toLine', { to: m.to.join(', ') })}
              {m.cc?.length ? ` · ${t('emails.ccLine', { cc: m.cc.join(', ') })}` : ''}
            </span>
          ) : (
            <span className="block truncate text-xs text-muted-foreground">{m.hidden ? t('emails.hiddenBody') : preview.replace(/\s+/g, ' ').trim()}</span>
          )}
        </span>
        <span className="shrink-0 text-[11px] text-muted-foreground" title={new Date(m.at).toLocaleString()}>
          {relativeTime(m.at)}
        </span>
      </button>
      {open ? (
        <div className="border-t px-3 py-2.5 text-[13px]">
          {m.error ? <Alert tone="danger" className="mb-2">{m.error}</Alert> : null}
          {m.hidden ? (
            <p className="inline-flex items-center gap-1.5 text-muted-foreground">
              <EyeOff className="size-3.5" /> {t('emails.hiddenBody')}
            </p>
          ) : m.html ? (
            <RichTextView value={m.html} />
          ) : (
            <p className="whitespace-pre-wrap break-words">{m.body}</p>
          )}
        </div>
      ) : null}
    </li>
  );
}

function ReplyBox({ object, id, thread, all, onDone }: { object: ObjectKey; id: string; thread: EmailThread; all: boolean; onDone: () => void }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const targets = useMemo(() => replyTargets(thread, all), [thread, all]);
  const code = scope.prefix.startsWith('/w/') ? decodeURIComponent(scope.prefix.slice(3)) : '';
  const boxes = useQuery({ queryKey: ['mailboxes', code], queryFn: () => workspaceToolsApi(code).mailboxes(), enabled: Boolean(code) });
  const active = (boxes.data?.data ?? []).filter((b) => b.status !== 'paused');
  const [mailboxId, setMailboxId] = useState<string | null>(null);
  const chosenBox = mailboxId ?? (active.find((b) => b.id === targets.parent?.mailboxId)?.id || '');
  const [to, setTo] = useState(targets.to.join(', '));
  const [cc, setCc] = useState(targets.cc.join(', '));
  const [html, setHtml] = useState('');
  const [error, setError] = useState<string | null>(null);
  const send = useMutation({
    mutationFn: () =>
      scope.api.sendEmail(object, id, { to: splitList(to), cc: splitList(cc), html, replyTo: targets.parent?.id, mailboxId: chosenBox || undefined }),
    onSuccess: () => {
      toast.success(t('email.sent'));
      void qc.invalidateQueries({ queryKey: emailThreadsKey(scope.prefix, object, id) });
      void qc.invalidateQueries({ queryKey: [...recordKeys.detail(scope.prefix, object, id), 'timeline'] });
      onDone();
    },
    onError: (e) => setError(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const canSend = Boolean(html.trim()) && splitList(to).length > 0;
  return (
    <div className="mt-3 space-y-2 rounded-lg border bg-muted/20 p-3">
      {error ? <Alert tone="danger">{error}</Alert> : null}
      <div className="grid gap-2 sm:grid-cols-[auto_1fr] sm:items-center">
        <label className="text-xs font-medium text-muted-foreground" htmlFor="reply-from">
          {t('email.from')}
        </label>
        <Select id="reply-from" value={chosenBox} onChange={(e) => setMailboxId(e.target.value)}>
          <option value="">{t('email.fromCrm')}</option>
          {active.map((b) => (
            <option key={b.id} value={b.id}>
              {b.email}
            </option>
          ))}
        </Select>
        <label className="text-xs font-medium text-muted-foreground" htmlFor="reply-to">
          {t('email.to')}
        </label>
        <Input id="reply-to" value={to} onChange={(e) => setTo(e.target.value)} />
        <label className="text-xs font-medium text-muted-foreground" htmlFor="reply-cc">
          {t('email.cc')}
        </label>
        <Input id="reply-cc" value={cc} onChange={(e) => setCc(e.target.value)} />
      </div>
      <RichTextEditor value={html} onChange={setHtml} placeholder={t('emails.replyPlaceholder')} ariaLabel={t('emails.reply')} autoFocus onSubmit={() => canSend && !send.isPending && send.mutate()} />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={onDone}>
          {t('common.cancel')}
        </Button>
        <Button
          size="sm"
          disabled={!canSend}
          loading={send.isPending}
          onClick={() => {
            setError(null);
            send.mutate();
          }}
        >
          <Send /> {t('emails.send')}
        </Button>
      </div>
    </div>
  );
}
