import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { cn } from '@crm/lib/utils';

/**
 * A panel anchored to a trigger, for small forms (filters, sorts, columns). Closes on
 * Escape or a click outside; stays inside the viewport.
 */
export function Popover({
  trigger,
  children,
  open,
  onOpenChange,
  align = 'start',
  className,
  width = 360
}: {
  trigger: (props: { ref: (el: HTMLElement | null) => void; onClick: () => void; 'aria-expanded': boolean }) => ReactNode;
  children: ReactNode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  align?: 'start' | 'end';
  className?: string;
  width?: number;
}) {
  const anchor = useRef<HTMLElement | null>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number; maxHeight: number } | null>(null);

  const place = useCallback(() => {
    const el = anchor.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const w = Math.min(width, window.innerWidth - 16);
    let left = align === 'end' ? r.right - w : r.left;
    left = Math.max(8, Math.min(left, window.innerWidth - w - 8));
    const top = r.bottom + 6;
    setPos({ top, left, maxHeight: Math.max(160, window.innerHeight - top - 12) });
  }, [align, width]);

  useLayoutEffect(() => {
    if (open) place();
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (panel.current?.contains(t) || anchor.current?.contains(t)) return;
      // Clicks inside other popups (a select list, a lookup) belong to us too.
      if ((t as HTMLElement).closest?.('[data-popover-keep]')) return;
      onOpenChange(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onOpenChange(false);
    };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => {
      window.removeEventListener('mousedown', onDown);
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', place, true);
    };
  }, [open, onOpenChange, place]);

  return (
    <>
      {trigger({ ref: (el) => (anchor.current = el), onClick: () => onOpenChange(!open), 'aria-expanded': open })}
      {open && pos
        ? createPortal(
            <div
              ref={panel}
              role="dialog"
              style={{ top: pos.top, left: pos.left, width: Math.min(width, window.innerWidth - 16), maxHeight: pos.maxHeight }}
              className={cn('fixed z-50 overflow-auto rounded-lg border bg-popover p-3 text-popover-foreground shadow-pop animate-slide-up', className)}
            >
              {children}
            </div>,
            document.body
          )
        : null}
    </>
  );
}
