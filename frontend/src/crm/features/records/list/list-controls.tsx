import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ArrowDown, ArrowUp, ArrowUpDown, Columns3, Filter, GripVertical, Plus, Rows3, Trash2 } from 'lucide-react';
import type { FieldDef, ObjectMeta } from '@crm/api/types';
import type { FilterGroup, SortSpec } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Select, Checkbox } from '@crm/components/ui/form-controls';
import { Popover } from '@crm/components/ui/popover';
import { cn } from '@crm/lib/utils';
import { FilterBuilder } from './filter-builder';
import { countConditions, emptyGroup } from './filter-utils';

export function FilterButton({ meta, value, onChange }: { meta: ObjectMeta; value: FilterGroup | undefined; onChange: (g: FilterGroup | undefined) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const n = countConditions(value);
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      width={640}
      trigger={(p) => (
        <Button variant={n ? 'secondary' : 'outline'} size="sm" ref={p.ref as never} onClick={p.onClick} aria-expanded={p['aria-expanded']}>
          <Filter /> {n ? t('lists.filter.buttonN', { count: n }) : t('lists.filter.button')}
        </Button>
      )}
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <p className="text-[13px] font-semibold">{t('lists.filter.title')}</p>
          {n ? (
            <Button variant="link" size="sm" onClick={() => onChange(undefined)}>
              {t('lists.filter.clear')}
            </Button>
          ) : null}
        </div>
        <FilterBuilder meta={meta} value={value ?? emptyGroup()} onChange={(g) => onChange(g.filters.length ? g : undefined)} />
      </div>
    </Popover>
  );
}

const sortable = (f: FieldDef) => !['multiselect', 'relations', 'files', 'json', 'textarea', 'address'].includes(f.type);

export function SortButton({ meta, value, onChange }: { meta: ObjectMeta; value: SortSpec[]; onChange: (s: SortSpec[]) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const fields = [{ key: 'title', label: t('records.list.name') } as { key: string; label: string }, ...meta.fields.filter(sortable)];
  const labelOf = (k: string) => fields.find((f) => f.key === k)?.label ?? k;
  const set = (i: number, s: SortSpec | null) => {
    const next = [...value];
    if (s) next[i] = s;
    else next.splice(i, 1);
    onChange(next);
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      width={420}
      trigger={(p) => (
        <Button variant={value.length ? 'secondary' : 'outline'} size="sm" ref={p.ref as never} onClick={p.onClick} aria-expanded={p['aria-expanded']}>
          <ArrowUpDown /> {value.length ? t('lists.sort.buttonOn', { field: labelOf(value[0]!.field) + (value.length > 1 ? ` +${value.length - 1}` : '') }) : t('lists.sort.button')}
        </Button>
      )}
    >
      <div className="space-y-2">
        <p className="text-[13px] font-semibold">{t('lists.sort.title')}</p>
        {value.length === 0 ? <p className="text-[13px] text-muted-foreground">{t('lists.sort.none')}</p> : null}
        {value.map((s, i) => (
          <div key={i} className="flex items-center gap-2">
            <span className="w-12 shrink-0 text-xs text-muted-foreground">{i === 0 ? t('lists.sort.first') : t('lists.sort.then')}</span>
            <Select className="flex-1" aria-label={t('lists.sort.field')} value={s.field} onChange={(e) => set(i, { ...s, field: e.target.value })}
              options={fields.map((f) => ({ value: f.key, label: f.label }))} />
            <Button variant="outline" size="icon" onClick={() => set(i, { ...s, dir: s.dir === 'asc' ? 'desc' : 'asc' })} aria-label={s.dir === 'asc' ? t('lists.sort.asc') : t('lists.sort.desc')}
              title={s.dir === 'asc' ? t('lists.sort.asc') : t('lists.sort.desc')}>
              {s.dir === 'asc' ? <ArrowUp /> : <ArrowDown />}
            </Button>
            <Button variant="subtle" size="icon" onClick={() => set(i, null)} aria-label={t('lists.sort.remove')}>
              <Trash2 />
            </Button>
          </div>
        ))}
        {value.length < 5 ? (
          <Button variant="ghost" size="sm" onClick={() => onChange([...value, { field: value.length ? 'createdAt' : 'title', dir: value.length ? 'desc' : 'asc' }])}>
            <Plus /> {t('lists.sort.add')}
          </Button>
        ) : null}
      </div>
    </Popover>
  );
}

/** Pick and order the columns of the table. */
export function ColumnsButton({ meta, value, onChange }: { meta: ObjectMeta; value: string[]; onChange: (cols: string[]) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [drag, setDrag] = useState<string | null>(null);
  const byKey = new Map(meta.fields.map((f) => [f.key, f]));
  const shown = value.filter((k) => byKey.has(k));
  const hidden = meta.fields.filter((f) => !shown.includes(f.key) && f.key !== 'code').sort((a, b) => a.label.localeCompare(b.label));
  const move = (from: string, to: string) => {
    const next = shown.filter((k) => k !== from);
    next.splice(next.indexOf(to), 0, from);
    onChange(next);
  };
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      width={300}
      align="end"
      trigger={(p) => (
        <Button variant="outline" size="sm" ref={p.ref as never} onClick={p.onClick} aria-expanded={p['aria-expanded']}>
          <Columns3 /> {t('lists.columns.button')}
        </Button>
      )}
    >
      <div className="space-y-3">
        <div>
          <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{t('lists.columns.shown')}</p>
          <ul className="space-y-0.5">
            {shown.map((k) => (
              <li
                key={k}
                draggable
                onDragStart={() => setDrag(k)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={() => drag && drag !== k && move(drag, k)}
                className={cn('flex items-center gap-2 rounded px-1 py-1 text-[13px] hover:bg-muted', drag === k && 'opacity-50')}
              >
                <GripVertical className="size-3.5 cursor-grab text-muted-foreground" aria-hidden />
                <Checkbox checked onCheckedChange={() => onChange(shown.filter((x) => x !== k))} label={byKey.get(k)?.label ?? k} />
              </li>
            ))}
          </ul>
        </div>
        {hidden.length ? (
          <div>
            <p className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">{t('lists.columns.hidden')}</p>
            <ul className="max-h-56 space-y-0.5 overflow-auto">
              {hidden.map((f) => (
                <li key={f.key} className="flex items-center gap-2 rounded px-1 py-1 text-[13px] hover:bg-muted">
                  <span className="size-3.5" />
                  <Checkbox checked={false} onCheckedChange={() => onChange([...shown, f.key])} label={f.label} />
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </Popover>
  );
}

export function DensityButton({ compact, onChange }: { compact: boolean; onChange: (c: boolean) => void }) {
  const { t } = useTranslation();
  return (
    <Button variant="outline" size="icon-sm" onClick={() => onChange(!compact)} aria-pressed={compact} aria-label={t('lists.density')} title={t('lists.density')}>
      <Rows3 />
    </Button>
  );
}
