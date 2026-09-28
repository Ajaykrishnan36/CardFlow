import { useEffect, useRef, useState } from 'react';
import { cn } from '@crm/lib/utils';

interface OtpInputProps {
  value: string;
  onChange: (value: string) => void;
  onComplete?: (value: string) => void;
  length?: number;
  invalid?: boolean;
  disabled?: boolean;
  autoFocus?: boolean;
  id?: string;
  'aria-describedby'?: string;
  label: string;
}

/**
 * One real <input> holds the whole code; the six boxes are a visual layer on top.
 * A single field can't drop keystrokes while focus hops between boxes, and it gets
 * paste, SMS/authenticator autofill and screen-reader labelling for free.
 */
export function OtpInput({ value, onChange, onComplete, length = 6, invalid, disabled, autoFocus, id, label, ...aria }: OtpInputProps) {
  const ref = useRef<HTMLInputElement>(null);
  const [focused, setFocused] = useState(false);

  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  const keepCaretAtEnd = () => {
    const el = ref.current;
    if (el && el.selectionStart !== el.value.length) el.setSelectionRange(el.value.length, el.value.length);
  };

  const active = Math.min(value.length, length - 1);

  return (
    <div className="relative" onClick={() => ref.current?.focus()}>
      <input
        ref={ref}
        id={id}
        aria-describedby={aria['aria-describedby']}
        aria-label={label}
        aria-invalid={invalid || undefined}
        value={value}
        disabled={disabled}
        inputMode="numeric"
        pattern="[0-9]*"
        autoComplete="one-time-code"
        maxLength={length}
        spellCheck={false}
        onFocus={() => {
          setFocused(true);
          keepCaretAtEnd();
        }}
        onBlur={() => setFocused(false)}
        onSelect={keepCaretAtEnd}
        onChange={(e) => {
          const clean = e.target.value.replace(/\D/g, '').slice(0, length);
          onChange(clean);
          if (clean.length === length) onComplete?.(clean);
        }}
        className="absolute inset-0 z-10 h-full w-full cursor-text bg-transparent text-transparent caret-transparent opacity-0 outline-none disabled:cursor-not-allowed"
      />
      <div className="flex justify-between gap-2 sm:gap-2.5" aria-hidden>
        {Array.from({ length }, (_, i) => {
          const char = value[i] ?? '';
          const isActive = focused && i === active && !disabled;
          return (
            <div
              key={i}
              className={cn(
                'relative grid h-12 w-full max-w-[3.25rem] place-items-center rounded-lg border bg-background text-xl font-semibold tabular-nums text-foreground shadow-sm transition-[border-color,box-shadow] sm:h-14 sm:text-2xl',
                invalid ? 'border-danger' : isActive ? 'border-primary ring-[3px] ring-primary/15' : char ? 'border-foreground/25' : 'border-input',
                disabled && 'opacity-60'
              )}
            >
              {char}
              {isActive && !char ? <span className="h-6 w-px animate-pulse bg-foreground" /> : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
