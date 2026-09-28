// Fetch wrapper for the CRM API (PRD §11 FE-01/FE-02): same-origin cookies, CSRF
// double-submit header on every mutation, PRD error envelope → ApiError.

export const API_BASE = '/api/crm/v1';
const CSRF_COOKIE = 'crm_csrf';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fieldErrors: Record<string, string>;
  readonly requestId?: string;
  readonly retryAfter?: number;

  constructor(status: number, code: string, message: string, fieldErrors: Record<string, string> = {}, requestId?: string, retryAfter?: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fieldErrors = fieldErrors;
    this.requestId = requestId;
    this.retryAfter = retryAfter;
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError;
}

function readCookie(name: string): string | null {
  const match = document.cookie.split('; ').find((c) => c.startsWith(name + '='));
  return match ? decodeURIComponent(match.slice(name.length + 1)) : null;
}

async function ensureCsrf(force = false): Promise<string> {
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
  if (method !== 'GET') headers['X-CSRF-Token'] = await ensureCsrf();
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';

  let res: Response;
  try {
    res = await fetch(API_BASE + path, {
      method,
      headers,
      body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
      credentials: 'same-origin',
      signal: opts.signal
    });
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'network_error', FALLBACK_MESSAGES[0]);
  }

  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => null);

  if (!res.ok) {
    const err = new ApiError(
      res.status,
      data?.code ?? `http_${res.status}`,
      data?.message ?? FALLBACK_MESSAGES[res.status] ?? FALLBACK_MESSAGES[500],
      data?.fieldErrors ?? {},
      data?.requestId,
      Number(res.headers.get('Retry-After')) || undefined
    );
    if (err.code === 'csrf_failed' && !retried) {
      await ensureCsrf(true);
      return api<T>(path, opts, true);
    }
    if (res.status === 401 && path !== '/me' && !path.startsWith('/auth/')) {
      onUnauthorized?.();
    }
    throw err;
  }
  return data as T;
}
