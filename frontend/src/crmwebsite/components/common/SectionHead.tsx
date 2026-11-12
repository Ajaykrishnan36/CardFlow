import type { ReactNode } from 'react';

export function SectionHead({ eyebrow, title, text, center = true, light = false }: { eyebrow: string; title: ReactNode; text?: ReactNode; center?: boolean; light?: boolean }) {
  return (
    <header className={`cw-head${center ? ' is-center' : ''}${light ? ' is-light' : ''}`}>
      <p className="cw-eyebrow" data-reveal>
        <span className="cw-eyebrow-dot" aria-hidden />
        {eyebrow}
      </p>
      <h2 data-reveal>{title}</h2>
      {text ? (
        <p className="cw-lead" data-reveal>
          {text}
        </p>
      ) : null}
    </header>
  );
}
