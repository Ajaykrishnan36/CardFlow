import type { HTMLAttributes, ReactNode } from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { AlertCircle, CheckCircle2, Info, TriangleAlert } from 'lucide-react';
import { cn } from '@crm/lib/utils';

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('rounded-lg border bg-card text-card-foreground shadow-card', className)} {...props} />;
}

export function CardHeader({ title, description, actions, className }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4', className)}>
      <div className="min-w-0">
        <h2 className="text-[15px] font-semibold leading-6 text-foreground">{title}</h2>
        {description ? <p className="mt-0.5 text-[13px] text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}

const badgeVariants = cva('inline-flex items-center gap-1 whitespace-nowrap rounded-full px-2 py-0.5 text-[11px] font-semibold leading-4', {
  variants: {
    tone: {
      neutral: 'bg-muted text-muted-foreground',
      primary: 'bg-primary-soft text-primary',
      success: 'bg-success-soft text-success',
      warning: 'bg-warning-soft text-warning',
      danger: 'bg-danger-soft text-danger'
    }
  },
  defaultVariants: { tone: 'neutral' }
});

export function Badge({ tone, className, ...props }: HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ tone }), className)} {...props} />;
}

const alertVariants = cva('flex gap-3 rounded-lg border px-3.5 py-3 text-[13px] leading-5', {
  variants: {
    tone: {
      info: 'border-primary/20 bg-primary-soft text-foreground [&_svg]:text-primary',
      success: 'border-success/25 bg-success-soft text-foreground [&_svg]:text-success',
      warning: 'border-warning/30 bg-warning-soft text-foreground [&_svg]:text-warning',
      danger: 'border-danger/25 bg-danger-soft text-foreground [&_svg]:text-danger'
    }
  },
  defaultVariants: { tone: 'info' }
});

const alertIcons = { info: Info, success: CheckCircle2, warning: TriangleAlert, danger: AlertCircle };

export function Alert({ tone = 'info', title, children, className }: VariantProps<typeof alertVariants> & { title?: ReactNode; children?: ReactNode; className?: string }) {
  const Icon = alertIcons[tone ?? 'info'];
  return (
    <div role={tone === 'danger' || tone === 'warning' ? 'alert' : 'status'} className={cn(alertVariants({ tone }), 'animate-slide-up', className)}>
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden />
      <div className="min-w-0 space-y-0.5">
        {title ? <p className="font-semibold">{title}</p> : null}
        {children ? <div className="text-muted-foreground [&_a]:font-medium [&_a]:text-primary">{children}</div> : null}
      </div>
    </div>
  );
}
