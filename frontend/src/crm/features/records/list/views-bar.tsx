import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { CalendarDays, Copy, Ellipsis, Globe, KanbanSquare, Lock, Pencil, Plus, Star, Table2, Trash, Trash2 } from 'lucide-react';
import type { ObjectMeta } from '@crm/api/types';
import type { SavedView, ViewDefinition, ViewKind } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogTitle, Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from '@crm/components/ui/menu';
import { Select } from '@crm/components/ui/form-controls';
import { cn } from '@crm/lib/utils';

export const ALL_VIEW = 'all';
export const BIN_VIEW = 'bin';

export const kindIcon = { table: Table2, kanban: KanbanSquare, calendar: CalendarDays } as const;

export function ViewsBar({
  meta,
  views,
  current,
  onSelect,
  canShare,
  canBin,
  favorites,
  onToggleFavorite,
  onCreate,
  onRename,
  onDuplicate,
  onShare,
  onDelete
}: {
  meta: ObjectMeta;
  views: SavedView[];
  current: string;
  onSelect: (id: string) => void;
  canShare: boolean;
  canBin: boolean;
  favorites: Set<string>;
  onToggleFavorite: (v: SavedView) => void;
  onCreate: () => void;
  onRename: (v: SavedView) => void;
  onDuplicate: (v: SavedView) => void;
  onShare: (v: SavedView, shared: boolean) => void;
  onDelete: (v: SavedView) => void;
}) {
  const { t } = useTranslation();
  const tab = (id: string, label: string, Icon: typeof Table2, extra?: React.ReactNode) => (
    <div key={id} className={cn('group flex shrink-0 items-center rounded-md', current === id ? 'bg-background shadow-sm ring-1 ring-border' : 'hover:bg-muted')}>
      <button
        type="button"
        role="tab"
        aria-selected={current === id}
        onClick={() => onSelect(id)}
        className={cn('flex items-center gap-1.5 whitespace-nowrap px-2.5 py-1.5 text-[13px] font-medium', current === id ? 'text-foreground' : 'text-muted-foreground')}
      >
        <Icon className="size-3.5" aria-hidden />
        {label}
      </button>
      {extra}
    </div>
  );
  return (
    <div className="flex items-center gap-1 overflow-x-auto border-b px-3 py-1.5" role="tablist" aria-label={t('lists.views.label')}>
      {tab(ALL_VIEW, t('lists.views.all', { objects: meta.labelPlural }), Table2)}
      {views.map((v) =>
        tab(
          v.id,
          v.name,
          kindIcon[v.kind],
          <Menu>
            <MenuTrigger asChild>
              <button type="button" className={cn('mr-1 grid size-5 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground', current !== v.id && 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100')}
                aria-label={t('lists.views.actions', { name: v.name })}>
                <Ellipsis className="size-3.5" />
              </button>
            </MenuTrigger>
            <MenuContent>
              <MenuItem onSelect={() => onToggleFavorite(v)}>
                <Star /> {favorites.has(v.id) ? t('lists.views.unfavorite') : t('lists.views.favorite')}
              </MenuItem>
              <MenuItem onSelect={() => onDuplicate(v)}>
                <Copy /> {t('lists.views.duplicate')}
              </MenuItem>
              {v.canEdit ? (
                <>
                  <MenuItem onSelect={() => onRename(v)}>
                    <Pencil /> {t('lists.views.rename')}
                  </MenuItem>
                  {canShare ? (
                    <MenuItem onSelect={() => onShare(v, v.visibility !== 'shared')}>
                      {v.visibility === 'shared' ? <Lock /> : <Globe />} {v.visibility === 'shared' ? t('lists.views.makePersonal') : t('lists.views.share')}
                    </MenuItem>
                  ) : null}
                  <MenuSeparator />
                  <MenuItem danger onSelect={() => onDelete(v)}>
                    <Trash2 /> {t('lists.views.delete')}
                  </MenuItem>
                </>
              ) : null}
            </MenuContent>
          </Menu>
        )
      )}
      <Button variant="subtle" size="sm" className="shrink-0" onClick={onCreate}>
        <Plus /> {t('lists.views.new')}
      </Button>
      {canBin ? <div className="ml-auto shrink-0">{tab(BIN_VIEW, t('lists.bin.tab'), Trash)}</div> : null}
    </div>
  );
}

/** Name a new view (from scratch or as a copy of the current one) and choose its type. */
export function ViewDialog({
  meta,
  open,
  onOpenChange,
  initial,
  canShare,
  busy,
  onSave,
  title
}: {
  meta: ObjectMeta;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  initial: { name: string; kind: ViewKind; visibility: 'personal' | 'shared'; definition: ViewDefinition };
  canShare: boolean;
  busy: boolean;
  onSave: (v: { name: string; kind: ViewKind; visibility: 'personal' | 'shared'; definition: ViewDefinition }) => void;
  title: string;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(initial.name);
  const [kind, setKind] = useState<ViewKind>(initial.kind);
  const [visibility, setVisibility] = useState(initial.visibility);
  const [groupBy, setGroupBy] = useState(initial.definition.groupBy ?? meta.statusField ?? '');
  const [dateField, setDateField] = useState(initial.definition.calendarField ?? '');
  const groupable = meta.fields.filter((f) => ['select', 'boolean', 'lookup', 'rating'].includes(f.type));
  const dateFields = meta.fields.filter((f) => f.type === 'date' || f.type === 'datetime');
  const ok = name.trim() && (kind !== 'kanban' || groupBy) && (kind !== 'calendar' || dateField);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md p-5">
        <DialogTitle className="text-base font-semibold">{title}</DialogTitle>
        <DialogDescription className="mt-1 text-[13px] text-muted-foreground">{t('lists.views.dialogBody')}</DialogDescription>
        <form
          className="mt-4 space-y-3"
          onSubmit={(e) => {
            e.preventDefault();
            if (!ok) return;
            onSave({
              name: name.trim(),
              kind,
              visibility,
              definition: { ...initial.definition, groupBy: kind === 'kanban' ? groupBy : initial.definition.groupBy, calendarField: kind === 'calendar' ? dateField : undefined }
            });
          }}
        >
          <label className="block space-y-1 text-[13px]">
            <span className="font-medium">{t('lists.views.name')}</span>
            <Input autoFocus value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder={t('lists.views.namePlaceholder')} />
          </label>
          <div className="space-y-1 text-[13px]">
            <span className="font-medium">{t('lists.views.type')}</span>
            <div className="grid grid-cols-3 gap-2">
              {(['table', 'kanban', 'calendar'] as ViewKind[]).map((k) => {
                const Icon = kindIcon[k];
                const disabled = (k === 'kanban' && !groupable.length) || (k === 'calendar' && !dateFields.length);
                return (
                  <button key={k} type="button" disabled={disabled} onClick={() => setKind(k)} aria-pressed={kind === k}
                    className={cn('flex flex-col items-center gap-1 rounded-lg border px-2 py-2.5 text-xs font-medium disabled:opacity-40', kind === k ? 'border-primary bg-primary-soft text-primary' : 'hover:bg-muted')}>
                    <Icon className="size-4" aria-hidden />
                    {t(`lists.views.kind.${k}`)}
                  </button>
                );
              })}
            </div>
          </div>
          {kind === 'kanban' ? (
            <label className="block space-y-1 text-[13px]">
              <span className="font-medium">{t('lists.views.columnsBy')}</span>
              <Select value={groupBy} onChange={(e) => setGroupBy(e.target.value)} placeholder={t('lists.views.pickField')} options={groupable.map((f) => ({ value: f.key, label: f.label }))} />
            </label>
          ) : null}
          {kind === 'calendar' ? (
            <label className="block space-y-1 text-[13px]">
              <span className="font-medium">{t('lists.views.dateBy')}</span>
              <Select value={dateField} onChange={(e) => setDateField(e.target.value)} placeholder={t('lists.views.pickField')} options={dateFields.map((f) => ({ value: f.key, label: f.label }))} />
            </label>
          ) : null}
          {canShare ? (
            <label className="block space-y-1 text-[13px]">
              <span className="font-medium">{t('lists.views.whoSees')}</span>
              <Select value={visibility} onChange={(e) => setVisibility(e.target.value as 'personal' | 'shared')}
                options={[
                  { value: 'personal', label: t('lists.views.personal') },
                  { value: 'shared', label: t('lists.views.everyone') }
                ]} />
            </label>
          ) : null}
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('lists.bulk.cancel')}
            </Button>
            <Button type="submit" loading={busy} disabled={!ok}>
              {t('lists.views.save')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
