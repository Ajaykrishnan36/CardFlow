// CRM API client for the mobile app (unified session, D-93).
// The same sign-in token works for the app API (/api/v1) and the CRM API (/api/crm/v1).
// Errors are thrown as CrmError — screens show the server's message, never a fake success.
import { API_BASE_URL } from './api';

export const CRM_BASE_URL = API_BASE_URL.replace(/\/api\/v1\/?$/, '/api/crm/v1');

export class CrmError extends Error {
  constructor(status, code, message, fieldErrors = {}, details = undefined) {
    super(message);
    this.name = 'CrmError';
    this.status = status;
    this.code = code;
    this.fieldErrors = fieldErrors || {};
    this.details = details;
  }
}

let tokenGetter = () => null;
let onUnauthorized = null;

/** The auth context registers how to read the current session token and what to do when it is refused. */
export function configureCrm({ getToken, onSessionExpired }) {
  tokenGetter = getToken || (() => null);
  onUnauthorized = onSessionExpired || null;
}

/** A session token issued by the unified sign-in (older installs hold a legacy JWT that the CRM refuses). */
export function isUnifiedToken(token) {
  return typeof token === 'string' && token.startsWith('crms_');
}

export function newIdempotencyKey() {
  return `m-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

function qs(params = {}) {
  const parts = [];
  Object.entries(params).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`);
  });
  return parts.length ? `?${parts.join('&')}` : '';
}

export async function crm(path, { method = 'GET', body, token, idempotencyKey, raw = false } = {}) {
  const auth = token || tokenGetter();
  const headers = { Accept: 'application/json', 'X-Session-Transport': 'bearer' };
  if (auth) headers.Authorization = `Bearer ${auth}`;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;

  let res;
  try {
    res = await fetch(`${CRM_BASE_URL}${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  } catch (e) {
    throw new CrmError(0, 'network_error', "Can't reach the server. Check your connection and try again.");
  }
  if (raw) return res;
  if (res.status === 204) return undefined;
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const err = new CrmError(
      res.status,
      data?.code || `http_${res.status}`,
      data?.message || (res.status === 403 ? "You don't have access to this." : res.status === 404 ? 'Not found.' : 'Something went wrong. Please try again.'),
      data?.fieldErrors,
      data?.details
    );
    if (res.status === 401 && onUnauthorized) onUnauthorized(err);
    throw err;
  }
  return data;
}

const w = (code) => `/w/${encodeURIComponent(code)}`;

export const crmApi = {
  me: (token) => crm('/me', { token }),
  logout: (token) => crm('/auth/logout', { method: 'POST', token }),
  logoutAll: () => crm('/auth/logout-all', { method: 'POST' }),
  renew: () => crm('/auth/session/renew', { method: 'POST' }),
  sessions: () => crm('/me/sessions').then((r) => r.data || []),
  revokeSession: (id) => crm(`/me/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  businesses: () => crm('/businesses'),
  createBusiness: (body, idempotencyKey) => crm('/businesses', { method: 'POST', body, idempotencyKey }),
  business: (code) => crm(`${w(code)}/business`),
  updateBusiness: (code, body) => crm(`${w(code)}/business`, { method: 'PATCH', body }),

  context: (code) => crm(`${w(code)}/context`),
  summary: (code, params) => crm(`${w(code)}/dashboard/summary${qs(params)}`),

  meta: (code, object) => crm(`${w(code)}/crm/meta/${object}`),
  list: (code, object, params) => crm(`${w(code)}/crm/${object}${qs(params)}`),
  get: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}`),
  create: (code, object, values, idempotencyKey) => crm(`${w(code)}/crm/${object}`, { method: 'POST', body: { values }, idempotencyKey }),
  update: (code, object, id, values, expectedVersion) =>
    crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}`, { method: 'PATCH', body: { values, expectedVersion } }),
  remove: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  lookup: (code, target, q = '') => crm(`${w(code)}/lookup/${target}${qs({ q })}`).then((r) => r.data || []),
  timeline: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/timeline${qs({ limit: 30 })}`).then((r) => r.data || []),
  addNote: (code, object, id, body, kind = 'note') => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/notes`, { method: 'POST', body: { body, kind } }),
  convertLead: (code, id, body, idempotencyKey) => crm(`${w(code)}/crm/leads/${encodeURIComponent(id)}/convert`, { method: 'POST', body, idempotencyKey }),

  cards: (code, params) => crm(`${w(code)}/cards${qs(params)}`),
  card: (code, id) => crm(`${w(code)}/cards/${encodeURIComponent(id)}`),
  matchCard: (code, card) => crm(`${w(code)}/cards/match`, { method: 'POST', body: card }),
  saveCard: (code, body, idempotencyKey) => crm(`${w(code)}/cards`, { method: 'POST', body, idempotencyKey }),
  unlinkCard: (code, cardId, linkId) => crm(`${w(code)}/cards/${encodeURIComponent(cardId)}/links/${encodeURIComponent(linkId)}`, { method: 'DELETE' }),

  plan: (code) => crm(`${w(code)}/plan`)
};

/** The first thing to show from an error: a field message if there is one, else the message. */
export function errorText(e) {
  if (e instanceof CrmError) {
    const first = Object.values(e.fieldErrors || {})[0];
    return first || e.message;
  }
  return 'Something went wrong. Please try again.';
}
