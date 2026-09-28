import { forwardRef, type InputHTMLAttributes, type ReactNode } from 'react';
import { cn } from '@crm/lib/utils';

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  leading?: ReactNode;
  trailing?: ReactNode;
  invalid?: boolean;
  inputSize?: 'md' | 'lg';
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className, leading, trailing, invalid, inputSize = 'md', ...props }, ref) => (
    <div
      className={cn(
        'group relative flex w-full items-center rounded-md border bg-background shadow-sm transition-[border-color,box-shadow]',
        'focus-within:border-primary focus-within:ring-[3px] focus-within:ring-primary/15',
        invalid ? 'border-danger focus-within:border-danger focus-within:ring-danger/15' : 'border-input hover:border-muted-foreground/40',
        props.disabled && 'opacity-60',
        className
      )}
    >
      {leading ? <span className="pointer-events-none pl-3 text-muted-foreground [&_svg]:size-4">{leading}</span> : null}
      <input
        ref={ref}
        aria-invalid={invalid || undefined}
        className={cn(
          'peer w-full min-w-0 bg-transparent px-3 text-foreground outline-none placeholder:text-muted-foreground/70 disabled:cursor-not-allowed',
          inputSize === 'lg' ? 'h-11 text-[15px]' : 'h-9 text-sm',
          leading && 'pl-2.5',
          trailing && 'pr-1'
        )}
        {...props}
      />
      {trailing ? <span className="flex items-center pr-1.5">{trailing}</span> : null}
    </div>
  )
);
Input.displayName = 'Input';
