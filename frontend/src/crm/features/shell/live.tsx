import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { BadgeIndianRupee, Bell, Check, CheckCheck, CreditCard, Star, Trash2, UserPlus, Wrench, X } from 'lucide-react';
import { recordsApiFor } from '@crm/api/endpoints';
import type { AppNotification, NotificationFilter, NotificationInbox } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Popover } from '@crm/components/ui/popover';
import { cn, relativeTime } from '@crm/lib/utils';
import { navIcon } from './nav-icons';

/** Keeps pages current: the server says what changed, pages refetch what they show. */
export function useLiveUpdates(prefix: string | null) {
  const qc = useQueryClient();
  const pending = useRef(new Set<string>());
  const timer = useRef<number | undefined>();
  useEffect(() => {
    if (!prefix || typeof EventSource === 'undefined') return;
    const es = new EventSource(recordsApiFor(prefix).streamUrl(), { withCredentials: true });
    const flush = () => {
      for (const object of pending.current) void qc.invalidateQueries({ queryKey: ['records', prefix, object] });
      if (pending.current.size) {
        void qc.invalidateQueries({ queryKey: ['workspace'] });
        void qc.invalidateQueries({ queryKey: ['platform', 'dashboard'] });
      }
      pending.current.clear();
    };
    es.addEventListener('record', (e) => {
      try {
        const d = JSON.parse((e as MessageEvent).data) as { object: string };
        pending.current.add(d.object);
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(flush, 400);
      } catch {
        /* ignore */
      }
    });
    es.addEventListener('notification', (e) => {
      void qc.invalidateQueries({ queryKey: ['notifications', prefix] });
      try {
        const n = JSON.parse((e as MessageEvent).data) as AppNotification;
        toast(n.title, { description: n.body || undefined });
      } catch {
        /* ignore */
      }
    });
    return () => {
      window.clearTimeout(timer.current);
      es.close();
    };
  }, [prefix, qc]);
}

type InboxTab = 'today' | 'unread' | 'all';

/** What a notification is about, for its icon. */
function noticeIcon(kind: string) {
  if (kind.startsWith('billing')) return CreditCard;
  if (kind.startsWith('member')) return UserPlus;
  if (kind.startsWith('sale')) return BadgeIndianRupee;
  if (kind === 'work.booked' || kind === 'assigned') return Wrench;
  return Bell;
}

