// One set of URLs for the phone layout and the desktop CRM (D-104): the same link opens
// the same thing on both. This file is the phone layout's side of that map.

const DESKTOP_ONLY = new Set(['reports', 'dashboards', 'workflows', 'campaigns', 'settings', 'setup', 'app-users', 'businesses', 'support']);
const OBJECT_RE = /^[a-z][a-z0-9_]{1,40}$/;

/** What a URL means on a phone. `null` = the phone layout has no screen for it (the desktop page is shown instead). */
export function parseRoute(pathname, search = '') {
  const q = new URLSearchParams(search);
  const parts = pathname.split('/').filter(Boolean);
  if (parts[0] === 'share' && parts[1]) return { name: 'share', id: parts[1] };
  if (parts.length === 0) return { name: 'root' };
  if (parts[0] !== 'crm') return null;
  if (parts.length === 1 || parts[1] === 'home' || parts[1] === 'login') return { name: 'root' };
  if (parts[1] === 'businesses' && parts.length === 2) return { name: 'businesses', create: q.has('new') };
  if (parts[1] === 'browse') return parts[2] ? { name: 'listing', id: parts[2] } : { name: 'browse' };
  if (parts[1] === 'me') {
    if (parts[2] === 'support') {
      if (parts[3] === 'new') return { name: 'support', view: 'request' };
      if (parts[3] === 'tickets') return parts[4] ? { name: 'support', view: 'detail', id: parts[4] } : { name: 'support', view: 'tickets' };
      return { name: 'support', view: 'hub' };
    }
    return parts.length === 2 ? { name: 'profile' } : null;
  }
  if (parts[1] !== 'w' || !parts[2]) return null;
  const code = decodeURIComponent(parts[2]);
  const section = parts[3];
  if (!section || section === 'home') return { name: 'home', code };
  if (section === 'menu') return { name: 'menu', code };
  if (section === 'listing') return { name: 'mybusiness', code };
  if (section === 'cards' && parts.length === 4) {
    if (q.has('scan')) return { name: 'scan', code };
    if (q.get('card')) return { name: 'card', code, id: q.get('card') };
    return { name: 'cards', code };
  }
  if (DESKTOP_ONLY.has(section) || !OBJECT_RE.test(section)) return null;
  if (parts.length === 4) {
    if (q.has('new')) return { name: 'form', code, object: section, prefill: q.get('prefill') || '' };
    return { name: 'list', code, object: section, q: q.get('q') || '' };
  }
  if (parts.length === 5) {
    const id = decodeURIComponent(parts[4]);
    return q.has('edit') ? { name: 'form', code, object: section, id } : { name: 'detail', code, object: section, id };
  }
  return null;
}

/** Whether the phone layout shows this URL itself. */
export function phoneHandles(pathname, search) {
  return parseRoute(pathname, search) !== null;
}

const w = (code) => `/crm/w/${encodeURIComponent(code)}`;

export const paths = {
  home: (code) => `${w(code)}/home`,
  menu: (code) => `${w(code)}/menu`,
  cards: (code) => `${w(code)}/cards`,
  card: (code, id) => `${w(code)}/cards?card=${encodeURIComponent(id)}`,
  scan: (code) => `${w(code)}/cards?scan=1`,
  myBusiness: (code) => `${w(code)}/listing`,
  list: (code, object, q) => `${w(code)}/${object}${q ? `?q=${encodeURIComponent(q)}` : ''}`,
  detail: (code, object, id) => `${w(code)}/${object}/${encodeURIComponent(id)}`,
  create: (code, object, prefill) => `${w(code)}/${object}?new=1${prefill ? `&prefill=${prefill}` : ''}`,
  edit: (code, object, id) => `${w(code)}/${object}/${encodeURIComponent(id)}?edit=1`,
  team: (code) => `${w(code)}/settings/access`,
  businessProfile: (code) => `${w(code)}/settings/business`,
  businesses: '/crm/businesses',
  browse: '/crm/browse',
  listing: (id) => `/crm/browse/${encodeURIComponent(id)}`,
  profile: '/crm/me',
  support: (view, id) => (view === 'request' ? '/crm/me/support/new' : view === 'tickets' ? '/crm/me/support/tickets' : view === 'detail' ? `/crm/me/support/tickets/${encodeURIComponent(id)}` : '/crm/me/support')
};
