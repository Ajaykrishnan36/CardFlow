import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { ArrowDown, ArrowUp, Eye, EyeOff, LayoutTemplate, Lock, Monitor, RotateCcw, Smartphone } from 'lucide-react';
import { api, isApiError } from '@crm/api/client';
import { Alert } from '@crm/components/ui/card';
import { Button } from '@crm/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@crm/components/ui/menu';
import { cn } from '@crm/lib/utils';
import { arrange, type LayoutItem, type UILayout } from '../../../navigation/layout-items';

// Arranging the dashboard and the menu (D-130): order and show/hide, saved for the whole
// business (or for the owner console), separately for desktop and for phones.

export type Device = 'desktop' | 'mobile';
export type Surface = 'dashboard' | 'nav';

interface LayoutsResponse {
  layouts: Record<Surface, Partial<Record<Device, UILayout>>>;
  canEdit: boolean;
}

const layoutKey = (prefix: string) => ['ui-layout', prefix] as const;

export function useUiLayouts(prefix: string | null | undefined) {
  return useQuery({ queryKey: layoutKey(prefix ?? ''), queryFn: () => api<LayoutsResponse>(`${prefix}/ui-layout`), enabled: Boolean(prefix), staleTime: 5 * 60_000 });
}

/** Which arrangement this screen uses: the phone one under 768px, else the desktop one. */
export function useDevice(): Device {
  const query = '(max-width: 767px)';
  const [mobile, setMobile] = useState(() => typeof window !== 'undefined' && window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const on = () => setMobile(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return mobile ? 'mobile' : 'desktop';
}

/** The saved arrangement of a surface for the current screen size, and whether the viewer may change it. */
export function useLayout(prefix: string | null | undefined, surface: Surface) {
  const device = useDevice();
  const q = useUiLayouts(prefix);
  return { layout: q.data?.layouts[surface]?.[device] ?? null, canEdit: q.data?.canEdit ?? false, device };
}

export interface LayoutGroup {
  title: string;
  hint?: string;
  items: LayoutItem[];
  /** Keys that can be moved but not hidden. */
  locked?: string[];
}

interface Draft {
  order: Record<string, string[]>; // group title → keys in order
  hidden: Set<string>;
}

function draftFrom(groups: LayoutGroup[], layout?: UILayout | null): Draft {
  const order: Record<string, string[]> = {};
  for (const g of groups) order[g.title] = arrange(g.items, (i) => i.key, layout ? { order: layout.order, hidden: [] } : null).map((i) => i.key);
  return { order, hidden: new Set(layout?.hidden ?? []) };
}

export function LayoutEditor({ prefix, surface, title, groups, onClose }: {
  prefix: string; surface: Surface; title: string; groups: (device: Device) => LayoutGroup[]; onClose: () => void;
}) {
  const qc = useQueryClient();
  const q = useUiLayouts(prefix);
  const current = useDevice();
  const [device, setDevice] = useState<Device>(current);
  const [drafts, setDrafts] = useState<Partial<Record<Device, Draft>>>({});
  const [error, setError] = useState<string | null>(null);
  const list = useMemo(() => groups(device), [groups, device]);
  const saved = q.data?.layouts[surface]?.[device];
  const draft = drafts[device] ?? draftFrom(list, saved);
  // Each change starts from the latest draft, so two quick clicks never lose one of them.
  const change = (fn: (d: Draft) => Draft) => setDrafts((all) => ({ ...all, [device]: fn(all[device] ?? draftFrom(list, saved)) }));
  const labels = new Map(list.flatMap((g) => g.items.map((i) => [i.key, i.label] as const)));

  const move = (group: string, key: string, by: number) =>
    change((d) => {
      const keys = [...(d.order[group] ?? [])];
      const i = keys.indexOf(key);
      const j = i + by;
      if (i < 0 || j < 0 || j >= keys.length) return d;
      [keys[i], keys[j]] = [keys[j]!, keys[i]!];
      return { ...d, order: { ...d.order, [group]: keys } };
    });
  const toggle = (key: string) =>
    change((d) => {
      const hidden = new Set(d.hidden);
      if (hidden.has(key)) hidden.delete(key);
      else hidden.add(key);
      return { ...d, hidden };
    });

  const save = useMutation({
    mutationFn: async () => {
      // Both tabs are saved: whatever was changed for desktop and for phones.
      for (const d of ['desktop', 'mobile'] as Device[]) {
        const x = drafts[d];
        if (!x) continue;
        const known = new Set(groups(d).flatMap((g) => g.items.map((i) => i.key)));
        await api(`${prefix}/ui-layout`, { method: 'PUT', body: { surface, device: d, order: groups(d).flatMap((g) => x.order[g.title] ?? []), hidden: [...x.hidden].filter((k) => known.has(k)) } });
      }
    },
    onSuccess: () => {
      toast.success('Layout saved');
      void qc.invalidateQueries({ queryKey: layoutKey(prefix) });
      onClose();
    },
    onError: (e) => setError(isApiError(e) ? Object.values(e.fieldErrors)[0] ?? e.message : 'Could not save the layout.')
  });
  const reset = useMutation({
    mutationFn: () => api(`${prefix}/ui-layout?surface=${surface}&device=${device}`, { method: 'DELETE' }),
    onSuccess: () => {
      toast.success(`Back to the standard ${device === 'mobile' ? 'phone' : 'desktop'} layout`);
      setDrafts((d) => ({ ...d, [device]: undefined }));
      void qc.invalidateQueries({ queryKey: layoutKey(prefix) });
    },
    onError: () => setError('Could not reset the layout.')
  });

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-lg p-0">
        <div className="border-b px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <LayoutTemplate className="size-4 text-primary" /> {title}
          </DialogTitle>
          <DialogDescription className="mt-1 text-[13px] text-muted-foreground">
            Choose what shows and in what order. This is saved for everyone here, separately for computers and for phones. People still see only what their permissions allow.
          </DialogDescription>
          <div className="mt-3 inline-flex rounded-md border bg-muted/60 p-0.5" role="tablist">
            {(['desktop', 'mobile'] as Device[]).map((d) => (
              <button key={d} type="button" role="tab" aria-selected={device === d} onClick={() => setDevice(d)}
                className={cn('flex items-center gap-1.5 rounded px-3 py-1.5 text-[13px] font-medium', device === d ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground')}>
                {d === 'desktop' ? <Monitor className="size-3.5" /> : <Smartphone className="size-3.5" />} {d === 'desktop' ? 'Desktop' : 'Mobile'}
                {drafts[d] ? <span className="size-1.5 rounded-full bg-primary" aria-label="changed" /> : null}
              </button>
            ))}
          </div>
        </div>
        <div className="max-h-[56vh] space-y-4 overflow-y-auto px-5 py-4">
          {error ? <Alert tone="danger">{error}</Alert> : null}
          {list.map((g) => (
            <section key={g.title}>
              <p className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{g.title}</p>
              {g.hint ? <p className="mb-1.5 text-xs text-muted-foreground">{g.hint}</p> : null}
              <ul className="divide-y rounded-md border">
                {(draft.order[g.title] ?? []).filter((k) => labels.has(k)).map((key, i, keys) => {
                  const off = draft.hidden.has(key);
                  const locked = g.locked?.includes(key);
                  return (
                    <li key={key} className={cn('flex items-center gap-1 px-3 py-1.5 text-[13px]', off && 'bg-muted/40 text-muted-foreground')}>
                      <span className={cn('min-w-0 flex-1 truncate', off && 'line-through')}>{labels.get(key)}</span>
                      <Button type="button" variant="ghost" size="icon-sm" disabled={i === 0} aria-label={`Move ${labels.get(key)} up`} onClick={() => move(g.title, key, -1)}><ArrowUp /></Button>
                      <Button type="button" variant="ghost" size="icon-sm" disabled={i === keys.length - 1} aria-label={`Move ${labels.get(key)} down`} onClick={() => move(g.title, key, 1)}><ArrowDown /></Button>
                      {locked ? (
                        <span className="grid size-8 place-items-center text-muted-foreground" title="Always shown"><Lock className="size-3.5" /></span>
                      ) : (
                        <Button type="button" variant="ghost" size="icon-sm" aria-label={off ? `Show ${labels.get(key)}` : `Hide ${labels.get(key)}`} aria-pressed={!off} onClick={() => toggle(key)}>
                          {off ? <EyeOff /> : <Eye />}
                        </Button>
                      )}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}
        </div>
        <div className="flex items-center justify-between gap-2 border-t px-5 py-3">
          <Button type="button" variant="ghost" size="sm" loading={reset.isPending} onClick={() => reset.mutate()}>
            <RotateCcw /> Standard {device === 'mobile' ? 'mobile' : 'desktop'} layout
          </Button>
          <span className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
            <Button type="button" loading={save.isPending} disabled={!drafts.desktop && !drafts.mobile} onClick={() => { setError(null); save.mutate(); }}>Save layout</Button>
          </span>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** "Edit layout" button + its editor, shown only to people who may customize. */
export function EditLayoutButton({ prefix, surface, title, groups, label = 'Edit layout', className, compact }: {
  prefix: string | null | undefined; surface: Surface; title: string; groups: (device: Device) => LayoutGroup[]; label?: string; className?: string; compact?: boolean;
}) {
  const q = useUiLayouts(prefix);
  const [open, setOpen] = useState(false);
  if (!prefix || !q.data?.canEdit) return null;
  return (
    <>
      {compact ? (
        <button type="button" onClick={() => setOpen(true)} aria-label={label} title={label} className={className}>
          <LayoutTemplate className="size-4" />
        </button>
      ) : (
        <Button variant="outline" size="sm" onClick={() => setOpen(true)} className={className}>
          <LayoutTemplate /> {label}
        </Button>
      )}
      {open ? <LayoutEditor prefix={prefix} surface={surface} title={title} groups={groups} onClose={() => setOpen(false)} /> : null}
    </>
  );
}
