// CRM API for the phone layout. It uses the same client as the desktop CRM, so both
// share one session (the browser's sign-in cookie, or the native shell's token) and one
// error shape. Errors are thrown — screens show the server's message, never a fake success.
import { api, ApiError } from '@crm/api/client';

export const CrmError = ApiError;

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

export function crm(path, { method = 'GET', body, idempotencyKey } = {}) {
  return api(path, { method, body, headers: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : undefined });
}

const w = (code) => `/w/${encodeURIComponent(code)}`;

export const crmApi = {
  me: () => crm('/me'),
  logoutAll: () => crm('/auth/logout-all', { method: 'POST' }),
  renew: () => crm('/auth/session/renew', { method: 'POST' }),
  sessions: () => crm('/me/sessions').then((r) => r.data || []),
  revokeSession: (id) => crm(`/me/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  updateMe: (body) => crm('/me', { method: 'PATCH', body }),
  requestEmailCode: (email) => crm('/me/email/request', { method: 'POST', body: { email } }),
  verifyEmail: (email, code) => crm('/me/email/verify', { method: 'POST', body: { email, code } }),
  setPassword: (newPassword, currentPassword) => crm('/me/password', { method: 'POST', body: { newPassword, currentPassword } }),
  requestPhoneChange: (phone) => crm('/me/phone/request', { method: 'POST', body: { phone } }),
  verifyPhoneChange: (phone, code) => crm('/me/phone/verify', { method: 'POST', body: { phone, code } }),

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
  uiLayout: (code) => crm(`${w(code)}/ui-layout`),
  documentLines: (code, object, id) => crm(`${w(code)}/${object}/${encodeURIComponent(id)}/lines`),
  convertQuote: (code, id) => crm(`${w(code)}/quotes/${encodeURIComponent(id)}/convert`, { method: 'POST' }),
  invoiceOrder: (code, id) => crm(`${w(code)}/sales_orders/${encodeURIComponent(id)}/invoice`, { method: 'POST' }),
  invoiceWorkOrder: (code, id) => crm(`${w(code)}/work_orders/${encodeURIComponent(id)}/invoice`, { method: 'POST' }),
  caseWorkOrder: (code, id) => crm(`${w(code)}/cases/${encodeURIComponent(id)}/work-order`, { method: 'POST' }),
  contactRoles: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/contact-roles`),
  recordTeam: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/team`),
  invoiceLedger: (code, id) => crm(`${w(code)}/invoices/${encodeURIComponent(id)}/payments`),
  caseSla: (code, id) => crm(`${w(code)}/cases/${encodeURIComponent(id)}/sla`),
  renewContract: (code, id, body) => crm(`${w(code)}/contracts/${encodeURIComponent(id)}/renew`, { method: 'POST', body }),
  lookup: (code, target, q = '') => crm(`${w(code)}/lookup/${target}${qs({ q })}`).then((r) => r.data || []),
  timeline: (code, object, id) => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/timeline${qs({ limit: 30 })}`).then((r) => r.data || []),
  addNote: (code, object, id, body, kind = 'note') => crm(`${w(code)}/crm/${object}/${encodeURIComponent(id)}/notes`, { method: 'POST', body: { body, kind } }),
  convertLead: (code, id, body, idempotencyKey) => crm(`${w(code)}/crm/leads/${encodeURIComponent(id)}/convert`, { method: 'POST', body, idempotencyKey }),

  cards: (code, params) => crm(`${w(code)}/cards${qs(params)}`),
  card: (code, id) => crm(`${w(code)}/cards/${encodeURIComponent(id)}`),
  matchCard: (code, card) => crm(`${w(code)}/cards/match`, { method: 'POST', body: card }),
  saveCard: (code, body, idempotencyKey) => crm(`${w(code)}/cards`, { method: 'POST', body, idempotencyKey }),
  unlinkCard: (code, cardId, linkId) => crm(`${w(code)}/cards/${encodeURIComponent(cardId)}/links/${encodeURIComponent(linkId)}`, { method: 'DELETE' }),

  plan: (code) => crm(`${w(code)}/plan`),
  gettingStarted: (code) => crm(`${w(code)}/getting-started`)
};

/** The first thing to show from an error: a field message if there is one, else the message. */
export function errorText(e) {
  if (e instanceof ApiError) {
    const first = Object.values(e.fieldErrors || {})[0];
    return first || e.message;
  }
  return 'Something went wrong. Please try again.';
}
