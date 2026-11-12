import { useId } from 'react';

/** The Cogniz CRM mark and name, as the app shows them. */
export function Logo({ light = false }: { light?: boolean }) {
  const id = `cw-logo-${useId().replace(/:/g, '')}`;
  return (
    <span className={`cw-brand${light ? ' is-light' : ''}`}>
      <svg viewBox="0 0 32 32" className="cw-brand-mark" aria-hidden>
        <defs>
          <linearGradient id={id} x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
            <stop stopColor="#2DD4BF" />
            <stop offset="1" stopColor="#0F766E" />
          </linearGradient>
        </defs>
        <rect width="32" height="32" rx="8" fill={`url(#${id})`} />
        <path d="M9 11.5c0-1.1.9-2 2-2h10a2 2 0 0 1 0 4H11a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".95" />
        <path d="M9 16c0-1.1.9-2 2-2h6.5a2 2 0 0 1 0 4H11a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".75" />
        <path d="M9 20.5c0-1.1.9-2 2-2h3a2 2 0 0 1 0 4h-3a2 2 0 0 1-2-2Z" fill="#fff" fillOpacity=".55" />
        <circle cx="21.5" cy="20.5" r="2.5" fill="#fff" />
      </svg>
      <span>
        Cogniz <b>CRM</b>
      </span>
    </span>
  );
}
