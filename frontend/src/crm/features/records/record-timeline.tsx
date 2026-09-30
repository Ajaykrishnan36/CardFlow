import { useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  ArrowRightLeft, AtSign, CalendarDays, CheckSquare, FileText, History, Mail, MessageSquare, Phone, PlusCircle, RotateCcw, Smartphone, StickyNote, Trash2, Users
} from 'lucide-react';
import { isApiError } from '@crm/api/client';
import type { ObjectKey } from '@crm/api/types';
import type { TimelineItem } from '@crm/api/types-features';
import { Badge } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Textarea } from '@crm/components/ui/form-controls';
import { SegmentedFilter } from '@crm/components/page';
import { Skeleton } from '@crm/components/ui/spinner';
import { cn, relativeTime } from '@crm/lib/utils';
import { guessTone, recordKeys } from './use-object-meta';
import { scopedLookupHref, useRecordScope } from './record-scope';

type Kind = 'all' | 'notes' | 'emails' | 'tasks' | 'events' | 'files' | 'history';

export function timelineKey(prefix: string, object: ObjectKey, id: string, kind: string) {
  return [...recordKeys.detail(prefix, object, id), 'timeline', kind] as const;
}

const iconFor = (it: TimelineItem) => {
  if (it.kind === 'note') return StickyNote;
  if (it.kind === 'call') return Phone;
  if (it.kind === 'email') return Mail;
  if (it.kind === 'task') return CheckSquare;
  if (it.kind === 'event') return CalendarDays;
  if (it.kind === 'communication') return MessageSquare;
  if (it.kind.startsWith('file')) return FileText;
  if (it.kind === 'record.created') return PlusCircle;
  if (it.kind === 'record.converted') return ArrowRightLeft;
  if (it.kind === 'record.restored') return RotateCcw;
  if (it.kind.startsWith('app.')) return Smartphone;
  if (it.kind.startsWith('ticket.')) return MessageSquare;
  return History;
};

function show(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—';
  if (Array.isArray(v)) return v.map(show).join(', ');
  if (typeof v === 'boolean') return v ? 'Yes' : 'No';
  if (typeof v === 'object') return JSON.stringify(v);
  const s = String(v);
  if (/^\d{4}-\d{2}-\d{2}T/.test(s)) {
    const d = new Date(s);
    if (!Number.isNaN(d.getTime())) return d.toLocaleString('en-IN', { day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });
  }
  return s;
}

/** Everything that happened to a record, newest first, with a composer on top. */
export function RecordTimeline({ object, id, canWrite }: { object: ObjectKey; id: string; canWrite: boolean }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const [kind, setKind] = useState<Kind>('all');
  const q = useInfiniteQuery({
    queryKey: timelineKey(scope.prefix, object, id, kind),
    queryFn: ({ pageParam }) => scope.api.timeline(object, id, { kind: kind === 'all' ? undefined : kind, before: pageParam || undefined, limit: 30 }),
    initialPageParam: '',
    getNextPageParam: (last) => last.next || undefined
  });
  const items = q.data?.pages.flatMap((p) => p.data) ?? [];
  const del = useMutation({
    mutationFn: (itemId: string) => scope.api.deleteTimelineItem(itemId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: [...recordKeys.detail(scope.prefix, object, id), 'timeline'] }),
    onError: (e) => toast.error(isApiError(e) ? e.message : t('common.genericError'))
  });
  return (
    <div className="space-y-4 p-3 sm:p-4">
      {canWrite ? <Composer object={object} id={id} /> : null}
      <SegmentedFilter<Kind>
        value={kind}
        onChange={setKind}
        options={(['all', 'notes', 'emails', 'tasks', 'events', 'files', 'history'] as Kind[]).map((k) => ({ value: k, label: t(`timeline.filter.${k}`) }))}
      />
      {q.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-14" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <p className="rounded-lg border border-dashed px-4 py-8 text-center text-[13px] text-muted-foreground">{t('timeline.empty')}</p>
      ) : (
        <ol className="relative space-y-4 border-l pl-5">
          {items.map((it) => {
            const Icon = iconFor(it);
            const link = it.link ? scopedLookupHref(scope, it.link.object, it.link.id) : null;
            const changes = it.detail?.changes;
            const direction = it.detail?.direction as string | undefined;
            return (
              <li key={it.id} className="relative">
                <span className="absolute -left-[31px] top-0 grid size-6 place-items-center rounded-full border bg-card text-muted-foreground">
                  <Icon className="size-3.5" aria-hidden />
                </span>
                <div className="group rounded-lg border bg-card px-3 py-2">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[13px]">
                    {link ? (
                      <Link to={link} className="font-medium text-primary hover:underline">
                        {it.title}
                      </Link>
                    ) : it.kind === 'file' || it.kind === 'file.uploaded' ? (
                      <a href={scope.api.fileUrl(String(it.detail?.fileId ?? ''))} className="font-medium text-primary hover:underline">
                        {it.title}
                      </a>
                    ) : (
                      <span className="font-medium text-foreground">{it.kind === 'record.updated' ? t('timeline.updated') : it.title}</span>
                    )}
                    {it.status ? <Badge tone={guessTone(it.status)}>{it.status.replace(/_/g, ' ')}</Badge> : null}
                    {direction ? <Badge>{t(`timeline.direction.${direction}`, { defaultValue: direction })}</Badge> : null}
                    <span className="text-xs text-muted-foreground">
                      {it.actor?.label ? `${it.actor.label} · ` : ''}
                      <time dateTime={it.at} title={new Date(it.at).toLocaleString('en-IN')}>
                        {relativeTime(it.at)}
                      </time>
                    </span>
                    {it.canDelete ? (
                      <button type="button" onClick={() => del.mutate(it.id)} className="ml-auto grid size-6 place-items-center rounded text-muted-foreground opacity-0 hover:bg-muted hover:text-danger group-hover:opacity-100 focus-visible:opacity-100"
                        aria-label={t('timeline.deleteNote')}>
                        <Trash2 className="size-3.5" />
                      </button>
                    ) : null}
                  </div>
                  {it.kind === 'email' ? (
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {t('timeline.emailFromTo', { from: String(it.detail?.fromName || it.detail?.from || ''), to: show(it.detail?.to) })}
                      {it.detail?.error ? <span className="ml-1 text-danger">— {String(it.detail.error)}</span> : null}
                    </p>
                  ) : null}
                  {changes?.length ? (
                    <ul className="mt-1 space-y-0.5 text-xs">
                      {changes.map((c) => (
                        <li key={c.field} className="text-muted-foreground">
                          <span className="font-medium text-foreground">{c.label}</span>: <span className="line-through decoration-muted-foreground/40">{show(c.from)}</span> → <span className="text-foreground">{show(c.to)}</span>
                        </li>
                      ))}
                    </ul>
                  ) : null}
                  {it.body ? <p className={cn('mt-1 whitespace-pre-wrap text-[13px] text-foreground', it.kind === 'email' && 'line-clamp-6')}>{renderMentions(it.body)}</p> : null}
                </div>
              </li>
            );
          })}
        </ol>
      )}
      {q.hasNextPage ? (
        <Button variant="outline" size="sm" className="w-full" loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
          {t('timeline.more')}
        </Button>
      ) : null}
    </div>
  );
}

