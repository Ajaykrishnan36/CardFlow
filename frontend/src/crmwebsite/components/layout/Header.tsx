import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Menu, PhoneCall, X } from 'lucide-react';
import { Logo } from '../common/Logo';
import { nav } from '../../data/content';

export function Header({ onBookCall }: { onBookCall: () => void }) {
  const [stuck, setStuck] = useState(false);
  const [open, setOpen] = useState(false);
  // The page scrolls inside the app's own container (.crm-root), or the window elsewhere.
  useEffect(() => {
    const box = document.querySelector<HTMLElement>('.crm-root');
    const target: HTMLElement | Window = box ?? window;
    const on = () => setStuck((box ? box.scrollTop : window.scrollY) > 10);
    on();
    target.addEventListener('scroll', on, { passive: true });
    return () => target.removeEventListener('scroll', on);
  }, []);
  useEffect(() => {
    if (!open) return;
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    window.addEventListener('keydown', esc);
    return () => window.removeEventListener('keydown', esc);
  }, [open]);
  return (
    <header className={`cw-nav${stuck ? ' is-stuck' : ''}${open ? ' is-open' : ''}`}>
      <div className="cw-wrap cw-nav-in">
        <a href="#top" aria-label="Cogniz CRM home" onClick={() => setOpen(false)}>
          <Logo />
        </a>
        <nav className="cw-nav-links" id="cw-menu" aria-label="Main">
          {nav.map((n) => (
            <a key={n.href} href={n.href} onClick={() => setOpen(false)}>
              {n.label}
            </a>
          ))}
          <Link className="cw-only-small" to="/signup">
            Login
          </Link>
          <button
            type="button"
            className="cw-btn cw-btn-primary cw-only-small"
            onClick={() => {
              setOpen(false);
              onBookCall();
            }}
          >
            <PhoneCall aria-hidden /> Book a free call
          </button>
        </nav>
        <div className="cw-nav-cta">
          <Link className="cw-link" to="/signup">
            Login
          </Link>
          <button type="button" className="cw-btn cw-btn-primary cw-hide-small" onClick={onBookCall}>
            <PhoneCall aria-hidden /> Book free call
          </button>
          <button type="button" className="cw-nav-toggle" aria-label={open ? 'Close menu' : 'Open menu'} aria-expanded={open} aria-controls="cw-menu" onClick={() => setOpen((o) => !o)}>
            {open ? <X aria-hidden /> : <Menu aria-hidden />}
          </button>
        </div>
      </div>
    </header>
  );
}
