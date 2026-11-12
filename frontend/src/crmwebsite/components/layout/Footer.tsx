import { Link } from 'react-router-dom';
import { Logo } from '../common/Logo';
import { features, industries, nav } from '../../data/content';

export function Footer({ onBookCall, onEnquiry }: { onBookCall: () => void; onEnquiry: () => void }) {
  return (
    <footer className="cw-foot">
      <div className="cw-wrap cw-foot-grid">
        <div className="cw-foot-brand">
          <Logo light />
          <p>Leads, follow-ups, quotations, sales and your team in one secure place. Built for businesses moving from paper and Excel to a real system.</p>
          <div className="cw-foot-actions">
            <button type="button" className="cw-btn cw-btn-mint" onClick={onBookCall}>
              Book a free call
            </button>
            <button type="button" className="cw-btn cw-btn-glass" onClick={onEnquiry}>
              Send an enquiry
            </button>
          </div>
        </div>
        <nav aria-label="Website">
          <h3>Website</h3>
          {nav.map((n) => (
            <a key={n.href} href={n.href}>
              {n.label}
            </a>
          ))}
        </nav>
        <nav aria-label="Features">
          <h3>Features</h3>
          {features.slice(0, 7).map((f) => (
            <a key={f.id} href={`#feature-${f.id}`}>
              {f.eyebrow}
            </a>
          ))}
        </nav>
        <nav aria-label="Industries">
          <h3>Industries</h3>
          {industries.slice(0, 7).map((i) => (
            <a key={i.key} href="#industries">
              {i.label}
            </a>
          ))}
        </nav>
        <nav aria-label="Account">
          <h3>Account</h3>
          <Link to="/signup">Login</Link>
          <Link to="/signup">Create your CRM</Link>
          <a href="#contact">Contact us</a>
        </nav>
      </div>
      <div className="cw-wrap cw-foot-base">
        <small>© {new Date().getFullYear()} Cogniz CRM. All rights reserved.</small>
        <small>Screens on this site show demonstration data.</small>
      </div>
    </footer>
  );
}
