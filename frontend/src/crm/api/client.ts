// Fetch wrapper for the CRM API (PRD §11 FE-01/FE-02).
// In a browser: same-origin cookies, CSRF double-submit header on every mutation.
// In the native app shell (Capacitor) there is no shared origin, so the session is a
// bearer token kept on the device. Either way: PRD error envelope → ApiError.

/** Running inside the native app shell (not a browser tab). */
export const IS_NATIVE = Boolean((window as unknown as { Capacitor?: { isNativePlatform?: () => boolean } }).Capacitor?.isNativePlatform?.());
/** Where the API lives: this origin in a browser, the server's address in the native shell. */
export const API_ORIGIN = IS_NATIVE ? 'https://cardflow-api-fsij.onrender.com' : '';
export const API_BASE = `${API_ORIGIN}/api/crm/v1`;
const CSRF_COOKIE = 'crm_csrf';
const TOKEN_KEY = 'cf_session';

/** The native shell's session token ('' in a browser, where the cookie is the session). */
export function sessionToken(): string {
  if (!IS_NATIVE) return '';
  try {
    return window.localStorage.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

function storeSessionToken(token: string | null) {
  if (!IS_NATIVE) return;
  try {
    if (token) window.localStorage.setItem(TOKEN_KEY, token);
    else window.localStorage.removeItem(TOKEN_KEY);
  } catch {
    // storage unavailable: the session lasts until the app closes
  }
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fieldErrors: Record<string, string>;
  readonly requestId?: string;
  readonly retryAfter?: number;
  /** Extra data some errors carry (e.g. the duplicates found when saving a card). */
  readonly details?: Record<string, unknown>;

  constructor(status: number, code: string, message: string, fieldErrors: Record<string, string> = {}, requestId?: string, retryAfter?: number, details?: Record<string, unknown>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fieldErrors = fieldErrors;
    this.requestId = requestId;
    this.retryAfter = retryAfter;
    this.details = details;
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

function readCookie(name: string): string | null {
  const match = document.cookie.split('; ').find((c) => c.startsWith(name + '='));
  return match ? decodeURIComponent(match.slice(name.length + 1)) : null;
}

/** The CSRF token for a request that changes data (browser only). */
export async function ensureCsrf(force = false): Promise<string> {
  let token = force ? null : readCookie(CSRF_COOKIE);
  if (!token) {
    await fetch(`${API_BASE}/auth/csrf`, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
    token = readCookie(CSRF_COOKIE);
  }
  return token ?? '';
}

type UnauthorizedHandler = () => void;
let onUnauthorized: UnauthorizedHandler | null = null;

/** Called when an authenticated call returns 401 (expired/revoked session). */
export function setUnauthorizedHandler(fn: UnauthorizedHandler | null) {
  onUnauthorized = fn;
}

const FALLBACK_MESSAGES: Record<number, string> = {
  0: "Can't reach the server. Check your connection and try again.",
  403: "You don't have access to this.",
  404: 'Not found.',
  429: 'Too many attempts. Please wait a moment and try again.',
  500: 'Something went wrong. Please try again.',
  503: 'The CRM is temporarily unavailable. Please try again shortly.'
};

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

export async function api<T>(path: string, opts: RequestOptions = {}, retried = false): Promise<T> {
  const method = opts.method ?? 'GET';
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
  if (IS_NATIVE) {
    headers['X-Session-Transport'] = 'bearer';
    const token = sessionToken();
    if (token) headers.Authorization = `Bearer ${token}`;
  } else if (method !== 'GET') {
    headers['X-CSRF-Token'] = await ensureCsrf();
  }
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await fetch(API_BASE + path, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      credentials: IS_NATIVE ? 'omit' : 'same-origin',
      signal: opts.signal
    });
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'network_error', FALLBACK_MESSAGES[0]);
  }

  if (IS_NATIVE && res.ok && path.startsWith('/auth/logout')) storeSessionToken(null);
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => null);

  if (!res.ok) {
    const err = new ApiError(
      res.status,
      data?.code ?? `http_${res.status}`,
      data?.message ?? FALLBACK_MESSAGES[res.status] ?? FALLBACK_MESSAGES[500],
      data?.fieldErrors ?? {},
      data?.requestId,
      Number(res.headers.get('Retry-After')) || undefined,
      data?.details
    );
    if (err.code === 'csrf_failed' && !retried) {
      await ensureCsrf(true);
      return api<T>(path, opts, true);
    }
    if (res.status === 401 && !path.startsWith('/auth/')) storeSessionToken(null);
    if (res.status === 401 && path !== '/me' && !path.startsWith('/auth/')) {
      onUnauthorized?.();
    }
    throw err;
  }
  // Native shell: signing in (or renewing) hands back the session token; signing out drops it.
  if (IS_NATIVE && path.startsWith('/auth/')) {
    if (path.startsWith('/auth/logout')) storeSessionToken(null);
    else if (data && typeof data.token === 'string' && data.token) storeSessionToken(data.token);
  }
  return data as T;
}
