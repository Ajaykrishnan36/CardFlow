import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { FolderPlus, Plus, Trash2 } from 'lucide-react';
import type { FieldDef, ObjectMeta } from '@crm/api/types';
import { isGroup, type FilterCondition, type FilterGroup, type FilterNode, type FilterOp } from '@crm/api/types-features';
import { Button } from '@crm/components/ui/button';
import { Input } from '@crm/components/ui/input';
import { Select } from '@crm/components/ui/form-controls';
import { LookupCombobox } from '../field-input';
import { defaultValue, emptyGroup, newCondition, opsFor, valueKindFor } from './filter-utils';

/** Nested AND / OR conditions on any field the viewer can see. */
export function FilterBuilder({ meta, value, onChange }: { meta: ObjectMeta; value: FilterGroup; onChange: (g: FilterGroup) => void }) {
  const fields = useMemo(() => [...meta.fields].sort((a, b) => a.label.localeCompare(b.label)), [meta.fields]);
  return <GroupEditor fields={fields} group={value} depth={0} onChange={onChange} />;
}

function GroupEditor({
  fields,
  group,
  depth,
  onChange,
  onRemove
}: {
  fields: FieldDef[];
  group: FilterGroup;
  depth: number;
  onChange: (g: FilterGroup) => void;
  onRemove?: () => void;
}) {
  const { t } = useTranslation();
  const byKey = new Map(fields.map((f) => [f.key, f]));
  const set = (i: number, n: FilterNode | null) => {
    const next = [...group.filters];
    if (n === null) next.splice(i, 1);
    else next[i] = n;
    onChange({ ...group, filters: next });
  };
  const firstField = fields.find((f) => f.key !== 'code') ?? fields[0];
  return (
    <div className={depth > 0 ? 'space-y-2 rounded-md border border-dashed bg-muted/30 p-2.5' : 'space-y-2'}>
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <span className="text-muted-foreground">{t('lists.filter.match')}</span>
        <Select
          className="w-28"
          aria-label={t('lists.filter.matchLabel')}
          value={group.op}
          onChange={(e) => onChange({ ...group, op: e.target.value as 'and' | 'or' })}
          options={[
            { value: 'and', label: t('lists.filter.all') },
            { value: 'or', label: t('lists.filter.any') }
          ]}
        />
        <span className="text-muted-foreground">{t('lists.filter.ofThese')}</span>
        {onRemove ? (
          <Button variant="subtle" size="icon-sm" className="ml-auto" onClick={onRemove} aria-label={t('lists.filter.removeGroup')}>
            <Trash2 />
          </Button>
        ) : null}
      </div>
      {group.filters.length === 0 ? <p className="text-[13px] text-muted-foreground">{t('lists.filter.none')}</p> : null}
      {group.filters.map((n, i) =>
        isGroup(n) ? (
          <GroupEditor key={i} fields={fields} group={n} depth={depth + 1} onChange={(g) => set(i, g)} onRemove={() => set(i, null)} />
        ) : (
          <ConditionRow key={i} fields={fields} field={byKey.get(n.field)} cond={n} onChange={(c) => set(i, c)} onRemove={() => set(i, null)} />
        )
      )}
      <div className="flex flex-wrap gap-2">
        {firstField ? (
          <Button variant="ghost" size="sm" onClick={() => onChange({ ...group, filters: [...group.filters, newCondition(firstField)] })}>
            <Plus /> {t('lists.filter.addCondition')}
          </Button>
        ) : null}
        {depth < 2 && firstField ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onChange({ ...group, filters: [...group.filters, { ...emptyGroup(group.op === 'and' ? 'or' : 'and'), filters: [newCondition(firstField)] }] })}
          >
            <FolderPlus /> {t('lists.filter.addGroup')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function ConditionRow({
  fields,
  field,
  cond,
  onChange,
  onRemove
}: {
  fields: FieldDef[];
  field?: FieldDef;
  cond: FilterCondition;
  onChange: (c: FilterCondition) => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  const ops = field ? opsFor(field) : [];
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 sm:grid-cols-[10rem_9.5rem_minmax(0,1fr)_auto]">
      <Select
        aria-label={t('lists.filter.field')}
        value={cond.field}
        onChange={(e) => {
          const f = fields.find((x) => x.key === e.target.value);
          if (f) onChange(newCondition(f));
        }}
        options={fields.map((f) => ({ value: f.key, label: f.label }))}
        className="col-span-1"
      />
      <Button variant="subtle" size="icon" onClick={onRemove} aria-label={t('lists.filter.remove')} className="sm:order-last">
        <Trash2 />
      </Button>
      <Select
        aria-label={t('lists.filter.comparison')}
        value={cond.op}
        onChange={(e) => {
          const op = e.target.value as FilterOp;
          const kind = field ? valueKindFor(field, op) : 'text';
          const prevKind = field ? valueKindFor(field, cond.op) : 'text';
          onChange({ ...cond, op, value: kind === prevKind ? cond.value : defaultValue(kind) });
        }}
        options={ops.map((o) => ({ value: o, label: t(`lists.op.${o}`) }))}
        className="col-span-2 sm:col-span-1"
      />
      <div className="col-span-2 sm:col-span-1">{field ? <ValueEditor field={field} cond={cond} onChange={(v) => onChange({ ...cond, value: v })} /> : null}</div>
    </div>
  );
}

function ValueEditor({ field, cond, onChange }: { field: FieldDef; cond: FilterCondition; onChange: (v: unknown) => void }) {
  const { t } = useTranslation();
  const kind = valueKindFor(field, cond.op);
  const v = cond.value;
  switch (kind) {
    case 'none':
      return <div className="h-9" />;
    case 'days':
      return (
        <Input
          type="number"
          min={1}
          max={3650}
          aria-label={t('lists.filter.days')}
          value={String(v ?? '')}
          trailing={<span className="pr-2 text-xs text-muted-foreground">{t('lists.filter.daysUnit')}</span>}
          onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))}
        />
      );
    case 'number':
      return <Input type="number" step="any" aria-label={t('lists.filter.value')} value={String(v ?? '')} onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))} />;
    case 'numberPair':
    case 'datePair': {
      const pair = Array.isArray(v) ? v : ['', ''];
      const type = kind === 'numberPair' ? 'number' : 'date';
      return (
        <div className="flex items-center gap-1.5">
          <Input type={type} aria-label={t('lists.filter.from')} value={String(pair[0] ?? '')} onChange={(e) => onChange([e.target.value, pair[1]])} />
          <span className="text-xs text-muted-foreground">{t('lists.filter.and')}</span>
          <Input type={type} aria-label={t('lists.filter.to')} value={String(pair[1] ?? '')} onChange={(e) => onChange([pair[0], e.target.value])} />
        </div>
      );
    }
    case 'date':
      return <Input type="date" aria-label={t('lists.filter.value')} value={String(v ?? '').slice(0, 10)} onChange={(e) => onChange(e.target.value)} />;
    case 'bool':
      return (
        <Select
          aria-label={t('lists.filter.value')}
          value={String(v === false || v === 'false' ? 'false' : 'true')}
          onChange={(e) => onChange(e.target.value === 'true')}
          options={[
            { value: 'true', label: t('records.common.yes') },
            { value: 'false', label: t('records.common.no') }
          ]}
        />
      );
    case 'option':
      return (
        <Select
          aria-label={t('lists.filter.value')}
          value={Array.isArray(v) ? String(v[0] ?? '') : String(v ?? '')}
          onChange={(e) => onChange(e.target.value)}
          placeholder={t('lists.filter.value')}
          options={(field.options ?? []).map((o) => ({ value: o.value, label: o.label }))}
        />
      );
    case 'options': {
      const chosen = Array.isArray(v) ? v.map(String) : typeof v === 'string' && v ? [v] : [];
      return (
        <div className="flex max-h-36 flex-wrap gap-1 overflow-auto rounded-md border px-1.5 py-1" role="group" aria-label={t('lists.filter.value')}>
          {(field.options ?? []).map((o) => {
            const on = chosen.includes(o.value);
            return (
              <button
                key={o.value}
                type="button"
                aria-pressed={on}
                onClick={() => onChange(on ? chosen.filter((x) => x !== o.value) : [...chosen, o.value])}
                className={
                  on
                    ? 'rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground'
                    : 'rounded-full border px-2 py-0.5 text-xs font-medium text-muted-foreground hover:border-primary/40 hover:text-foreground'
                }
              >
                {o.label}
              </button>
            );
          })}
        </div>
      );
    }
    case 'lookup':
      return field.lookup ? (
        <div data-popover-keep>
          <LookupCombobox
            target={field.lookup}
            value={typeof v === 'string' && v ? v : Array.isArray(v) && v.length ? String(v[0]) : null}
            label={(cond as FilterCondition & { label?: string }).label}
            onChange={(lv) => onChange(field.type === 'relations' ? (lv ? [lv.id] : []) : lv?.id ?? '')}
          />
        </div>
      ) : null;
  }
  return <Input aria-label={t('lists.filter.value')} value={String(v ?? '')} onChange={(e) => onChange(e.target.value)} />;
}
