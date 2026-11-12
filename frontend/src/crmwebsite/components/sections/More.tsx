import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, Check, ChevronDown, MessageSquare, PhoneCall } from 'lucide-react';
import { SectionHead } from '../common/SectionHead';
import { ScreenFrame } from '../common/ScreenFrame';
import { RequestForm } from '../forms/RequestForm';
import { benefits, faqs, industries, roles, screens, showcase, teamSteps } from '../../data/content';
import { useInView, reducedMotion } from '../../hooks/useInView';
import { getPlans, type Plan } from '../../services/website-api';

/** How a team works a lead: ten steps that light up in turn, and what each role sees. */
export function TeamFlow() {
  const [ref, inView] = useInView<HTMLOListElement>(0.3);
  const [step, setStep] = useState(-1);
  const [role, setRole] = useState(0);
  useEffect(() => {
    if (!inView) return;
    if (reducedMotion()) return setStep(teamSteps.length - 1);
    let i = 0;
    const t = setInterval(() => {
      setStep(i);
      i += 1;
      if (i >= teamSteps.length) clearInterval(t);
    }, 520);
    return () => clearInterval(t);
  }, [inView]);
  const r = roles[role]!;
  const RoleIcon = r.icon;
  return (
    <section className="cw-sec cw-sec-tint" id="team" aria-labelledby="cw-team-title">
      <div className="cw-wrap">
        <SectionHead
          eyebrow="How your team works with your leads"
          title={<span id="cw-team-title">You see every lead. Your team follows every one.</span>}
          text="From the enquiry to the final answer, everyone knows what to do next, and you can see it happening."
        />
        <ol ref={ref} className="cw-team-flow">
          {teamSteps.map((s, i) => (
            <li key={i} className={i <= step ? 'is-on' : ''}>
              <span className="cw-team-n">{i + 1}</span>
              <span className={`cw-who who-${s.who.split(' ')[0]!.toLowerCase()}`}>{s.who}</span>
              <p>{s.text}</p>
            </li>
          ))}
        </ol>
        <div className="cw-roles" data-reveal>
          <div className="cw-role-tabs" role="tablist" aria-label="Views by role">
            {roles.map((x, i) => {
              const I = x.icon;
              return (
                <button key={x.role} role="tab" type="button" aria-selected={i === role} className={i === role ? 'is-on' : ''} onClick={() => setRole(i)}>
                  <I aria-hidden /> {x.role}
                </button>
              );
            })}
          </div>
          <div className="cw-role-body" role="tabpanel" key={r.role}>
            <div className="cw-role-copy">
              <h3>
                <RoleIcon aria-hidden /> {r.role} sees
              </h3>
              <ul className="cw-checks">
                {r.sees.map((x) => (
                  <li key={x}>
                    <Check aria-hidden /> {x}
                  </li>
                ))}
              </ul>
              <p className="cw-note">What each person sees and can change depends on the role and permissions you give them.</p>
            </div>
            <ScreenFrame src={screens[r.screen]} alt={`What a ${r.role.toLowerCase()} works with in the CRM (sample data)`} />
          </div>
        </div>
      </div>
    </section>
  );
}

/** Many trades, one CRM: pick an industry and its typical flow and use case appear. */
export function Industries() {
  const [at, setAt] = useState(0);
  const ind = industries[at]!;
  const Icon = ind.icon;
  return (
    <section className="cw-sec" id="industries" aria-labelledby="cw-ind-title">
      <div className="cw-wrap">
        <SectionHead
          eyebrow="Designed for different industries"
          title={<span id="cw-ind-title">It speaks the language of your business</span>}
          text="Choose your industry when you sign up and the CRM comes with the right fields and words for your trade."
        />
        <div className="cw-ind">
          <div className="cw-ind-list" role="tablist" aria-label="Industries" data-reveal>
            {industries.map((x, i) => {
              const I = x.icon;
              return (
                <button key={x.key} type="button" role="tab" aria-selected={i === at} className={i === at ? 'is-on' : ''} onClick={() => setAt(i)}>
                  <I aria-hidden /> {x.label}
                </button>
              );
            })}
          </div>
          <div className="cw-ind-card" role="tabpanel" key={ind.key}>
            <span className="cw-ind-ic">
              <Icon aria-hidden />
            </span>
            <h3>{ind.label}</h3>
            <p>{ind.text}</p>
            <ol className="cw-ind-flow">
              {ind.flow.map((f, i) => (
                <li key={f} style={{ animationDelay: `${i * 110}ms` }}>
                  <span>{i + 1}</span>
                  {f}
                </li>
              ))}
            </ol>
            <p className="cw-note">Example flow. You can rename stages and fields to match how you work.</p>
          </div>
        </div>
      </div>
    </section>
  );
}

