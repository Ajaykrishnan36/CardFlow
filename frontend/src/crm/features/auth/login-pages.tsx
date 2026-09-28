import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { ArrowRight, Crown, ShieldCheck } from 'lucide-react';
import { LogoMark } from '@crm/components/brand';
import { AuthHeading, AuthLayout } from './auth-layout';
import { LoginForm } from './login-form';

export function useDocumentTitle(title: string) {
  const { t } = useTranslation();
  useEffect(() => {
    document.title = `${title} · ${t('brand.name')}`;
  }, [title, t]);
}

/** /crm/login — the main entry. Members and the platform owner both sign in here (D-12). */
export function LoginPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.login.submit'));
  return (
    <AuthLayout
      footer={
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="size-3.5" aria-hidden />
          {t('auth.login.invitationOnly')}
        </span>
      }
    >
      <AuthHeading title={t('auth.login.title')} subtitle={t('auth.login.subtitle')} />
      <LoginForm audience="workspace" />
      <div className="mt-8 flex flex-col items-start gap-1.5 rounded-lg border border-dashed bg-muted/40 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
        <div className="flex items-center gap-2.5 text-[13px] text-muted-foreground">
          <Crown className="size-4 text-primary" aria-hidden />
          {t('auth.login.ownerPrompt')}
        </div>
        <Link to="/crm/owner/login" className="group inline-flex items-center gap-1 text-[13px] font-medium text-primary hover:underline">
          {t('auth.login.ownerLink')}
          <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden />
        </Link>
      </div>
    </AuthLayout>
  );
}

/** /crm/owner/login — dedicated owner console (PRD §10.3): centred card, dark by design. */
export function OwnerLoginPage() {
  const { t } = useTranslation();
  useDocumentTitle(t('auth.owner.title'));
  return (
    <div className="dark flex min-h-full flex-col">
      <div className="relative flex flex-1 flex-col overflow-hidden bg-[hsl(228_30%_5%)] text-foreground">
        <div className="pointer-events-none absolute left-1/2 top-[-18rem] size-[46rem] -translate-x-1/2 rounded-full bg-indigo-600/25 blur-3xl" aria-hidden />
        <div className="crm-grid-bg pointer-events-none absolute inset-0 opacity-60" aria-hidden />

        <main className="relative flex flex-1 items-center justify-center px-5 py-12">
          <div className="w-full max-w-[420px] animate-slide-up">
            <div className="mb-8 flex flex-col items-center text-center">
              <div className="relative">
                <LogoMark className="size-12 shadow-glow" />
                <span className="absolute -bottom-1.5 -right-1.5 grid size-6 place-items-center rounded-full border-2 border-[hsl(228_30%_5%)] bg-amber-400 text-amber-950">
                  <Crown className="size-3" aria-hidden />
                </span>
              </div>
              <p className="mt-5 text-xs font-semibold uppercase tracking-[0.2em] text-indigo-300">{t('brand.name')}</p>
              <h1 className="mt-2 text-[26px] font-semibold tracking-tight">{t('auth.owner.title')}</h1>
              <p className="mt-2 text-sm text-muted-foreground">{t('auth.owner.subtitle')}</p>
            </div>

            <div className="rounded-2xl border bg-card/80 p-7 shadow-2xl backdrop-blur-xl sm:p-8">
              <LoginForm audience="owner" />
            </div>

            <div className="mt-6 flex flex-col items-center gap-3 text-center">
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <ShieldCheck className="size-3.5" aria-hidden />
                {t('auth.owner.restricted')}
              </p>
              <Link to="/crm/login" className="text-[13px] font-medium text-indigo-300 hover:underline">
                ← {t('auth.owner.workspaceLink')}
              </Link>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
}
