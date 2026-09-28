import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { ChevronsUpDown, Search, X } from 'lucide-react';
import type { FieldDef, LookupTarget, LookupValue } from '@crm/api/types';
import { Input } from '@crm/components/ui/input';
import { Field } from '@crm/components/ui/field';
import { Select, Switch, Textarea } from '@crm/components/ui/form-controls';
import { Spinner } from '@crm/components/ui/spinner';
import { cn } from '@crm/lib/utils';
import { HelpTip } from './field-value';
import { isoToLocalInput, localInputToIso } from './use-object-meta';
import { canLookup, useRecordScope } from './record-scope';

interface ControlProps {
  id?: string;
  'aria-describedby'?: string;
  invalid?: boolean;
}

export interface FieldInputProps extends ControlProps {
  field: FieldDef;
  value: unknown;
  onChange: (value: unknown) => void;
  /** Current display label of a lookup value. */
  lookupLabel?: string;
  disabled?: boolean;
}

const str = (v: unknown) => (v === null || v === undefined ? '' : String(v));

/** Editor control for one field, chosen by FieldType. */
export function FieldInput({ field, value, onChange, lookupLabel, disabled, ...control }: FieldInputProps) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const text = (type: string, inputMode?: 'email' | 'tel' | 'url' | 'text') => (
    <Input
      {...control}
      type={type}
      inputMode={inputMode}
      disabled={disabled}
      value={str(value)}
      autoComplete="off"
      onChange={(e) => onChange(e.target.value)}
    />
  );

  switch (field.type) {
    case 'email':
      return text('email', 'email');
    case 'phone':
      return text('tel', 'tel');
    case 'url':
      return text('url', 'url');
    case 'textarea':
      return <Textarea {...control} disabled={disabled} rows={3} value={str(value)} onChange={(e) => onChange(e.target.value)} />;
    case 'number':
    case 'currency':
    case 'percent':
      return (
        <Input
          {...control}
          type="number"
          step="any"
          inputMode="decimal"
          disabled={disabled}
          value={str(value)}
          leading={field.type === 'currency' ? <span className="text-[13px]">₹</span> : undefined}
          trailing={field.type === 'percent' ? <span className="pr-1.5 text-[13px] text-muted-foreground">%</span> : undefined}
          onChange={(e) => {
            const raw = e.target.value;
            if (raw === '') return onChange(null);
            const n = Number(raw);
            onChange(Number.isFinite(n) ? n : raw);
          }}
        />
      );
    case 'date':
      return <Input {...control} type="date" disabled={disabled} value={str(value).slice(0, 10)} onChange={(e) => onChange(e.target.value || null)} />;
    case 'datetime':
      return <Input {...control} type="datetime-local" disabled={disabled} value={isoToLocalInput(value)} onChange={(e) => onChange(localInputToIso(e.target.value))} />;
    case 'select':
      return (
        <Select
          {...control}
          disabled={disabled}
          value={str(value)}
          placeholder={t('records.input.selectPlaceholder')}
          options={field.options ?? []}
          onChange={(e) => onChange(e.target.value || null)}
        />
      );
    case 'multiselect':
      return <MultiselectInput {...control} field={field} disabled={disabled} value={value} onChange={onChange} />;
    case 'boolean':
      return (
        <div className="flex h-9 items-center">
          <Switch id={control.id} checked={Boolean(value)} disabled={disabled} onCheckedChange={(c) => onChange(c)} aria-label={field.label} />
        </div>
      );
    case 'lookup':
      if (field.lookup && !canLookup(scope, field.lookup)) {
        // Members can't search this target (e.g. workspaces / products): show the value, read-only.
        return <Input {...control} disabled value={lookupLabel ?? str(value)} placeholder={t('records.input.lookupUnavailable')} title={t('records.input.lookupUnavailable')} readOnly />;
      }
      return field.lookup ? (
        <LookupCombobox
          {...control}
          target={field.lookup}
          value={typeof value === 'string' ? value : value == null ? null : String(value)}
          label={lookupLabel}
          disabled={disabled}
          onChange={(v) => onChange(v?.id ?? null)}
        />
      ) : (
        text('text')
      );
    default:
      return text('text');
  }
}

