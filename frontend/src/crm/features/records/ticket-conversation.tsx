import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { MessagesSquare, Send } from 'lucide-react';
import { workspaceAppApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { TicketMessage, TicketThread } from '@crm/api/types';
import { Badge, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Skeleton } from '@crm/components/ui/spinner';
import { ErrorState } from '@crm/components/states';
import { Avatar } from '@crm/features/shell/user-menu';
import { cn, relativeTime } from '@crm/lib/utils';

const statusTone = { open: 'warning', in_progress: 'primary', resolved: 'success' } as const;

/**
 * A Business Card Snap ticket as a chat on its case (D-91): the person's messages on the left,
 * support replies on the right with who sent them and their role. New messages from the app
 * show up within a few seconds.
 */
export function TicketConversation({ code, ticketId, canReply }: { code: string; ticketId: string; canReply: boolean }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const api = workspaceAppApi(code);
  const key = ['app-ticket', code, ticketId];
  const q = useQuery({ queryKey: key, queryFn: () => api.ticket(ticketId), refetchInterval: 15_000 });
  const [draft, setDraft] = useState('');
  const endRef = useRef<HTMLLIElement>(null);
  const count = q.data?.messages.length ?? 0;
  useEffect(() => {
    endRef.current?.scrollIntoView({ block: 'nearest' });
  }, [count]);

  const send = useMutation({
    mutationFn: (body: string) => api.replyTicket(ticketId, body),
    onSuccess: (thread: TicketThread) => {
      qc.setQueryData(key, thread);
      setDraft('');
      toast.success(t('records.ticket.sent'));
      // The case's status and "Reply in app" follow on the next sync.
      setTimeout(() => void qc.invalidateQueries({ queryKey: ['records'] }), 3000);
    },
    onError: (e) => toast.error(isApiError(e) ? e.message : String(e))
  });
  const submit = () => {
    const body = draft.trim();
    if (body && !send.isPending) send.mutate(body);
  };
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  const thread = q.data;
  return (
    <Card className="mb-4 overflow-hidden">
      <CardHeader
        className="px-4 py-3"
        title={
          <span className="flex items-center gap-2 text-[13px]">
            <MessagesSquare className="size-4 text-muted-foreground" aria-hidden />
            {t('records.ticket.title')}
            {thread ? <Badge tone={statusTone[thread.status] ?? 'neutral'}>{t(`records.ticket.status.${thread.status}`)}</Badge> : null}
          </span>
        }
        description={thread ? t('records.ticket.subtitle', { name: thread.userName || t('records.ticket.customer') }) : undefined}
      />
      {q.isError ? (
        <div className="p-4">
          <ErrorState title={t('records.ticket.loadError')} onRetry={() => void q.refetch()} />
        </div>
      ) : !thread ? (
        <div className="space-y-3 p-4">
          <Skeleton className="h-12 w-2/3" />
          <Skeleton className="ml-auto h-12 w-2/3" />
        </div>
      ) : (
        <>
          <ol className="max-h-[460px] space-y-4 overflow-y-auto bg-muted/30 px-4 py-4" aria-label={t('records.ticket.title')}>
            {thread.messages.map((m) => (
              <Bubble key={m.id} m={m} />
            ))}
            <li ref={endRef} aria-hidden className="h-0" />
          </ol>
          {canReply ? (
            <div className="border-t p-3">
              <label htmlFor={`ticket-reply-${ticketId}`} className="sr-only">
                {t('records.ticket.placeholder', { name: thread.userName })}
              </label>
              <textarea
                id={`ticket-reply-${ticketId}`}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={onKey}
                rows={2}
                maxLength={4000}
                placeholder={t('records.ticket.placeholder', { name: thread.userName || t('records.ticket.customer') })}
                className="w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground outline-none placeholder:text-muted-foreground/70 focus:border-primary focus:ring-[3px] focus:ring-primary/15"
              />
              <div className="mt-2 flex items-center justify-between gap-3">
                <span className="hidden text-xs text-muted-foreground sm:inline">{t('records.ticket.hint')}</span>
                <Button size="sm" onClick={submit} disabled={!draft.trim() || send.isPending} className="ml-auto">
                  <Send /> {t('records.ticket.send')}
                </Button>
              </div>
            </div>
          ) : null}
        </>
      )}
    </Card>
  );
}

function Bubble({ m }: { m: TicketMessage }) {
  const { t } = useTranslation();
  const mine = m.sender === 'support';
  const who = m.authorName || (mine ? 'Support team' : t('records.ticket.customer'));
  return (
    <li className={cn('flex items-end gap-2', mine && 'flex-row-reverse')}>
      <Avatar name={who} className={cn('size-8', !mine && 'from-slate-400 to-slate-500')} />
      <div className={cn('flex max-w-[78%] flex-col gap-1', mine && 'items-end')}>
        <span className="text-[11.5px] text-muted-foreground">
          <span className="font-medium text-foreground">{who}</span>
          {' · '}
          {m.authorRole || (mine ? 'Support team' : t('records.ticket.customer'))}
          {' · '}
          <time dateTime={m.createdAt} title={new Date(m.createdAt).toLocaleString()}>
            {relativeTime(m.createdAt)}
          </time>
        </span>
        <p
          className={cn(
            'whitespace-pre-wrap break-words rounded-2xl px-3.5 py-2 text-sm leading-relaxed',
            mine ? 'rounded-br-md bg-primary text-primary-foreground' : 'rounded-bl-md border bg-card text-foreground'
          )}
        >
          {m.body}
        </p>
      </div>
    </li>
  );
}
