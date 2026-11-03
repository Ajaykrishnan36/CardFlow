import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { Check, ChevronsUpDown, Search, X } from 'lucide-react';
import { Input } from '@crm/components/ui/input';
import { cn } from '@crm/lib/utils';

export interface SearchOption {
  value: string;
  label: string;
  /** More words the search matches (not shown). */
  keywords?: string;
}

/**
 * A searchable single-select (D-174): type to narrow the list, arrow keys and Enter to pick,
 * Escape to close. Exactly one value; the chosen one shows in the box.
 */
export function SearchSelect({
  value,
  onChange,
  options,
  placeholder = 'Search or choose…',
  invalid,
  disabled,
  id,
  emptyText = 'Nothing matches. Choose Other.',
  'aria-describedby': describedBy,
}: {
  value: string;
  onChange: (value: string) => void;
  options: SearchOption[];
  placeholder?: string;
  invalid?: boolean;
  disabled?: boolean;
  id?: string;
  emptyText?: string;
  'aria-describedby'?: string;
}) {
  const listId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [active, setActive] = useState(0);
  const selected = options.find((o) => o.value === value);

  const results = useMemo(() => {
    const q = text.trim().toLowerCase();
    if (!q) return options;
    const words = q.split(/\s+/);
    return options.filter((o) => {
      const hay = `${o.label} ${o.keywords ?? ''}`.toLowerCase();
      return words.every((w) => hay.includes(w));
    });
  }, [options, text]);

  useEffect(() => setActive(0), [text]);
  // When it opens, start on the chosen one.
  useEffect(() => {
    if (open && !text) {
      const i = results.findIndex((o) => o.value === value);
      setActive(i >= 0 ? i : 0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  useEffect(() => {
    if (!open) return;
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active, open]);

  const choose = (o: SearchOption) => {
    onChange(o.value);
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
        value={open ? text : selected?.label ?? ''}
        placeholder={open && selected ? selected.label : placeholder}
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
              aria-label="Clear"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                onChange('');
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
          ref={listRef}
          id={listId}
          role="listbox"
          className="absolute left-0 right-0 top-full z-40 mt-1 max-h-64 overflow-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-pop animate-fade-in"
        >
          {results.length === 0 ? (
            <li className="px-2 py-2 text-[13px] text-muted-foreground">{emptyText}</li>
          ) : (
            results.map((o, i) => (
              <li
                key={o.value}
                id={`${listId}-${i}`}
                data-index={i}
                role="option"
                aria-selected={o.value === value}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setActive(i)}
                onClick={() => choose(o)}
                className={cn(
                  'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-[13px]',
                  i === active ? 'bg-muted' : '',
                  o.value === value && 'font-semibold text-primary',
                )}
              >
                <span className="min-w-0 flex-1 truncate">{o.label}</span>
                {o.value === value ? <Check className="size-3.5 shrink-0" aria-hidden /> : null}
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}
