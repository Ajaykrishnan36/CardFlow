import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Boxes, KeyRound, ShieldCheck, Sparkles } from 'lucide-react';
import { Logo } from '@crm/components/brand';
import { ThemeToggle } from '@crm/features/shell/theme-toggle';
import { cn } from '@crm/lib/utils';

/**
 * Sign-in frame. ≥1024px: brand panel + form side by side (the "desktop-first"
 * experience). 768–1023px: centred card on a tinted canvas. <768px: full-width form.
 */
export function AuthLayout({ children, footer }: { children: ReactNode; footer?: ReactNode }) {
  const { t } = useTranslation();
  const year = new Date().getFullYear();
  return (
    <div className="flex min-h-full flex-col bg-background lg:grid lg:h-full lg:grid-cols-[minmax(0,1.08fr)_minmax(0,1fr)] 2xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
      <BrandPanel />
      <div className="crm-scroll relative flex flex-1 flex-col bg-background md:bg-surface md:bg-[radial-gradient(ellipse_at_top,hsl(var(--primary)/0.08),transparent_60%)] lg:h-full lg:min-h-0 lg:overflow-y-auto lg:bg-background lg:bg-none">
        <header className="flex items-center justify-between px-5 pt-5 sm:px-8 sm:pt-6">
          <Logo className="lg:invisible" />
          <ThemeToggle />
        </header>
        <main className="flex flex-1 items-center justify-center px-5 py-8 sm:px-8 sm:py-12">
          <div className="w-full max-w-[420px] animate-slide-up md:rounded-2xl md:border md:bg-card md:p-9 md:shadow-card lg:max-w-[400px] lg:border-0 lg:bg-transparent lg:p-0 lg:shadow-none">
            {children}
          </div>
        </main>
        <footer className="flex flex-wrap items-center justify-between gap-2 px-5 pb-5 text-xs text-muted-foreground sm:px-8 sm:pb-6">
          <span>
            © {year} {t('brand.name')}
          </span>
          {footer}
        </footer>
      </div>
    </div>
  );
}

function BrandPanel() {
  const { t } = useTranslation();
  const features = [
    { icon: Boxes, title: t('auth.brand.f1Title'), body: t('auth.brand.f1Body') },
    { icon: KeyRound, title: t('auth.brand.f2Title'), body: t('auth.brand.f2Body') },
    { icon: ShieldCheck, title: t('auth.brand.f3Title'), body: t('auth.brand.f3Body') }
  ];
  return (
    <aside className="relative hidden h-full overflow-hidden bg-[#1E1B4B] text-white lg:flex lg:flex-col">
      {/* Layered backdrop: gradient, glow blobs, fading grid. */}
      <div className="absolute inset-0 bg-[linear-gradient(135deg,#312E81_0%,#1E1B4B_45%,#0B0A24_100%)]" aria-hidden />
      <div className="absolute -left-24 -top-24 size-[28rem] rounded-full bg-indigo-500/30 blur-3xl" aria-hidden />
      <div className="absolute -bottom-32 right-[-6rem] size-[26rem] rounded-full bg-violet-500/20 blur-3xl" aria-hidden />
      <div className="crm-grid-bg absolute inset-0" aria-hidden />

      <div className="relative flex min-h-0 flex-1 flex-col px-12 py-10 xl:px-16 xl:py-12">
        <Logo inverted />

        <div className="my-auto max-w-xl py-10">
          <span className="inline-flex items-center gap-1.5 rounded-full border border-white/15 bg-white/5 px-3 py-1 text-xs font-medium text-indigo-100 backdrop-blur">
            <Sparkles className="size-3.5" aria-hidden />
            {t('auth.brand.eyebrow')}
          </span>
          <h1 className="mt-5 text-[34px] font-semibold leading-[1.15] tracking-tight xl:text-[42px]">{t('auth.brand.headline')}</h1>
          <p className="mt-4 max-w-lg text-[15px] leading-relaxed text-indigo-100/75">{t('auth.brand.sub')}</p>

          <ul className="mt-9 space-y-5">
            {features.map(({ icon: Icon, title, body }) => (
              <li key={title} className="flex gap-4">
                <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-white/10 bg-white/[0.07] shadow-inner">
                  <Icon className="size-5 text-indigo-200" aria-hidden />
                </span>
                <div>
                  <p className="text-[15px] font-semibold">{title}</p>
                  <p className="mt-0.5 text-sm leading-relaxed text-indigo-100/65">{body}</p>
                </div>
              </li>
            ))}
          </ul>
        </div>

        <PipelinePreview />

        <p className="mt-8 text-xs text-indigo-100/50">{t('auth.brand.trust')}</p>
      </div>
    </aside>
  );
}

/** Abstract, decorative board — no fake names or numbers. */
function PipelinePreview() {
  const { t } = useTranslation();
  const columns = [
    { label: t('auth.brand.previewNew'), cards: [0.9, 0.62, 0.78], dot: 'bg-sky-300' },
    { label: t('auth.brand.previewQualified'), cards: [0.7, 0.85], dot: 'bg-amber-300' },
    { label: t('auth.brand.previewWon'), cards: [0.8], dot: 'bg-emerald-300' }
  ];
  return (
    <div className="relative hidden rounded-2xl border border-white/10 bg-white/[0.06] p-4 shadow-2xl backdrop-blur-md [@media(min-height:1000px)]:xl:block" aria-hidden>
      <div className="mb-3 flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-indigo-100/60">{t('auth.brand.previewTitle')}</span>
        <span className="flex gap-1">
          <span className="size-2 rounded-full bg-white/20" />
          <span className="size-2 rounded-full bg-white/20" />
          <span className="size-2 rounded-full bg-white/20" />
        </span>
      </div>
      <div className="grid grid-cols-3 gap-3">
        {columns.map((col, ci) => (
          <div key={col.label} className="rounded-xl bg-black/15 p-2.5">
            <p className="mb-2 flex items-center gap-1.5 text-[11px] font-medium text-indigo-100/70">
              <span className={cn('size-1.5 rounded-full', col.dot)} />
              {col.label}
            </p>
            <div className="space-y-2">
              {col.cards.map((w, i) => (
                <div
                  key={i}
                  className="animate-float rounded-lg border border-white/10 bg-white/[0.08] p-2"
                  style={{ animationDelay: `${(ci * 3 + i) * 0.7}s` }}
                >
                  <div className="flex items-center gap-1.5">
                    <span className="size-4 shrink-0 rounded-full bg-gradient-to-br from-indigo-300 to-violet-400" />
                    <span className="h-1.5 rounded-full bg-white/50" style={{ width: `${w * 100}%` }} />
                  </div>
                  <span className="mt-2 block h-1.5 w-2/3 rounded-full bg-white/20" />
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/** Small heading block used by every auth screen. */
export function AuthHeading({ title, subtitle, icon }: { title: ReactNode; subtitle?: ReactNode; icon?: ReactNode }) {
  return (
    <div className="mb-7">
      {icon ? <div className="mb-5">{icon}</div> : null}
      <h1 className="text-[26px] font-semibold leading-tight tracking-tight text-foreground">{title}</h1>
      {subtitle ? <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{subtitle}</p> : null}
    </div>
  );
}
