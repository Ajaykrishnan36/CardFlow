import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/crm.css';
import './i18n';
import { CrmApp } from './app';

// Shares CardFlow's index.html, so adjust only what the CRM needs, only on /crm pages.
document.documentElement.classList.add('crm-page');
document.documentElement.lang = 'en';
// CardFlow disables pinch-zoom for its native shell; the CRM keeps it (WCAG 2.2 AA).
document.querySelector('meta[name="viewport"]')?.setAttribute('content', 'width=device-width, initial-scale=1, viewport-fit=cover');

const host = document.getElementById('root');
if (host) {
  const container = document.createElement('div');
  container.id = 'crm-root';
  container.className = 'crm-root';
  host.appendChild(container);
  createRoot(container).render(
    <StrictMode>
      <CrmApp />
    </StrictMode>
  );
}