/** @[Name](id) → a highlighted @Name. */
function renderMentions(body: string) {
  const parts = body.split(/(@\[[^\]]+\]\([0-9a-fA-F-]{36}\))/g);
  return parts.map((p, i) => {
    const m = /^@\[([^\]]+)\]\(/.exec(p);
    return m ? (
      <span key={i} className="rounded bg-primary-soft px-1 font-medium text-primary">
        @{m[1]}
      </span>
    ) : (
      <span key={i}>{p}</span>
    );
  });
}

function Composer({ object, id }: { object: ObjectKey; id: string }) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const qc = useQueryClient();
  const [kind, setKind] = useState<'note' | 'call'>('note');
  const [body, setBody] = useState('');
  const [mention, setMention] = useState<string | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const people = useQuery({
    queryKey: ['lookup', scope.prefix, 'users', mention ?? ''],
    queryFn: () => scope.api.lookup('users', mention ?? ''),
    enabled: mention !== null
  });
  const add = useMutation({
    mutationFn: () => scope.api.addNote(object, id, { body, kind }),
    onSuccess: () => {
      setBody('');
      void qc.invalidateQueries({ queryKey: [...recordKeys.detail(scope.prefix, object, id), 'timeline'] });
      toast.success(kind === 'call' ? t('timeline.callLogged') : t('timeline.noteAdded'));
    },
    onError: (e) => toast.error(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : t('common.genericError'))
  });
  const onType = (v: string) => {
    setBody(v);
    const caret = ref.current?.selectionStart ?? v.length;
    const m = /@([\w.]{0,30})$/.exec(v.slice(0, caret));
    setMention(m ? m[1]! : null);
  };
  const pick = (name: string, pid: string) => {
    const caret = ref.current?.selectionStart ?? body.length;
    const before = body.slice(0, caret).replace(/@([\w.]{0,30})$/, `@[${name.split(' · ')[0]}](${pid}) `);
    setBody(before + body.slice(caret));
    setMention(null);
    ref.current?.focus();
  };
  const placeholder = useMemo(() => (kind === 'call' ? t('timeline.callPlaceholder') : t('timeline.notePlaceholder')), [kind, t]);
  return (
    <div className="rounded-lg border bg-muted/20 p-2.5">
      <div className="mb-2 flex gap-1">
        {(['note', 'call'] as const).map((k) => (
          <button key={k} type="button" onClick={() => setKind(k)} aria-pressed={kind === k}
            className={cn('inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium', kind === k ? 'bg-background text-foreground shadow-sm ring-1 ring-border' : 'text-muted-foreground hover:text-foreground')}>
            {k === 'note' ? <StickyNote className="size-3.5" /> : <Phone className="size-3.5" />} {t(`timeline.compose.${k}`)}
          </button>
        ))}
      </div>
      <div className="relative">
        <Textarea ref={ref} rows={3} value={body} placeholder={placeholder} onChange={(e) => onType(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && body.trim()) add.mutate();
            if (e.key === 'Escape') setMention(null);
          }} />
        {mention !== null && (people.data?.length ?? 0) > 0 ? (
          <ul className="absolute left-2 top-full z-30 mt-1 w-64 rounded-md border bg-popover p-1 shadow-pop">
            {people.data!.slice(0, 6).map((p) => (
              <li key={p.id}>
                <button type="button" onMouseDown={(e) => e.preventDefault()} onClick={() => pick(p.label, p.id)} className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[13px] hover:bg-muted">
                  <Users className="size-3.5 text-muted-foreground" /> <span className="truncate">{p.label}</span>
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      <div className="mt-2 flex items-center justify-between gap-2">
        <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
          <AtSign className="size-3" /> {t('timeline.mentionHint')}
        </span>
        <Button size="sm" disabled={!body.trim()} loading={add.isPending} onClick={() => add.mutate()}>
          {kind === 'call' ? t('timeline.logCall') : t('timeline.addNote')}
        </Button>
      </div>
    </div>
  );
}