export function Benefits() {
  return (
    <section className="cw-sec cw-sec-dark" id="why" aria-labelledby="cw-why-title">
      <div className="cw-wrap">
        <SectionHead
          light
          eyebrow="Why business owners choose a CRM"
          title={<span id="cw-why-title">Your business should not depend on memory. It should run on a system.</span>}
        />
        <ul className="cw-benefits">
          {benefits.map((b) => {
            const I = b.icon;
            return (
              <li key={b.title} data-reveal>
                <span>
                  <I aria-hidden />
                </span>
                {b.title}
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}

/** The product, screen by screen: real CRM pages with sample data. */
export function Showcase() {
  const [at, setAt] = useState(0);
  const item = showcase[at]!;
  return (
    <section className="cw-sec" id="product" aria-labelledby="cw-show-title">
      <div className="cw-wrap">
        <SectionHead eyebrow="Inside the product" title={<span id="cw-show-title">Take a look around</span>} text="Real screens of Cogniz CRM. The numbers and names are demonstration data." />
        <div className="cw-show-tabs" role="tablist" aria-label="Screens" data-reveal>
          {showcase.map((s, i) => (
            <button key={s.key} type="button" role="tab" aria-selected={i === at} className={i === at ? 'is-on' : ''} onClick={() => setAt(i)}>
              {s.label}
            </button>
          ))}
        </div>
        <div className="cw-show-stage" data-reveal>
          <div className="cw-show-screen" key={item.key}>
            <ScreenFrame src={screens[item.key]} alt={`${item.label} screen in Cogniz CRM (sample data)`} label={`Cogniz CRM · ${item.label}`} />
          </div>
        </div>
      </div>
    </section>
  );
}

export function Pricing() {
  const [plans, setPlans] = useState<Plan[]>([]);
  useEffect(() => {
    void getPlans().then(setPlans).catch(() => setPlans([]));
  }, []);
  if (!plans.length) return null;
  const names: Record<string, string> = { '1m': '1 month', '3m': '3 months', '6m': '6 months', '12m': '12 months' };
  return (
    <section className="cw-sec cw-sec-tint" id="pricing" aria-labelledby="cw-price-title">
      <div className="cw-wrap">
        <SectionHead eyebrow="Simple pricing" title={<span id="cw-price-title">Pay per person. Nothing to pay today.</span>} text="Create your CRM free. You pay only once your business is approved." />
        <div className="cw-plans">
          {plans.map((p) => (
            <article key={p.key} className={`cw-plan${p.key === '12m' ? ' is-best' : ''}`} data-reveal>
              {p.key === '12m' ? <span className="cw-plan-tag">Best value</span> : null}
              <h3>{names[p.key] ?? p.key}</h3>
              <p className="cw-plan-price">₹{p.price.toLocaleString('en-IN')}</p>
              <small>per person</small>
              <p className="cw-plan-per">about ₹{Math.round(p.price / (p.months || 1)).toLocaleString('en-IN')} a month</p>
              <Link className={`cw-btn ${p.key === '12m' ? 'cw-btn-primary' : 'cw-btn-outline'}`} to="/signup">
                Start free
              </Link>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

/** The two working forms: a free call (with a preferred time) and a general enquiry. */
export function Contact({ tab, setTab }: { tab: 'call' | 'enquiry'; setTab: (t: 'call' | 'enquiry') => void }) {
  return (
    <section className="cw-sec cw-sec-cta" id="contact" aria-labelledby="cw-contact-title">
      <div className="cw-wrap cw-contact">
        <div className="cw-contact-copy">
          <p className="cw-eyebrow is-light" data-reveal>
            <span className="cw-eyebrow-dot" aria-hidden />
            Book a free call
          </p>
          <h2 id="cw-contact-title" data-reveal>
            Let&apos;s understand your business and show you how a CRM can help
          </h2>
          <p className="cw-lead" data-reveal>
            Tell us a little about your business. Our team will connect with you, understand your current process, and show you how to manage your leads, follow-ups, customers and sales in one place.
          </p>
          <ul className="cw-contact-steps">
            <li data-reveal>
              <span>1</span>
              <b>You ask</b> for a call at a time that suits you
            </li>
            <li data-reveal>
              <span>2</span>
              <b>We confirm</b> the time by phone or message
            </li>
            <li data-reveal>
              <span>3</span>
              <b>We show you</b> the CRM set up for your trade
            </li>
          </ul>
        </div>
        <div className="cw-contact-card" data-reveal>
          <div className="cw-contact-tabs" role="tablist" aria-label="Contact us">
            <button type="button" role="tab" aria-selected={tab === 'call'} className={tab === 'call' ? 'is-on' : ''} onClick={() => setTab('call')}>
              <PhoneCall aria-hidden /> Book a free call
            </button>
            <button type="button" role="tab" id="cw-enquiry" aria-selected={tab === 'enquiry'} className={tab === 'enquiry' ? 'is-on' : ''} onClick={() => setTab('enquiry')}>
              <MessageSquare aria-hidden /> Send an enquiry
            </button>
          </div>
          <div role="tabpanel">
            <p className="cw-contact-sub">{tab === 'call' ? 'Pick a day and time. We will confirm it with you before calling.' : 'Not ready for a call? Ask us anything and we will reply.'}</p>
            {tab === 'call' ? <RequestForm key="call" kind="call" idPrefix="cw-call" /> : <RequestForm key="enquiry" kind="enquiry" idPrefix="cw-enq" />}
          </div>
        </div>
      </div>
    </section>
  );
}

export function Faq({ onEnquiry }: { onEnquiry: () => void }) {
  const [open, setOpen] = useState<number | null>(0);
  return (
    <section className="cw-sec" id="faq" aria-labelledby="cw-faq-title">
      <div className="cw-wrap cw-narrow">
        <SectionHead eyebrow="Questions" title={<span id="cw-faq-title">Before you leave the notebook behind</span>} />
        <div className="cw-faq">
          {faqs.map((f, i) => (
            <div key={f.q} className={`cw-faq-item${open === i ? ' is-open' : ''}`} data-reveal>
              <h3>
                <button type="button" aria-expanded={open === i} aria-controls={`cw-faq-${i}`} onClick={() => setOpen(open === i ? null : i)}>
                  {f.q}
                  <ChevronDown aria-hidden />
                </button>
              </h3>
              <div className="cw-faq-a" id={`cw-faq-${i}`} role="region">
                <div>
                  <p>{f.a}</p>
                </div>
              </div>
            </div>
          ))}
        </div>
        <p className="cw-faq-more" data-reveal>
          Have a question we did not answer?{' '}
          <button type="button" className="cw-link-btn" onClick={onEnquiry}>
            Send us an enquiry <ArrowRight aria-hidden />
          </button>
        </p>
      </div>
    </section>
  );
}

export function FinalCta({ onBookCall, onEnquiry }: { onBookCall: () => void; onEnquiry: () => void }) {
  return (
    <section className="cw-final" aria-labelledby="cw-final-title">
      <div className="cw-wrap cw-final-in" data-reveal>
        <h2 id="cw-final-title">Your leads are valuable. Don&apos;t let them get lost.</h2>
        <p>Move from manual tracking to a connected business system. Bring your leads, customers, sales team, quotations and follow-ups together.</p>
        <div className="cw-actions is-center">
          <button type="button" className="cw-btn cw-btn-mint cw-btn-lg" onClick={onBookCall}>
            <PhoneCall aria-hidden /> Book a free call
          </button>
          <button type="button" className="cw-btn cw-btn-glass cw-btn-lg" onClick={onEnquiry}>
            <MessageSquare aria-hidden /> Send an enquiry
          </button>
          <a className="cw-btn cw-btn-glass cw-btn-lg" href="#features">
            Explore the CRM <ArrowRight aria-hidden />
          </a>
        </div>
      </div>
    </section>
  );
}
