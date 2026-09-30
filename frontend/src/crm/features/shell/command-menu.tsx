import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { recordsApiFor } from '@crm/api/endpoints';
import { ArrowLeftRight, CornerDownLeft, LogOut, Search, UserRound } from 'lucide-react';
import type { NavItem } from '@crm/api/types';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { useSignOut, workspaceHomePath } from '@crm/auth/session';
import { useUI } from '@crm/lib/ui-store';
import { cn } from '@crm/lib/utils';
import { navIcon } from './nav-icons';

interface Command {
  id: string;
  label: string;
  hint?: string;
  icon: React.ComponentType<{ className?: string }>;
  run: () => void;
}

/** ⌘K / Ctrl+K palette (PRD §10.4): jump to any page the user can see, or run a command. */
export function CommandMenu({
  navigation,
  workspaces,
  currentWorkspace,
  apiPrefix,
  routeBase
}: {
  /** Record search runs against this CRM ('/platform' or '/w/<code>'). */
  apiPrefix?: string | null;
  routeBase?: string;
  navigation: NavItem[];
  /** Workspace app only: other workspaces to switch to. */
  workspaces?: Array<{ code: string; name: string }>;
  currentWorkspace?: string;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const open = useUI((s) => s.commandOpen);
  const setOpen = useUI((s) => s.setCommandOpen);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLUListElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen(!useUI.getState().commandOpen);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [setOpen]);

  useEffect(() => {
    if (open) {
      setQuery('');
      setActive(0);
    }
  }, [open]);

  const commands = useMemo<Command[]>(() => {
    const pages: Command[] = navigation.map((n) => ({
      id: `nav:${n.key}`,
      label: n.label,
      hint: n.available ? n.group : t('common.soon'),
      icon: navIcon(n.icon),
      run: () => navigate(n.path)
    }));
    const switches: Command[] = (workspaces ?? [])
      .filter((w) => w.code !== currentWorkspace)
      .map((w) => ({
        id: `ws:${w.code}`,
        label: t('workspaceApp.shell.switchTo', { name: w.name }),
        hint: t('workspaceApp.shell.switchWorkspace'),
        icon: ArrowLeftRight,
        run: () => navigate(workspaceHomePath(w.code))
      }));
    return [
      ...pages,
      ...switches,
      { id: 'profile', label: t('shell.profile'), icon: UserRound, run: () => navigate('/crm/me') },
      { id: 'signout', label: t('common.signOut'), icon: LogOut, run: () => void signOut() }
    ];
  }, [navigation, workspaces, currentWorkspace, navigate, signOut, t]);

  // Records: search every object the viewer can read (debounced).
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const h = setTimeout(() => setDebounced(query.trim()), 200);
    return () => clearTimeout(h);
  }, [query]);
  const recordsQ = useQuery({
    queryKey: ['search', apiPrefix, debounced],
    queryFn: () => recordsApiFor(apiPrefix!).search(debounced, 5),
    enabled: open && Boolean(apiPrefix) && debounced.length >= 2,
    staleTime: 15_000
  });
  const recordCommands = useMemo<Command[]>(
    () =>
      (recordsQ.data ?? []).flatMap((g) =>
        g.rows.map((r) => ({
          id: `rec:${g.object}:${r.id}`,
          label: r.title || r.code || '—',
          hint: [g.label, r.subtitle].filter(Boolean).join(' · '),
          icon: navIcon(g.icon ?? (g.object === 'leads' ? 'user-plus' : g.object === 'accounts' ? 'briefcase' : g.object === 'contacts' ? 'contact' : 'box')),
          run: () => navigate(`${routeBase}/${g.object}/${encodeURIComponent(r.id)}`)
        }))
      ),
    [recordsQ.data, routeBase, navigate]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const pages = q ? commands.filter((c) => c.label.toLowerCase().includes(q) || c.hint?.toLowerCase().includes(q)) : commands;
    return q.length >= 2 ? [...recordCommands, ...pages] : pages;
  }, [commands, query, recordCommands]);

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);

  const run = (c: Command | undefined) => {
    if (!c) return;
    setOpen(false);
    c.run();
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent hideClose className="max-w-xl overflow-hidden p-0">
        <DialogTitle className="sr-only">{t('shell.search')}</DialogTitle>
        <DialogDescription className="sr-only">{t('shell.commandPlaceholder')}</DialogDescription>
        <div className="flex items-center gap-2.5 border-b px-4">
          <Search className="size-4 text-muted-foreground" aria-hidden />
          <input
            autoFocus
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                setActive((a) => Math.min(a + 1, filtered.length - 1));
              } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                setActive((a) => Math.max(a - 1, 0));
              } else if (e.key === 'Enter') {
                e.preventDefault();
                run(filtered[active]);
              }
            }}
            placeholder={t('shell.commandPlaceholder')}
            className="h-12 flex-1 bg-transparent text-[15px] outline-none placeholder:text-muted-foreground/70"
            role="combobox"
            aria-expanded
            aria-controls="crm-command-list"
            aria-activedescendant={filtered[active] ? `cmd-${filtered[active].id}` : undefined}
          />
          <kbd className="rounded border bg-muted px-1.5 text-[11px] text-muted-foreground">Esc</kbd>
        </div>
        <ul ref={listRef} id="crm-command-list" role="listbox" className="crm-scroll max-h-[min(60vh,380px)] overflow-y-auto p-2">
          {filtered.length === 0 ? (
            <li className="px-3 py-8 text-center text-[13px] text-muted-foreground">{recordsQ.isFetching ? t('shell.searching') : t('shell.commandEmpty')}</li>
          ) : (
            filtered.map((c, i) => {
              const Icon = c.icon;
              return (
                <li
                  key={c.id}
                  id={`cmd-${c.id}`}
                  data-index={i}
                  role="option"
                  aria-selected={i === active}
                  onMouseMove={() => setActive(i)}
                  onClick={() => run(c)}
                  className={cn(
                    'flex cursor-pointer items-center gap-3 rounded-md px-3 py-2 text-[13px]',
                    i === active ? 'bg-primary-soft text-foreground' : 'text-foreground/85'
                  )}
                >
                  <Icon className={cn('size-4', i === active ? 'text-primary' : 'text-muted-foreground')} />
                  <span className="flex-1">{c.label}</span>
                  {c.hint ? <span className="text-xs text-muted-foreground">{c.hint}</span> : null}
                  {i === active ? <CornerDownLeft className="size-3.5 text-muted-foreground" aria-hidden /> : null}
                </li>
              );
            })
          )}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
