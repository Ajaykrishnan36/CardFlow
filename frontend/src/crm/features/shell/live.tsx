import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Bell, CheckCheck, Star } from 'lucide-react';
import { recordsApiFor } from '@crm/api/endpoints';
import type { AppNotification } from '@crm/api/types-features';
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

export function NotificationsBell({ prefix }: { prefix: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const api = recordsApiFor(prefix);
  const key = ['notifications', prefix];
  const q = useQuery({ queryKey: key, queryFn: () => api.notifications(), refetchInterval: 5 * 60_000 });
  const read = useMutation({
    mutationFn: (body: { ids?: number[]; all?: boolean }) => api.readNotifications(body),
    onSuccess: (data) => qc.setQueryData(key, data)
  });
  const unread = q.data?.unread ?? 0;
  const items = q.data?.data ?? [];
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      align="end"
      width={380}
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
      <div className="flex items-center justify-between border-b px-3 py-2">
        <p className="text-[13px] font-semibold">{t('notifications.title')}</p>
        {unread ? (
          <Button variant="link" size="sm" onClick={() => read.mutate({ all: true })}>
            <CheckCheck className="size-3.5" /> {t('notifications.readAll')}
          </Button>
        ) : null}
      </div>
      {items.length === 0 ? (
        <p className="px-4 py-10 text-center text-[13px] text-muted-foreground">{t('notifications.empty')}</p>
      ) : (
        <ul className="max-h-[60vh] divide-y overflow-y-auto">
          {items.map((n) => (
            <li key={n.id}>
              <button
                type="button"
                onClick={() => {
                  if (!n.readAt) read.mutate({ ids: [n.id] });
                  setOpen(false);
                  if (n.link) navigate(n.link);
                }}
                className={cn('flex w-full gap-2.5 px-3 py-2.5 text-left hover:bg-muted/60', !n.readAt && 'bg-primary-soft/40')}
              >
                <span className={cn('mt-1.5 size-2 shrink-0 rounded-full', n.readAt ? 'bg-transparent' : 'bg-primary')} aria-hidden />
                <span className="min-w-0 flex-1">
                  <span className="block text-[13px] font-medium text-foreground">{n.title}</span>
                  {n.body ? <span className="line-clamp-2 block text-xs text-muted-foreground">{n.body}</span> : null}
                  <span className="mt-0.5 block text-[11px] text-muted-foreground">{relativeTime(n.createdAt)}</span>
                </span>
              </button>
            </li>
          ))}
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
