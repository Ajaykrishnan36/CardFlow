import { useEffect, useRef, useState } from 'react';
import { ArrowDown, Check, PhoneCall } from 'lucide-react';
import heroMp4 from '../../assets/videos/hero.mp4';
import heroWebm from '../../assets/videos/hero.webm';
import heroPoster from '../../assets/videos/hero-poster.webp';
import { reducedMotion } from '../../hooks/useInView';

/**
 * First impression: the headline for owners still on paper, and the hero video: a quotation
 * written by hand loses the customer; the same lead in Cogniz CRM wins him; the owner relaxes.
 * The video is muted, loops, pauses off screen, and is a still image when motion is reduced.
 */
export function Hero({ onBookCall }: { onBookCall: () => void }) {
  const video = useRef<HTMLVideoElement>(null);
  const [still] = useState(reducedMotion);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const el = video.current;
    if (!el || still || !('IntersectionObserver' in window)) return;
    const io = new IntersectionObserver(([e]) => {
      if (e!.isIntersecting) void el.play().catch(() => setFailed(true));
      else el.pause();
    });
    io.observe(el);
    return () => io.disconnect();
  }, [still]);

  return (
    <section className="cw-hero" id="top" aria-labelledby="cw-hero-title">
      <div className="cw-hero-bg" aria-hidden>
        <span className="cw-blob b1" />
        <span className="cw-blob b2" />
        <span className="cw-blob b3" />
        <span className="cw-grid-lines" />
      </div>
      <div className="cw-wrap cw-hero-grid">
        <div className="cw-hero-copy">
          <p className="cw-pill" data-reveal>
            <span className="cw-live" aria-hidden /> CRM for growing businesses
          </p>
          <h1 id="cw-hero-title" data-reveal>
            Stop managing leads on paper. <span className="cw-grad">Start growing your business with CRM.</span>
          </h1>
          <p className="cw-hero-lead" data-reveal>
            Every enquiry matters. Every follow-up counts. Bring your leads, customers, quotations, team, sales and business operations into one organised system.
          </p>
          <div className="cw-actions" data-reveal>
            <button type="button" className="cw-btn cw-btn-primary cw-btn-lg" onClick={onBookCall}>
              <PhoneCall aria-hidden /> Book a free call
            </button>
            <a className="cw-btn cw-btn-outline cw-btn-lg" href="#journey">
              Explore how it works <ArrowDown aria-hidden />
            </a>
          </div>
          <ul className="cw-ticks" data-reveal>
            <li>
              <Check aria-hidden /> Start with your mobile number
            </li>
            <li>
              <Check aria-hidden /> Your own private business address
            </li>
            <li>
              <Check aria-hidden /> Works on phone and computer
            </li>
          </ul>
        </div>

        <div className="cw-hero-stage" data-reveal>
          <div className="cw-hero-video">
            {still || failed ? (
              <img src={heroPoster} alt="A salesperson writes a quotation by hand and sends a photo the customer cannot read, so the customer leaves. With Cogniz CRM he adds the lead and sends a clear quotation, and the customer confirms. The owner relaxes, adds a team member and checks the business in one click" width={1280} height={800} />
            ) : (
              <video ref={video} muted loop playsInline autoPlay preload="metadata" poster={heroPoster} aria-label="Animation: a salesperson writes a quotation by hand and sends a photo the customer cannot read, so the customer leaves. With Cogniz CRM he adds the lead and sends a clear quotation, and the customer confirms. The owner relaxes, adds a team member and checks the business in one click" onError={() => setFailed(true)}>
                <source src={heroWebm} type="video/webm" />
                <source src={heroMp4} type="video/mp4" />
              </video>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
