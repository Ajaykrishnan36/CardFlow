import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '@crm/lib/utils';

/** Brand mark: stacked "records" glyph in the primary colour. */
export function LogoMark({ className }: { className?: string }) {
  // Unique per instance: a shared gradient id breaks when its first copy sits in a
  // display:none subtree (e.g. the desktop sidebar while the mobile drawer is open).
  const gradientId = `crm-logo-${useId().replace(/:/g, '')}`;
  return (
    <svg viewBox="0 0 32 32" className={cn('size-8', className)} aria-hidden>
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
          <stop stopColor="#818CF8" />
          <stop offset="1" stopColor="#4F46E5" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill={`url(#${gradientId})`} />
      <path d="M9 11.5c0-1.1.9-2 2-2h10a2 2 0 0 1 0 4H11a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".95" />
      <path d="M9 16c0-1.1.9-2 2-2h6.5a2 2 0 0 1 0 4H11a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".75" />
      <path d="M9 20.5c0-1.1.9-2 2-2h3a2 2 0 0 1 0 4h-3a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".55" />
      <circle cx="21.5" cy="20.5" r="2.5" fill="#fff" />
    </svg>
  );
}

export function Logo({ className, subtitle, inverted }: { className?: string; subtitle?: string; inverted?: boolean }) {
  const { t } = useTranslation();
  return (
    <div className={cn('flex items-center gap-2.5', className)}>
      <LogoMark />
      <div className="leading-tight">
        <p className={cn('text-[15px] font-semibold tracking-tight', inverted ? 'text-white' : 'text-foreground')}>{t('brand.name')}</p>
        {subtitle ? <p className={cn('text-xs', inverted ? 'text-white/60' : 'text-muted-foreground')}>{subtitle}</p> : null}
      </div>
    </div>
  );
}
