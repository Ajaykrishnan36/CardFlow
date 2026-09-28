import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { RefreshCw, type LucideIcon } from 'lucide-react';
import { Button } from './ui/button';
import { Spinner } from './ui/spinner';
import { LogoMark } from './brand';
import { cn } from '@crm/lib/utils';

export function FullPageLoader() {
  const { t } = useTranslation();
  return (
    <div className="grid min-h-full place-items-center bg-background p-6">
      <div className="flex flex-col items-center gap-4 animate-fade-in">
        <LogoMark className="size-10 animate-float" />
        <Spinner className="size-5 text-muted-foreground" label={t('common.loading')} />
      </div>
    </div>
  );
}

export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
  className
}: {
  icon: LucideIcon;
  title: ReactNode;
  body?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn('flex flex-col items-center px-6 py-12 text-center', className)}>
      <div className="mb-4 grid size-12 place-items-center rounded-xl bg-primary-soft text-primary">
        <Icon className="size-6" aria-hidden />
      </div>
      <h3 className="text-[15px] font-semibold text-foreground">{title}</h3>
      {body ? <p className="mt-1 max-w-sm text-[13px] text-muted-foreground">{body}</p> : null}
      {action ? <div className="mt-5">{action}</div> : null}
    </div>
  );
}

export function ErrorState({ title, message, onRetry, requestId }: { title: ReactNode; message?: string; onRetry?: () => void; requestId?: string }) {
  const { t } = useTranslation();
  return (
    <div role="alert" className="flex flex-col items-center px-6 py-12 text-center">
      <h3 className="text-[15px] font-semibold text-foreground">{title}</h3>
      <p className="mt-1 max-w-sm text-[13px] text-muted-foreground">{message ?? t('common.genericError')}</p>
      {requestId ? <p className="mt-1 font-mono text-[11px] text-muted-foreground/70">{t('common.requestId', { id: requestId })}</p> : null}
      {onRetry ? (
        <Button variant="outline" size="sm" className="mt-4" onClick={onRetry}>
          <RefreshCw /> {t('common.retry')}
        </Button>
      ) : null}
    </div>
  );
}
