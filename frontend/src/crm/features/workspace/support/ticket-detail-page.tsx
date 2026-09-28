import { useState, type FormEvent, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Building2, Contact, Headset, LifeBuoy, Lock, Phone, Send, Smartphone } from 'lucide-react';
import { workspaceSupportApi } from '@crm/api/endpoints';
import { isApiError } from '@crm/api/client';
import type { SupportTicket, TicketStatus } from '@crm/api/types';
import { Alert, Card, CardHeader } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Field } from '@crm/components/ui/field';
import { Select, Textarea } from '@crm/components/ui/form-controls';
import { Skeleton } from '@crm/components/ui/spinner';
import { Breadcrumbs, PageContainer } from '@crm/components/page';
import { EmptyState, ErrorState } from '@crm/components/states';
import { cn, relativeTime } from '@crm/lib/utils';
import { useDocumentTitle } from '@crm/features/auth/login-pages';
import { Avatar } from '@crm/features/shell/user-menu';
import { scopedLookupHref, useRecordScope } from '@crm/features/records/record-scope';
import { RecordNoAccess } from '@crm/features/records/record-states';
import { ticketAccess, useWorkspace } from '../workspace-context';
import { CategoryChip, SUPPORT_REFRESH_MS, supportKeys, supportPath, TICKET_STATUSES, ticketNumber, TicketStatusBadge } from './support-utils';

/** /crm/w/:ws/support/:id — conversation, reply composer (ticket update) and the customer's CRM records. */
export function TicketDetailPage() {
  const { context } = useWorkspace();
  const { id = '' } = useParams();
  if (!ticketAccess(context).read) return <RecordNoAccess />;
  // Keyed so a draft reply never carries over to another ticket.
  return <TicketDetailView key={id} id={id} />;
}

