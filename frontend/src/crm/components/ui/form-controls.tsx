import { forwardRef, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { cn } from '@crm/lib/utils';

const controlBase =
  'w-full rounded-md border bg-background text-sm text-foreground shadow-sm outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground/70 focus:border-primary focus:ring-[3px] focus:ring-primary/15 disabled:cursor-not-allowed disabled:opacity-60';

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(({ className, invalid, rows = 3, ...props }, ref) => (
  <textarea
    ref={ref}
    rows={rows}
    aria-invalid={invalid || undefined}
    className={cn(controlBase, 'min-h-[72px] px-3 py-2 leading-5', invalid ? 'border-danger focus:border-danger focus:ring-danger/15' : 'border-input hover:border-muted-foreground/40', className)}
    {...props}
  />
));
Textarea.displayName = 'Textarea';

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  invalid?: boolean;
  options?: Array<{ value: string; label: string }>;
  placeholder?: string;
}

/** Native select (best on mobile, fully accessible) styled like the inputs. */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(({ className, invalid, options, placeholder, children, ...props }, ref) => (
  <div className={cn('relative', className)}>
    <select
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        controlBase,
        'h-9 appearance-none pl-3 pr-8',
        invalid ? 'border-danger focus:border-danger focus:ring-danger/15' : 'border-input hover:border-muted-foreground/40'
      )}
      {...props}
    >
      {placeholder !== undefined ? <option value="">{placeholder}</option> : null}
      {options?.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
      {children}
    </select>
    <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
  </div>
));
Select.displayName = 'Select';

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'onChange'> {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  label?: ReactNode;
  description?: ReactNode;
  invalid?: boolean;
}

export function Checkbox({ checked, onCheckedChange, label, description, className, disabled, invalid: _invalid, ...props }: CheckboxProps) {
  return (
    <label className={cn('group flex cursor-pointer items-start gap-2.5', disabled && 'cursor-not-allowed opacity-60', className)}>
      <span className="relative mt-0.5 grid size-4 shrink-0 place-items-center">
        <input
          type="checkbox"
          className="peer absolute inset-0 cursor-pointer appearance-none rounded border border-input bg-background shadow-sm transition-colors checked:border-primary checked:bg-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 disabled:cursor-not-allowed"
          checked={checked}
          disabled={disabled}
          onChange={(e) => onCheckedChange(e.target.checked)}
          {...props}
        />
        <Check className="pointer-events-none relative size-3 text-primary-foreground opacity-0 peer-checked:opacity-100" strokeWidth={3} aria-hidden />
      </span>
      {label || description ? (
        <span className="min-w-0 leading-5">
          {label ? <span className="block text-[13px] font-medium text-foreground">{label}</span> : null}
          {description ? <span className="block text-xs text-muted-foreground">{description}</span> : null}
        </span>
      ) : null}
    </label>
  );
}

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  label?: ReactNode;
  description?: ReactNode;
  className?: string;
  id?: string;
  'aria-label'?: string;
}

export function Switch({ checked, onCheckedChange, disabled, label, description, className, id, ...aria }: SwitchProps) {
  const control = (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={aria['aria-label']}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-60',
        checked ? 'bg-primary' : 'bg-input'
      )}
    >
      <span className={cn('block size-4 rounded-full bg-white shadow-sm transition-transform', checked ? 'translate-x-4' : 'translate-x-0.5')} />
    </button>
  );
  if (!label && !description) return control;
  return (
    <div className={cn('flex items-start justify-between gap-4', className)}>
      <div className="min-w-0 leading-5">
        {label ? (
          <label htmlFor={id} className="block text-[13px] font-medium text-foreground">
            {label}
          </label>
        ) : null}
        {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      </div>
      {control}
    </div>
  );
}
