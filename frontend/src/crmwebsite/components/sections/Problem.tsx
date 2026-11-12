import { useEffect, useState } from 'react';
import { Check, X } from 'lucide-react';
import { SectionHead } from '../common/SectionHead';
import { after, before } from '../../data/content';
import { useInView } from '../../hooks/useInView';

/** The paper desk, as pieces that fly into an organised list when the visitor switches. */
const pieces = [
  { kind: 'paper', text: 'Ravi — bike enquiry\ncall back??', to: 'Ravi Kumar · New lead · follow-up 4 PM' },
  { kind: 'sticky', text: 'Quote for Meena\n90,000?', to: 'Meena Raj · Quotation QT-000013 sent' },
  { kind: 'sheet', text: 'leads_final_v3.xlsx', to: '128 leads · one searchable list' },
  { kind: 'chat', text: '7 unread enquiries', to: 'Every enquiry assigned to a person' },
  { kind: 'bill', text: 'Bill #214 UNPAID', to: 'Invoice INV-000013 · paid by UPI' },
  { kind: 'sticky b', text: 'Stock? ask Suresh', to: 'Rain cover · 24 on hand' }
];

export function Problem() {
  const [ref, inView] = useInView<HTMLDivElement>(0.35);
  const [crm, setCrm] = useState(false);
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!inView || touched) return;
    const t = setTimeout(() => setCrm(true), 1600);
    return () => clearTimeout(t);
  }, [inView, touched]);

  return (
    <section className="cw-sec" id="problem" aria-labelledby="cw-problem-title">
      <div className="cw-wrap">
        <SectionHead
          eyebrow="The everyday problem"
          title={
            <span id="cw-problem-title">
              Still running your business with <span className="cw-strike">paper, Excel and WhatsApp?</span>
            </span>
          }
          text="Most businesses do not lose customers to a competitor. They lose them to a missed call, a lost page and a follow-up nobody remembered."
        />

        <div className="cw-switch" role="group" aria-label="Compare" data-reveal>
          <button type="button" aria-pressed={!crm} className={!crm ? 'is-on' : ''} onClick={() => (setTouched(true), setCrm(false))}>
            On paper
          </button>
          <button type="button" aria-pressed={crm} className={crm ? 'is-on' : ''} onClick={() => (setTouched(true), setCrm(true))}>
            With Cogniz CRM
          </button>
          <span className="cw-switch-knob" style={{ transform: crm ? 'translateX(100%)' : 'none' }} aria-hidden />
        </div>

        <div ref={ref} className={`cw-desk${crm ? ' is-crm' : ''}`} data-reveal>
          <div className="cw-desk-board" aria-hidden>
            <div className="cw-desk-head">
              <span className="cw-dots">
                <i />
                <i />
                <i />
              </span>
              <b>{crm ? 'Leads · Cogniz CRM' : 'Your desk, today'}</b>
            </div>
          </div>
          {pieces.map((p, i) => (
            <div key={i} className={`cw-piece p${i + 1} k-${p.kind.split(' ')[0]}${p.kind.includes(' b') ? ' is-blue' : ''}`}>
              <span className="cw-piece-old">{p.text}</span>
              <span className="cw-piece-new">
                <Check aria-hidden />
                {p.to}
              </span>
            </div>
          ))}
          <p className="cw-sr">{crm ? 'With the CRM: every item sits in one organised list.' : 'On paper: notes, sheets, chats and bills scattered across the desk.'}</p>
        </div>

        <div className="cw-ba">
          <article className="cw-ba-card is-before" data-reveal>
            <h3>
              <span className="cw-tag is-red">Before CRM</span>
            </h3>
            <ul>
              {before.map((t) => (
                <li key={t}>
                  <X aria-hidden /> {t}
                </li>
              ))}
            </ul>
          </article>
          <article className="cw-ba-card is-after" data-reveal>
            <h3>
              <span className="cw-tag is-teal">After CRM</span>
            </h3>
            <ul>
              {after.map((t) => (
                <li key={t}>
                  <Check aria-hidden /> {t}
                </li>
              ))}
            </ul>
          </article>
        </div>
      </div>
    </section>
  );
}
