import { forwardRef, type ComponentPropsWithoutRef, type ElementRef, type ReactNode } from 'react';
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { Check, X } from 'lucide-react';
import { cn } from '@crm/lib/utils';

// ---- Dropdown menu ----
export const Menu = DropdownMenu.Root;
export const MenuTrigger = DropdownMenu.Trigger;
export const MenuGroup = DropdownMenu.Group;
export const MenuRadioGroup = DropdownMenu.RadioGroup;

export const MenuContent = forwardRef<ElementRef<typeof DropdownMenu.Content>, ComponentPropsWithoutRef<typeof DropdownMenu.Content>>(
  ({ className, sideOffset = 6, ...props }, ref) => (
    <DropdownMenu.Portal>
      <DropdownMenu.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn(
          'z-50 min-w-[14rem] overflow-hidden rounded-lg border bg-popover p-1 text-popover-foreground shadow-pop animate-slide-up',
          className
        )}
        {...props}
      />
    </DropdownMenu.Portal>
  )
);
MenuContent.displayName = 'MenuContent';

export const MenuItem = forwardRef<ElementRef<typeof DropdownMenu.Item>, ComponentPropsWithoutRef<typeof DropdownMenu.Item> & { danger?: boolean }>(
  ({ className, danger, ...props }, ref) => (
    <DropdownMenu.Item
      ref={ref}
      className={cn(
        'relative flex cursor-default select-none items-center gap-2 rounded-md px-2 py-1.5 text-[13px] outline-none transition-colors data-[disabled]:pointer-events-none data-[highlighted]:bg-muted data-[disabled]:opacity-50 [&_svg]:size-4 [&_svg]:text-muted-foreground',
        danger && 'text-danger data-[highlighted]:bg-danger-soft [&_svg]:text-danger',
        className
      )}
      {...props}
    />
  )
);
MenuItem.displayName = 'MenuItem';

export const MenuRadioItem = forwardRef<ElementRef<typeof DropdownMenu.RadioItem>, ComponentPropsWithoutRef<typeof DropdownMenu.RadioItem>>(
  ({ className, children, ...props }, ref) => (
    <DropdownMenu.RadioItem
      ref={ref}
      className={cn(
        'relative flex cursor-default select-none items-center gap-2 rounded-md py-1.5 pl-8 pr-2 text-[13px] outline-none transition-colors data-[highlighted]:bg-muted [&_svg]:size-4',
        className
      )}
      {...props}
    >
      <span className="absolute left-2 flex size-4 items-center justify-center">
        <DropdownMenu.ItemIndicator>
          <Check className="size-4 text-primary" />
        </DropdownMenu.ItemIndicator>
      </span>
      {children}
    </DropdownMenu.RadioItem>
  )
);
MenuRadioItem.displayName = 'MenuRadioItem';

export function MenuLabel({ children, className }: { children: ReactNode; className?: string }) {
  return <DropdownMenu.Label className={cn('px-2 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground', className)}>{children}</DropdownMenu.Label>;
}

export function MenuSeparator() {
  return <DropdownMenu.Separator className="-mx-1 my-1 h-px bg-border" />;
}

// ---- Tooltip ----
export const TooltipProvider = TooltipPrimitive.Provider;

export function Tooltip({ content, children, side = 'right' }: { content: ReactNode; children: ReactNode; side?: 'top' | 'right' | 'bottom' | 'left' }) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          sideOffset={8}
          className="z-50 rounded-md bg-foreground px-2 py-1 text-xs font-medium text-background shadow-pop animate-fade-in"
        >
          {content}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}

// ---- Dialog ----
export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;
export const DialogTitle = DialogPrimitive.Title;
export const DialogDescription = DialogPrimitive.Description;

export const DialogContent = forwardRef<
  ElementRef<typeof DialogPrimitive.Content>,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { hideClose?: boolean; overlayClassName?: string }
>(({ className, children, hideClose, overlayClassName, ...props }, ref) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay className={cn('fixed inset-0 z-50 bg-black/40 backdrop-blur-[2px] animate-fade-in', overlayClassName)} />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        'fixed left-1/2 top-[12vh] z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 rounded-xl border bg-popover text-popover-foreground shadow-pop animate-slide-up focus:outline-none',
        className
      )}
      {...props}
    >
      {children}
      {!hideClose ? (
        <DialogPrimitive.Close className="absolute right-3 top-3 grid size-8 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">
          <X className="size-4" />
          <span className="sr-only">Close</span>
        </DialogPrimitive.Close>
      ) : null}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
DialogContent.displayName = 'DialogContent';