export function NotificationsBell({ prefix }: { prefix: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<InboxTab>('today');
  const api = recordsApiFor(prefix);
  const filter: NotificationFilter = tab === 'today' ? { today: '1' } : tab === 'unread' ? { unread: '1' } : {};
  const key = ['notifications', prefix, tab];
  // Counts for the badge come from any tab's answer; the list is the open tab's.
  const q = useQuery({ queryKey: key, queryFn: () => api.notifications(filter), refetchInterval: 60_000 });
  const done = (data: NotificationInbox) => {
    qc.setQueryData(key, data);
    void qc.invalidateQueries({ queryKey: ['notifications', prefix], predicate: (x) => x.queryKey[2] !== tab });
  };
  const read = useMutation({ mutationFn: (body: { ids?: number[]; all?: boolean }) => api.readNotifications(body, filter), onSuccess: done });
  const clear = useMutation({ mutationFn: (body: { ids?: number[]; all?: boolean }) => api.clearNotifications(body, filter), onSuccess: done });
  const unread = q.data?.unread ?? 0;
  const items = q.data?.data ?? [];
  const tabs: Array<{ value: InboxTab; label: string; count?: number }> = [
    { value: 'today', label: t('notifications.today'), count: q.data?.today },
    { value: 'unread', label: t('notifications.unread'), count: unread },
    { value: 'all', label: t('notifications.all'), count: q.data?.total }
  ];
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="end"
      width={400}
      className="p-0"
      trigger={(p) => (
        <Button variant="ghost" size="icon-sm" ref={p.ref as never} onClick={p.onClick} aria-expanded={p['aria-expanded']} aria-label={t('notifications.open', { count: unread })} className="relative">
          <Bell />
          {unread ? (
            <span className="absolute -right-0.5 -top-0.5 grid min-w-4 place-items-center rounded-full bg-danger px-1 text-[10px] font-semibold leading-4 text-white">{unread > 99 ? '99+' : unread}</span>
          ) : null}
        </Button>
      )}
    >
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <p className="text-[13px] font-semibold">{t('notifications.title')}</p>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" disabled={!unread || read.isPending} onClick={() => read.mutate({ all: true })}>
            <CheckCheck className="size-3.5" /> {t('notifications.readAll')}
          </Button>
          <Button variant="ghost" size="sm" disabled={!q.data?.total || clear.isPending} onClick={() => clear.mutate({ all: true })}>
            <Trash2 className="size-3.5" /> {t('notifications.clearAll')}
          </Button>
        </div>
      </div>
      <div role="tablist" className="flex gap-1 border-b px-2 py-1.5">
        {tabs.map((x) => (
          <button
            key={x.value}
            type="button"
            role="tab"
            aria-selected={tab === x.value}
            onClick={() => setTab(x.value)}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground',
              tab === x.value && 'bg-primary-soft text-primary hover:bg-primary-soft hover:text-primary'
            )}
          >
            {x.label}
            {x.count ? <span className="rounded-full bg-background px-1.5 text-[10px] leading-4 text-foreground/70 ring-1 ring-border">{x.count}</span> : null}
          </button>
        ))}
      </div>
      {q.isLoading ? (
        <p className="px-4 py-10 text-center text-[13px] text-muted-foreground">{t('common.loading', { defaultValue: 'Loading…' })}</p>
      ) : items.length === 0 ? (
        <p className="px-4 py-10 text-center text-[13px] text-muted-foreground">{t(tab === 'today' ? 'notifications.emptyToday' : tab === 'unread' ? 'notifications.emptyUnread' : 'notifications.empty')}</p>
      ) : (
        <ul className="max-h-[60vh] divide-y overflow-y-auto">
          {items.map((n) => {
            const Icon = noticeIcon(n.kind);
            return (
              <li key={n.id} className={cn('group relative flex gap-2.5 px-3 py-2.5 hover:bg-muted/60', !n.readAt && 'bg-primary-soft/40')}>
                <span className={cn('mt-0.5 grid size-7 shrink-0 place-items-center rounded-full', n.readAt ? 'bg-muted text-muted-foreground' : 'bg-primary-soft text-primary')} aria-hidden>
                  <Icon className="size-3.5" />
                </span>
                <button
                  type="button"
                  onClick={() => {
                    if (!n.readAt) read.mutate({ ids: [n.id] });
                    setOpen(false);
                    if (n.link) navigate(n.link);
                  }}
                  className="min-w-0 flex-1 pr-14 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <span className={cn('block text-[13px] text-foreground', !n.readAt && 'font-semibold')}>{n.title}</span>
                  {n.body ? <span className="line-clamp-2 block text-xs text-muted-foreground">{n.body}</span> : null}
                  <span className="mt-0.5 block text-[11px] text-muted-foreground">{relativeTime(n.createdAt)}</span>
                </button>
                <span className="absolute right-2 top-2 flex gap-0.5">
                  {!n.readAt ? (
                    <Button variant="ghost" size="icon-sm" aria-label={t('notifications.markRead')} title={t('notifications.markRead')} onClick={() => read.mutate({ ids: [n.id] })}>
                      <Check className="size-3.5" />
                    </Button>
                  ) : null}
                  <Button variant="ghost" size="icon-sm" aria-label={t('notifications.clear')} title={t('notifications.clear')} onClick={() => clear.mutate({ ids: [n.id] })}>
                    <X className="size-3.5" />
                  </Button>
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </Popover>
  );
}

/** The viewer's pinned records and views, in the sidebar. */
export function FavoritesNav({ prefix, collapsed, onNavigate }: { prefix: string; collapsed: boolean; onNavigate?: () => void }) {
  const { t } = useTranslation();
  const q = useQuery({ queryKey: ['favorites', prefix], queryFn: () => recordsApiFor(prefix).favorites(), staleTime: 60_000 });
  const list = q.data ?? [];
  if (!list.length || collapsed) return null;
  return (
    <div className="mb-3">
      <p className="mb-1 flex items-center gap-1.5 px-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/80">
        <Star className="size-3" aria-hidden /> {t('shell.favorites')}
      </p>
      <ul className="space-y-0.5">
        {list.map((f) => {
          const Icon = navIcon(f.icon ?? 'star');
          return (
            <li key={f.id}>
              <NavLink
                to={f.path}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn('flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] text-foreground/80 hover:bg-muted hover:text-foreground', isActive && 'bg-primary-soft font-medium text-primary')
                }
              >
                <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                <span className="truncate">{f.label}</span>
              </NavLink>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