function fullTime(iso: string) {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('en-IN', { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function TicketDetailView({ id }: { id: string }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { code, context } = useWorkspace();
  const canReply = ticketAccess(context).update;
  const q = useQuery({
    queryKey: supportKeys.ticket(code, id),
    queryFn: () => workspaceSupportApi(code).get(id),
    refetchInterval: SUPPORT_REFRESH_MS
  });
  const tk = q.data;
  useDocumentTitle(tk ? `${tk.subject} · ${ticketNumber(tk.id)}` : t('workspaceApp.support.title'));

  const [reply, setReply] = useState('');
  const [status, setStatus] = useState<TicketStatus | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const nextStatus = status ?? tk?.status ?? 'open';
  const hasReply = reply.trim().length > 0;
  const statusChanged = Boolean(tk && nextStatus !== tk.status);

  const update = useMutation({
    mutationFn: (body: { status?: TicketStatus; reply?: string }) => workspaceSupportApi(code).update(id, body),
    onSuccess: (updated, body) => {
      qc.setQueryData<SupportTicket>(supportKeys.ticket(code, id), updated);
      void qc.invalidateQueries({ queryKey: supportKeys.all(code) });
      void qc.invalidateQueries({ queryKey: ['workspace', code, 'dashboard'] });
      if (body.reply) toast.success(t('workspaceApp.support.replySent', { name: updated.user.name || t('workspaceApp.support.customer') }));
      else toast.success(t('workspaceApp.support.statusSaved', { status: t(`workspaceApp.support.status.${updated.status}`) }));
      setReply('');
      setStatus(null);
      setFormError(null);
    },
    onError: (e) => setFormError(isApiError(e) ? (e.status === 403 ? t('workspaceApp.support.forbidden') : e.message) : t('common.genericError'))
  });

  const submit = () => {
    if (!tk || update.isPending) return;
    if (!hasReply && !statusChanged) return;
    const body: { status?: TicketStatus; reply?: string } = {};
    if (hasReply) body.reply = reply.trim();
    if (statusChanged) body.status = nextStatus;
    update.mutate(body);
  };
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    submit();
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      submit();
    }
  };

  const listPath = supportPath(code);

  if (q.isError) {
    if (isApiError(q.error) && q.error.status === 403) return <RecordNoAccess />;
    const notFound = isApiError(q.error) && q.error.status === 404;
    return (
      <PageContainer wide>
        <Card>
          {notFound ? (
            <EmptyState
              icon={LifeBuoy}
              title={t('workspaceApp.support.notFoundTitle')}
              body={t('workspaceApp.support.notFoundBody')}
              action={
                <Button asChild variant="outline">
                  <Link to={listPath}>{t('workspaceApp.support.backToList')}</Link>
                </Button>
              }
            />
          ) : (
            <ErrorState
              title={t('workspaceApp.support.errorTitle')}
              message={isApiError(q.error) ? q.error.message : undefined}
              requestId={isApiError(q.error) ? q.error.requestId : undefined}
              onRetry={() => void q.refetch()}
            />
          )}
        </Card>
      </PageContainer>
    );
  }

  if (!tk) return <DetailSkeleton />;

  const customer = tk.user.name || t('workspaceApp.support.customer');

  return (
    <PageContainer wide>
      <Breadcrumbs items={[{ label: t('workspaceApp.support.title'), to: listPath }, { label: ticketNumber(tk.id) }]} />

      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="min-w-0 break-words text-xl font-semibold tracking-tight text-foreground sm:text-[22px]">{tk.subject || t('workspaceApp.support.noSubject')}</h1>
            <TicketStatusBadge status={tk.status} />
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            <CategoryChip category={tk.category} />
            <span className="font-mono">{ticketNumber(tk.id)}</span>
            <span aria-hidden>·</span>
            <time dateTime={tk.createdAt} title={fullTime(tk.createdAt)}>
              {t('workspaceApp.support.opened', { time: relativeTime(tk.createdAt) })}
            </time>
            {tk.source ? (
              <>
                <span aria-hidden>·</span>
                <span className="inline-flex items-center gap-1">
                  <Smartphone className="size-3" aria-hidden />
                  {tk.source}
                </span>
              </>
            ) : null}
          </div>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="min-w-0 space-y-4 lg:col-span-2">
          <Card className="overflow-hidden">
            <CardHeader className="px-4 py-3" title={<span className="text-[13px]">{t('workspaceApp.support.conversation')}</span>} />
            <ol className="space-y-4 px-4 py-4 sm:px-5" aria-label={t('workspaceApp.support.conversation')}>
              <Bubble side="customer" name={customer} meta={tk.user.role} time={tk.createdAt} text={tk.message} />
              {tk.reply ? (
                <Bubble side="agent" name={t('workspaceApp.support.supportTeam')} time={tk.repliedAt ?? tk.updatedAt} text={tk.reply} />
              ) : (
                <li className="text-center text-xs text-muted-foreground">{t('workspaceApp.support.awaitingReply')}</li>
              )}
            </ol>
          </Card>

          {canReply ? (
            <Card className="overflow-hidden">
              <form onSubmit={onSubmit} noValidate>
                <div className="space-y-3 p-4 sm:p-5">
                  {formError ? <Alert tone="danger">{formError}</Alert> : null}
                  <Field label={tk.reply ? t('workspaceApp.support.replaceReply') : t('workspaceApp.support.replyLabel')} hint={t('workspaceApp.support.replyHint', { name: customer })}>
                    <Textarea
                      rows={4}
                      value={reply}
                      onChange={(e) => setReply(e.target.value)}
                      onKeyDown={onKeyDown}
                      disabled={update.isPending}
                      placeholder={t('workspaceApp.support.replyPlaceholder')}
                      maxLength={4000}
                    />
                  </Field>
                </div>
                <div className="flex flex-col gap-2 border-t bg-muted/30 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
                  <label className="flex items-center gap-2 text-[13px] text-foreground">
                    <span className="shrink-0 text-muted-foreground">{t('workspaceApp.support.statusLabel')}</span>
                    <Select
                      className="w-40"
                      value={nextStatus}
                      onChange={(e) => setStatus(e.target.value as TicketStatus)}
                      disabled={update.isPending}
                      options={TICKET_STATUSES.map((s) => ({ value: s, label: t(`workspaceApp.support.status.${s}`) }))}
                    />
                  </label>
                  <div className="flex items-center gap-2">
                    <span className="hidden text-xs text-muted-foreground md:inline">{t('workspaceApp.support.shortcut')}</span>
                    {!hasReply && statusChanged ? (
                      <Button type="submit" loading={update.isPending}>
                        {t('workspaceApp.support.saveStatus')}
                      </Button>
                    ) : (
                      <Button type="submit" loading={update.isPending} disabled={!hasReply}>
                        <Send /> {t('workspaceApp.support.send')}
                      </Button>
                    )}
                  </div>
                </div>
              </form>
            </Card>
          ) : (
            <p className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-3 text-[13px] text-muted-foreground">
              <Lock className="size-4 shrink-0" aria-hidden />
              {t('workspaceApp.support.readOnly')}
            </p>
          )}
        </div>

        <aside className="min-w-0 space-y-4">
          <CustomerCard ticket={tk} />
        </aside>
      </div>
    </PageContainer>
  );
}

function Bubble({ side, name, meta, time, text }: { side: 'customer' | 'agent'; name: string; meta?: string; time: string; text: string }) {
  const agent = side === 'agent';
  return (
    <li className={cn('flex items-end gap-2.5', agent && 'flex-row-reverse')}>
      {agent ? (
        <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-primary-foreground" aria-hidden>
          <Headset className="size-4" />
        </span>
      ) : (
        <Avatar name={name} className="size-8" />
      )}
      <div className={cn('min-w-0 max-w-[85%] sm:max-w-[75%]', agent && 'text-right')}>
        <p className="mb-1 truncate text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{name}</span>
          {meta ? ` · ${meta}` : ''} ·{' '}
          <time dateTime={time} title={fullTime(time)}>
            {relativeTime(time)}
          </time>
        </p>
        <div
          className={cn(
            'whitespace-pre-wrap break-words rounded-2xl px-3.5 py-2.5 text-left text-[13px] leading-5',
            agent ? 'rounded-br-md bg-primary-soft text-foreground ring-1 ring-primary/15' : 'rounded-bl-md bg-muted text-foreground'
          )}
        >
          {text}
        </div>
      </div>
    </li>
  );
}

function CustomerCard({ ticket: tk }: { ticket: SupportTicket }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const links = [
    tk.account ? { icon: Building2, label: t('workspaceApp.support.account'), value: tk.account, href: scopedLookupHref(scope, 'accounts', tk.account.id) } : null,
    tk.contact ? { icon: Contact, label: t('workspaceApp.support.contact'), value: tk.contact, href: scopedLookupHref(scope, 'contacts', tk.contact.id) } : null
  ].filter(Boolean) as Array<{ icon: typeof Building2; label: string; value: { id: string; label: string }; href: string | null }>;

  return (
    <Card>
      <CardHeader className="px-4 py-3" title={<span className="text-[13px]">{t('workspaceApp.support.customerCard')}</span>} />
      <div className="space-y-3 px-4 py-3 text-[13px]">
        <div className="flex items-center gap-2.5">
          <Avatar name={tk.user.name || '?'} className="size-9" />
          <div className="min-w-0">
            <p className="truncate font-medium text-foreground">{tk.user.name || '—'}</p>
            {tk.user.role ? <p className="truncate text-xs text-muted-foreground">{tk.user.role}</p> : null}
          </div>
        </div>
        {tk.user.phone ? (
          <a href={`tel:${tk.user.phone.replace(/\s+/g, '')}`} className="flex items-center gap-2 font-medium tabular-nums text-primary hover:underline">
            <Phone className="size-3.5" aria-hidden />
            {tk.user.phone}
          </a>
        ) : null}
        {links.length ? (
          <ul className="divide-y rounded-lg border">
            {links.map((l) => {
              const Icon = l.icon;
              const body = (
                <>
                  <span className="grid size-7 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground">
                    <Icon className="size-3.5" aria-hidden />
                  </span>
                  <span className="min-w-0">
                    <span className="block text-[11px] text-muted-foreground">{l.label}</span>
                    <span className={cn('block truncate font-medium', l.href ? 'text-primary' : 'text-foreground')}>{l.value.label}</span>
                  </span>
                </>
              );
              return (
                <li key={l.label}>
                  {l.href ? (
                    <Link to={l.href} className="flex items-center gap-2.5 px-3 py-2 hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none">
                      {body}
                    </Link>
                  ) : (
                    <div className="flex items-center gap-2.5 px-3 py-2">{body}</div>
                  )}
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="text-xs text-muted-foreground">{t('workspaceApp.support.noRecords')}</p>
        )}
      </div>
    </Card>
  );
}

function DetailSkeleton() {
  return (
    <PageContainer wide>
      <Skeleton className="mb-3 h-4 w-40" />
      <Skeleton className="mb-2 h-7 w-80" />
      <Skeleton className="mb-5 h-4 w-56" />
      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="space-y-4 p-5 lg:col-span-2">
          <Skeleton className="h-16 w-3/4" />
          <Skeleton className="ml-auto h-12 w-2/3" />
          <Skeleton className="h-24" />
        </Card>
        <Card className="space-y-3 p-4">
          <Skeleton className="h-9 w-40" />
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-12" />
        </Card>
      </div>
    </PageContainer>
  );
}
