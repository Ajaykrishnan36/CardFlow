import { Check, FileText, Info } from 'lucide-react';
import { SectionHead } from '../common/SectionHead';
import { ScreenFrame } from '../common/ScreenFrame';
import { features, quoteSteps, screens } from '../../data/content';
import quotationDoc from '../../assets/images/doc-quotation.webp';
import invoiceDoc from '../../assets/images/doc-invoice.webp';

/** Every major feature with a real CRM screen, and the quotation flow given its own stage. */
export function Features() {
  return (
    <section className="cw-sec" id="features" aria-labelledby="cw-features-title">
      <div className="cw-wrap">
        <SectionHead
          eyebrow="Complete CRM features"
          title={
            <span id="cw-features-title">
              Everything your sales team needs, <span className="cw-grad">in one place</span>
            </span>
          }
          text="Real screens from Cogniz CRM, shown with demonstration data."
        />
        {features.map((f, i) => (
          <div key={f.id}>
            <article id={`feature-${f.id}`} className={`cw-feature${i % 2 ? ' is-flip' : ''}`}>
              <div className="cw-feature-copy">
                <p className="cw-eyebrow" data-reveal>
                  <span className="cw-eyebrow-dot" aria-hidden />
                  {f.eyebrow}
                </p>
                <h3 data-reveal>{f.title}</h3>
                <p className="cw-lead" data-reveal>
                  {f.text}
                </p>
                <ul className="cw-checks">
                  {f.points.map((p) => (
                    <li key={p} data-reveal>
                      <Check aria-hidden /> {p}
                    </li>
                  ))}
                </ul>
                {f.note ? (
                  <p className="cw-note" data-reveal>
                    <Info aria-hidden /> {f.note}
                  </p>
                ) : null}
              </div>
              <div className="cw-feature-shot" data-reveal>
                <ScreenFrame src={screens[f.screen]} alt={f.alt} />
              </div>
            </article>
            {f.id === 'pipeline' ? <QuotationStage /> : null}
          </div>
        ))}
      </div>
    </section>
  );
}

/** Quotations get special weight: the steps, the CRM screen and the real printed quotation. */
function QuotationStage() {
  return (
    <article id="feature-quotations" className="cw-quote" aria-labelledby="cw-quote-title">
      <div className="cw-quote-copy">
        <p className="cw-eyebrow is-light" data-reveal>
          <span className="cw-eyebrow-dot" aria-hidden />
          Quotations & estimates
        </p>
        <h3 id="cw-quote-title" data-reveal>
          A professional quotation in a few clicks, not an hour of typing
        </h3>
        <ol className="cw-quote-steps">
          {quoteSteps.map((s, i) => (
            <li key={s} data-reveal>
              <span>{i + 1}</span>
              {s}
            </li>
          ))}
        </ol>
      </div>
      <div className="cw-quote-visual" data-reveal>
        <div className="cw-quote-screen">
          <ScreenFrame src={screens.quote} alt="Quotation in the CRM with items, list price, discount, tax and total (sample data)" />
        </div>
        <figure className="cw-paper is-quote">
          <img src={quotationDoc} alt="Printed quotation QT-000013 with customer details, items, discount, tax and total (sample data)" width={960} height={692} loading="lazy" decoding="async" />
          <figcaption>
            <FileText aria-hidden /> Quotation PDF
          </figcaption>
        </figure>
        <figure className="cw-paper is-invoice">
          <img src={invoiceDoc} alt="Tax invoice INV-000013 made from the accepted quotation, fully paid (sample data)" width={960} height={781} loading="lazy" decoding="async" />
          <figcaption>
            <FileText aria-hidden /> Tax invoice
          </figcaption>
        </figure>
      </div>
    </article>
  );
}
