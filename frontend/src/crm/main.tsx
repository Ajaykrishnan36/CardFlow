import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/crm.css';
import './i18n';
import { CrmApp } from './app';

import { IS_NATIVE } from './api/client';

// One app for every URL (D-104).
document.documentElement.classList.add('crm-page');
document.documentElement.lang = 'en';
// The native shell disables pinch-zoom; in a browser it stays (WCAG 2.2 AA).
if (!IS_NATIVE) {
  document.querySelector('meta[name="viewport"]')?.setAttribute('content', 'width=device-width, initial-scale=1, viewport-fit=cover');
}

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
