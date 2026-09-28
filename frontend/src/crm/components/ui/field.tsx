import { cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from 'react';
import * as LabelPrimitive from '@radix-ui/react-label';
import { AlertCircle } from 'lucide-react';
import { cn } from '@crm/lib/utils';

interface FieldProps {
  label: ReactNode;
  error?: string;
  hint?: ReactNode;
  labelAside?: ReactNode;
  className?: string;
  children: ReactElement<Record<string, unknown>>;
}

/** Label + control + error/hint, wired with ids for screen readers (WCAG 2.2 AA). */
export function Field({ label, error, hint, labelAside, className, children }: FieldProps) {
  const id = useId();
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  const describedBy = [error ? errorId : null, hint ? hintId : null].filter(Boolean).join(' ') || undefined;

  const control = isValidElement(children)
    ? cloneElement(children, { id, 'aria-describedby': describedBy, invalid: Boolean(error) })
    : children;

  return (
    <div className={cn('space-y-1.5', className)}>
      <div className="flex items-center justify-between gap-2">
        <LabelPrimitive.Root htmlFor={id} className="text-[13px] font-medium text-foreground">
          {label}
        </LabelPrimitive.Root>
        {labelAside}
      </div>
      {control}
      {error ? (
        <p id={errorId} className="flex items-start gap-1.5 text-[13px] text-danger animate-fade-in">
          <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          {error}
        </p>
      ) : hint ? (
        <div id={hintId} className="text-[13px] text-muted-foreground">
          {hint}
        </div>
      ) : null}
    </div>
  );
}