/** Label + control + error for a record field (dialogs and the record page's edit mode). */
export function FieldEditor({ field, error, className, ...props }: FieldInputProps & { error?: string; className?: string }) {
  return (
    <Field
      className={className}
      error={error}
      label={
        <>
          {field.label}
          {field.required ? (
            <span className="ml-0.5 text-danger" aria-hidden>
              *
            </span>
          ) : null}
        </>
      }
      labelAside={field.helpText ? <HelpTip text={field.helpText} /> : undefined}
    >
      <FieldInput field={field} {...props} />
    </Field>
  );
}

// ---- Multiselect: tag chips, free text + option suggestions ----

function MultiselectInput({
  field,
  value,
  onChange,
  disabled,
  invalid,
  ...control
}: ControlProps & { field: FieldDef; value: unknown; onChange: (v: unknown) => void; disabled?: boolean }) {
  const { t } = useTranslation();
  const listId = useId();
  const [text, setText] = useState('');
  const values = Array.isArray(value) ? value.map(String) : value ? [String(value)] : [];
  const options = field.options ?? [];
  const labelOf = (v: string) => options.find((o) => o.value === v)?.label ?? v;

  const add = (raw: string) => {
    const s = raw.trim();
    if (!s) return;
    const match = options.find((o) => o.value.toLowerCase() === s.toLowerCase() || o.label.toLowerCase() === s.toLowerCase());
    const v = match?.value ?? s;
    if (!values.includes(v)) onChange([...values, v]);
    setText('');
  };
  const remove = (v: string) => {
    const next = values.filter((x) => x !== v);
    onChange(next.length ? next : null);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      add(text);
    } else if (e.key === 'Backspace' && !text && values.length) {
      remove(values[values.length - 1]!);
    }
  };

  const remaining = options.filter((o) => !values.includes(o.value));

  return (
    <div className="space-y-1.5">
      <div
        className={cn(
          'flex min-h-9 w-full flex-wrap items-center gap-1 rounded-md border bg-background px-1.5 py-1 shadow-sm transition-[border-color,box-shadow]',
          'focus-within:border-primary focus-within:ring-[3px] focus-within:ring-primary/15',
          invalid ? 'border-danger' : 'border-input hover:border-muted-foreground/40',
          disabled && 'opacity-60'
        )}
      >
        {values.map((v) => (
          <span key={v} className="inline-flex items-center gap-0.5 rounded bg-muted py-0.5 pl-2 pr-0.5 text-xs font-medium text-foreground">
            {labelOf(v)}
            <button
              type="button"
              disabled={disabled}
              onClick={() => remove(v)}
              className="grid size-4 place-items-center rounded text-muted-foreground hover:bg-background hover:text-foreground"
              aria-label={t('records.input.removeTag', { tag: labelOf(v) })}
            >
              <X className="size-3" aria-hidden />
            </button>
          </span>
        ))}
        <input
          {...control}
          aria-invalid={invalid || undefined}
          list={options.length ? listId : undefined}
          disabled={disabled}
          value={text}
          onChange={(e) => {
            const v = e.target.value;
            // Picking a datalist suggestion fires a change with the full option text.
            if (options.some((o) => o.label === v || o.value === v) && !v.endsWith(' ')) {
              add(v);
            } else setText(v);
          }}
          onKeyDown={onKeyDown}
          onBlur={() => add(text)}
          placeholder={values.length ? '' : t('records.input.tagsPlaceholder')}
          className="h-7 min-w-[8rem] flex-1 bg-transparent px-1.5 text-sm text-foreground outline-none placeholder:text-muted-foreground/70"
        />
        {options.length ? (
          <datalist id={listId}>
            {remaining.map((o) => (
              <option key={o.value} value={o.label} />
            ))}
          </datalist>
        ) : null}
      </div>
      {remaining.length > 0 && remaining.length <= 8 ? (
        <div className="flex flex-wrap gap-1">
          {remaining.map((o) => (
            <button
              key={o.value}
              type="button"
              disabled={disabled}
              onClick={() => onChange([...values, o.value])}
              className="rounded-full border border-dashed px-2 py-0.5 text-[11px] font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:bg-primary-soft hover:text-primary"
            >
              + {o.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// ---- Lookup: debounced searchable combobox ----

export function LookupCombobox({
  target,
  value,
  label,
  onChange,
  disabled,
  invalid,
  placeholder,
  id,
  'aria-describedby': describedBy
}: ControlProps & {
  target: LookupTarget;
  value: string | null;
  label?: string;
  onChange: (v: LookupValue | null) => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  const { t } = useTranslation();
  const scope = useRecordScope();
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [debounced, setDebounced] = useState('');
  const [active, setActive] = useState(0);
  const [selectedLabel, setSelectedLabel] = useState(label ?? '');

  useEffect(() => {
    if (label) setSelectedLabel(label);
  }, [label]);

  useEffect(() => {
    const h = setTimeout(() => setDebounced(text.trim()), 250);
    return () => clearTimeout(h);
  }, [text]);

  const q = useQuery({
    queryKey: ['lookup', scope.prefix, target, debounced],
    queryFn: () => scope.api.lookup(target, debounced),
    enabled: open,
    staleTime: 30_000
  });
  const results = q.data ?? [];

  useEffect(() => setActive(0), [debounced]);

  const choose = (v: LookupValue) => {
    setSelectedLabel(v.label);
    onChange(v);
    setOpen(false);
    setText('');
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!open) setOpen(true);
      else setActive((a) => Math.min(a + 1, Math.max(results.length - 1, 0)));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === 'Enter') {
      if (open && results[active]) {
        e.preventDefault();
        choose(results[active]!);
      }
    } else if (e.key === 'Escape') {
      if (open) {
        e.preventDefault();
        e.stopPropagation();
        setOpen(false);
        setText('');
      }
    }
  };

  const display = open ? text : value ? selectedLabel || value : '';
  const activeId = open && results[active] ? `${listId}-${active}` : undefined;

  return (
    <div className="relative">
      <Input
        ref={inputRef}
        id={id}
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={activeId}
        aria-describedby={describedBy}
        invalid={invalid}
        disabled={disabled}
        autoComplete="off"
        value={display}
        placeholder={open && value ? selectedLabel : placeholder ?? t('records.input.lookupPlaceholder')}
        leading={open ? <Search /> : undefined}
        onFocus={() => setOpen(true)}
        onClick={() => setOpen(true)}
        onBlur={() => {
          setOpen(false);
          setText('');
        }}
        onChange={(e) => {
          setText(e.target.value);
          if (!open) setOpen(true);
        }}
        onKeyDown={onKeyDown}
        trailing={
          value && !disabled ? (
            <button
              type="button"
              tabIndex={-1}
              className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label={t('records.input.clear')}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                setSelectedLabel('');
                onChange(null);
                inputRef.current?.focus();
              }}
            >
              <X className="size-3.5" />
            </button>
          ) : (
            <ChevronsUpDown className="mr-1.5 size-3.5 text-muted-foreground" aria-hidden />
          )
        }
      />
      {open ? (
        <ul
          id={listId}
          role="listbox"
          className="absolute left-0 right-0 top-full z-40 mt-1 max-h-60 overflow-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-pop animate-fade-in"
        >
          {q.isPending ? (
            <li className="flex items-center gap-2 px-2 py-2 text-[13px] text-muted-foreground">
              <Spinner className="size-3.5" /> {t('records.input.searching')}
            </li>
          ) : q.isError ? (
            <li className="px-2 py-2 text-[13px] text-danger">{t('records.input.lookupError')}</li>
          ) : results.length === 0 ? (
            <li className="px-2 py-2 text-[13px] text-muted-foreground">{t('records.input.noMatches')}</li>
          ) : (
            results.map((r, i) => (
              <li
                key={r.id}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={r.id === value}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setActive(i)}
                onClick={() => choose(r)}
                className={cn(
                  'flex cursor-pointer items-center justify-between gap-2 rounded px-2 py-1.5 text-[13px]',
                  i === active ? 'bg-muted' : '',
                  r.id === value && 'font-semibold text-primary'
                )}
              >
                <span className="truncate">{r.label}</span>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
