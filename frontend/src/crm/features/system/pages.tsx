import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Compass, Hammer, Lock, RefreshCw, ServerCrash } from 'lucide-react';
import { Button } from '@crm/components/ui/button';
import { LogoMark } from '@crm/components/brand';
import type { LucideIcon } from 'lucide-react';

function Centered({ icon: Icon, title, body, children }: { icon: LucideIcon; title: string; body: string; children?: React.ReactNode }) {
  return (
    <div className="grid min-h-full place-items-center px-6 py-16">
      <div className="flex max-w-md flex-col items-center text-center animate-slide-up">
        <div className="mb-5 grid size-14 place-items-center rounded-2xl border bg-card text-primary shadow-card">
          <Icon className="size-7" aria-hidden />
        </div>
        <h1 className="text-xl font-semibold tracking-tight text-foreground">{title}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{body}</p>
        {children ? <div className="mt-6 flex gap-2">{children}</div> : null}
      </div>
    </div>
  );
}

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <Centered icon={Compass} title={t('system.notFoundTitle')} body={t('system.notFoundBody')}>
      <Button asChild>
        <Link to="/crm">{t('system.goHome')}</Link>
      </Button>
    </Centered>
  );
}

export function NoAccessPage() {
  const { t } = useTranslation();
  return (
    <Centered icon={Lock} title={t('system.noAccessTitle')} body={t('system.noAccessBody')}>
      <Button asChild variant="outline">
        <Link to="/crm">{t('system.goHome')}</Link>
      </Button>
    </Centered>
  );
}

export function ComingSoonPage({ name, backTo }: { name: string; backTo: string }) {
  const { t } = useTranslation();
  return (
    <Centered icon={Hammer} title={t('system.comingSoonTitle', { name })} body={t('system.comingSoonBody')}>
      <Button asChild variant="outline">
        <Link to={backTo}>{t('system.backToOverview')}</Link>
      </Button>
    </Centered>
  );
}

export function UnavailablePage({ onRetry }: { onRetry?: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-full flex-col">
      <div className="p-6">
        <LogoMark />
      </div>
      <div className="flex-1">
        <Centered icon={ServerCrash} title={t('system.unavailableTitle')} body={t('system.unavailableBody')}>
          <Button variant="outline" onClick={() => (onRetry ? onRetry() : window.location.reload())}>
            <RefreshCw /> {t('common.retry')}
          </Button>
        </Centered>
      </div>
    </div>
  );
}
