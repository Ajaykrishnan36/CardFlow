import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Keyboard } from 'lucide-react';
import type { NavItem } from '@crm/api/types';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { useUI } from '@crm/lib/ui-store';

/** Letters for "g then letter", matched against the last part of a nav item's path. */
const GO_KEYS: Array<[string, string[]]> = [
  ['d', ['home']],
  ['l', ['leads']],
  ['a', ['accounts']],
  ['c', ['contacts']],
  ['o', ['opportunities']],
  ['t', ['tasks']],
  ['e', ['events', 'calendar']],
  ['n', ['notes']],
  ['s', ['cases']],
  ['r', ['reports']],
  ['b', ['dashboards']],
  ['w', ['workflows']],
  ['m', ['campaigns']],
  ['p', ['me']]
];

/** True when the key press belongs to a text field, or a dialog/menu has the focus. */
export function typingTarget(e: KeyboardEvent) {
  const el = e.target;
  if (!(el instanceof HTMLElement)) return false;
  if (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) return true;
  return Boolean(el.closest('[role="dialog"], [role="menu"], [role="listbox"]'));
}

/** Plain single-key shortcut: no modifiers, not typing, no dialog open. */
export function plainKey(e: KeyboardEvent) {
  return !e.metaKey && !e.ctrlKey && !e.altKey && !typingTarget(e) && !document.querySelector('[role="dialog"]');
}

/** Global shortcuts: g + letter to go to a page, / to search, ? for the list. */
export function Shortcuts({ navigation }: { navigation: NavItem[] }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const setCommandOpen = useUI((s) => s.setCommandOpen);
  const [help, setHelp] = useState(false);
  const pending = useRef(0);

  const targets = useMemo(() => {
    const out: Array<{ key: string; item: NavItem }> = [];
    for (const [key, names] of GO_KEYS) {
      const item = navigation.find((n) => n.available && names.includes(n.path.split('/').filter(Boolean).pop() ?? ''));
      if (item) out.push({ key, item });
    }
    return out;
  }, [navigation]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!plainKey(e)) return;
      if (pending.current && Date.now() - pending.current < 1500) {
        pending.current = 0;
        const hit = targets.find((x) => x.key === e.key.toLowerCase());
        if (hit) {
          e.preventDefault();
          navigate(hit.item.path);
        }
        return;
      }
      if (e.key === 'g') {
        pending.current = Date.now();
      } else if (e.key === '/') {
        e.preventDefault();
        setCommandOpen(true);
      } else if (e.key === '?') {
        e.preventDefault();
        setHelp(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [targets, navigate, setCommandOpen]);

  const rows: Array<[string[], string]> = [
    [['⌘', 'K'], t('shortcuts.search')],
    [['/'], t('shortcuts.search')],
    [['?'], t('shortcuts.help')],
    ...targets.map((x): [string[], string] => [['g', x.key], t('shortcuts.goTo', { page: x.item.label })]),
    [['j'], t('shortcuts.nextRecord')],
    [['k'], t('shortcuts.previousRecord')],
    [['e'], t('shortcuts.editRecord')],
    [['⌘', 'S'], t('shortcuts.save')],
    [['Esc'], t('shortcuts.cancel')]
  ];

  return (
    <Dialog open={help} onOpenChange={setHelp}>
      <DialogContent className="max-w-md p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Keyboard className="size-4 text-primary" /> {t('shortcuts.title')}
          </DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('shortcuts.subtitle')}</DialogDescription>
        </div>
        <ul className="max-h-[60vh] divide-y overflow-y-auto px-5 py-2">
          {rows.map(([keys, label], i) => (
            <li key={i} className="flex items-center justify-between gap-3 py-2 text-[13px]">
              <span>{label}</span>
              <span className="flex items-center gap-1">
                {keys.map((k, j) => (
                  <kbd key={j} className="min-w-6 rounded border bg-muted px-1.5 py-0.5 text-center font-mono text-[11px] text-muted-foreground">
                    {k}
                  </kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  );
}

// ---- next / previous record ----

const navKey = (base: string) => `crm.recordNav.${base}`;

/** The list page remembers the rows it shows, so a record page can step through them. */
export function rememberRecordList(base: string, ids: string[], listUrl: string) {
  try {
    sessionStorage.setItem(navKey(base), JSON.stringify({ ids, listUrl }));
  } catch {
    /* storage unavailable */
  }
}

export function recordNeighbours(base: string, id: string): { prev?: string; next?: string; index: number; total: number; listUrl?: string } {
  try {
    const raw = sessionStorage.getItem(navKey(base));
    if (!raw) return { index: -1, total: 0 };
    const { ids, listUrl } = JSON.parse(raw) as { ids: string[]; listUrl?: string };
    const i = ids.indexOf(id);
    if (i < 0) return { index: -1, total: ids.length };
    return { prev: ids[i - 1], next: ids[i + 1], index: i, total: ids.length, listUrl };
  } catch {
    return { index: -1, total: 0 };
  }
}
