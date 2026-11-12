import { useCallback, useEffect, useRef, useState } from 'react';
import { IS_NATIVE } from '@crm/api/client';
import { RootLoginPage, sharedHost } from '@crm/features/auth/tenant-login-page';
import './styles/website.css';
import { Header } from './components/layout/Header';
import { Footer } from './components/layout/Footer';
import { Hero } from './components/sections/Hero';
import { Problem } from './components/sections/Problem';
import { Journey } from './components/sections/Journey';
import { Features } from './components/sections/Features';
import { Benefits, Contact, Faq, FinalCta, Industries, Pricing, Showcase, TeamFlow } from './components/sections/More';
import { useReveal } from './hooks/useReveal';

/**
 * "/" (D-181). On the CRM's own address it is the public website; on a business's own domain
 * it is that business's sign-in, and in the phone app it is the sign-in, as before.
 */
export function HomePage() {
  const own = !sharedHost(window.location.hostname.toLowerCase());
  if (IS_NATIVE || own) return <RootLoginPage />;
  return <Website />;
}

const META: Array<[string, string, string]> = [
  ['name', 'description', 'Cogniz CRM brings your leads, follow-ups, quotations, team, stock and sales into one organised system. Move from notebooks, Excel and WhatsApp to a CRM built for growing businesses.'],
  ['property', 'og:title', 'Cogniz CRM · Stop managing leads on paper'],
  ['property', 'og:description', 'Capture every enquiry, assign it to your team, follow up on time, send quotations and see your business on one dashboard.'],
  ['property', 'og:type', 'website'],
  ['name', 'twitter:card', 'summary_large_image']
];

function Website() {
  const root = useRef<HTMLDivElement>(null);
  useReveal(root);
  const [tab, setTab] = useState<'call' | 'enquiry'>('call');

  // Page title, description and sharing tags while the website is open; put back on leaving.
  useEffect(() => {
    const prevTitle = document.title;
    document.title = 'Cogniz CRM · Stop managing leads on paper';
    const added: HTMLMetaElement[] = [];
    const prev: Array<[HTMLMetaElement, string]> = [];
    for (const [attr, key, content] of META) {
      let m = document.head.querySelector<HTMLMetaElement>(`meta[${attr}="${key}"]`);
      if (m) prev.push([m, m.content]);
      else {
        m = document.createElement('meta');
        m.setAttribute(attr, key);
        document.head.appendChild(m);
        added.push(m);
      }
      m.content = content;
    }
    return () => {
      document.title = prevTitle;
      added.forEach((m) => m.remove());
      prev.forEach(([m, c]) => (m.content = c));
    };
  }, []);

  const go = useCallback((t: 'call' | 'enquiry') => {
    setTab(t);
    requestAnimationFrame(() => {
      document.getElementById('contact')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      setTimeout(() => document.getElementById(t === 'call' ? 'cw-call-name' : 'cw-enq-name')?.focus({ preventScroll: true }), 650);
    });
  }, []);
  const bookCall = useCallback(() => go('call'), [go]);
  const enquiry = useCallback(() => go('enquiry'), [go]);

  return (
    <div className="cw" ref={root}>
      <a className="cw-skip" href="#main">
        Skip to content
      </a>
      <Header onBookCall={bookCall} />
      <main id="main">
        <Hero onBookCall={bookCall} />
        <Problem />
        <Journey />
        <Features />
        <TeamFlow />
        <Industries />
        <Benefits />
        <Showcase />
        <Pricing />
        <Contact tab={tab} setTab={setTab} />
        <Faq onEnquiry={enquiry} />
        <FinalCta onBookCall={bookCall} onEnquiry={enquiry} />
      </main>
      <Footer onBookCall={bookCall} onEnquiry={enquiry} />
    </div>
  );
}
