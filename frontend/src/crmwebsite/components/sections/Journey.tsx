import { useEffect, useRef, useState } from 'react';
import { SectionHead } from '../common/SectionHead';
import { ScreenFrame } from '../common/ScreenFrame';
import { journey, screens } from '../../data/content';

/**
 * One CRM for the whole business: thirteen steps from enquiry to repeat business. The step
 * in the middle of the screen lights up and the real CRM screen for it shows beside it.
 */
export function Journey() {
  const [at, setAt] = useState(0);
  const list = useRef<HTMLOListElement>(null);
  useEffect(() => {
    const items = Array.from(list.current?.querySelectorAll<HTMLElement>('[data-step]') ?? []);
    if (!items.length || !('IntersectionObserver' in window)) return;
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) if (e.isIntersecting) setAt(Number((e.target as HTMLElement).dataset.step));
      },
      { rootMargin: '-45% 0px -45% 0px' }
    );
    items.forEach((i) => io.observe(i));
    return () => io.disconnect();
  }, []);
  const step = journey[at]!;
  return (
    <section className="cw-sec cw-sec-dark" id="journey" aria-labelledby="cw-journey-title">
      <div className="cw-wrap">
        <SectionHead
          light
          eyebrow="One CRM for your entire business"
          title={<span id="cw-journey-title">From the first enquiry to repeat business, on one road</span>}
          text="The same customer moves forward step by step. Nothing is typed twice and nothing falls through the cracks."
        />
        <div className="cw-journey">
          <ol ref={list} className="cw-steps" style={{ ['--p' as string]: String(at / (journey.length - 1)) }}>
            {journey.map((s, i) => {
              const Icon = s.icon;
              return (
                <li key={s.title} data-step={i} className={i === at ? 'is-on' : i < at ? 'is-done' : ''}>
                  <button type="button" onClick={() => setAt(i)} aria-current={i === at ? 'step' : undefined}>
                    <span className="cw-step-ic">
                      <Icon aria-hidden />
                    </span>
                    <span className="cw-step-txt">
                      <small>Step {i + 1}</small>
                      <b>{s.title}</b>
                      <span>{s.text}</span>
                    </span>
                  </button>
                </li>
              );
            })}
          </ol>
          <div className="cw-journey-view">
            <div className="cw-journey-sticky">
              <p className="cw-journey-now" key={step.title}>
                <span>{at + 1}</span> {step.title}
              </p>
              <div className="cw-journey-screen" key={step.screen + at}>
                <ScreenFrame src={screens[step.screen]} alt={`${step.title}: ${step.text} (real CRM screen, sample data)`} />
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
