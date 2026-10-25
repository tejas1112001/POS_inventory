// Platform API client (/api/platform on admin.<BASE_DOMAIN>). The access
// token lives in memory only; the refresh token is the HttpOnly __Host-prt
// cookie, used by POST /auth/refresh with the CSRF header (TENANCY §7.4).

const BASE = '/api/platform';

export class ApiError extends Error {
  constructor(message, { status = 0, code = '', fields = null, details = null, retryAfter = null } = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = fields;
    this.details = details;
    this.retryAfter = retryAfter;
  }
}

let session = { token: null, expiresAt: 0, user: null };
const listeners = new Set();

export function getSession() {
  return session;
}

export function onSession(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function publish(next) {
  session = next;
  listeners.forEach((fn) => fn(session));
}

// setSession takes a SessionResult ({accessToken, expiresAt, user}).
export function setSession(res) {
  publish({ token: res.accessToken, expiresAt: Date.parse(res.expiresAt) || 0, user: res.user || session.user });
}

export function clearSession() {
  publish({ token: null, expiresAt: 0, user: null });
}

const AUTH_OPEN = ['/auth/login', '/auth/mfa/', '/auth/password/change', '/auth/refresh', '/auth/logout'];
const isOpenAuth = (path) => AUTH_OPEN.some((p) => path.startsWith(p));

let refreshing = null;

// A rotated refresh token must never be sent twice (reuse ends the
// session), so refreshes are serialised across tabs with a Web Lock.
function locked(fn) {
  return navigator.locks?.request ? navigator.locks.request('platform-auth-refresh', fn) : fn();
}

export function refresh() {
  if (!refreshing) {
    refreshing = locked(async () => {
      try {
        const res = await fetch(`${BASE}/auth/refresh`, { method: 'POST', credentials: 'same-origin', headers: { 'X-Requested-With': 'fetch' } });
        if (!res.ok) return null;
        const body = await res.json();
        setSession(body);
        return body;
      } catch {
        return null;
      }
    }).finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

async function toError(res) {
  let body = {};
  if ((res.headers.get('content-type') || '').includes('application/json')) body = await res.json().catch(() => ({}));
  const retry = Number(res.headers.get('Retry-After'));
  return new ApiError(body.error || `Request failed (${res.status})`, {
    status: res.status,
    code: body.code || '',
    fields: body.fields || null,
    details: body.details || null,
    retryAfter: retry > 0 ? retry : null,
  });
}

export async function request(method, path, body, retried = false) {
  if (session.token && session.expiresAt - Date.now() < 30_000 && !isOpenAuth(path)) await refresh();
  const headers = { 'X-Requested-With': 'fetch' };
  if (session.token) headers.Authorization = `Bearer ${session.token}`;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let res;
  try {
    res = await fetch(`${BASE}${path}`, { method, headers, credentials: 'same-origin', body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new ApiError('Cannot reach the server. Check the connection and try again.', { code: 'NETWORK' });
  }
  if (res.status === 401 && session.token && !retried && !isOpenAuth(path)) {
    if (await refresh()) return request(method, path, body, true);
    clearSession();
  }
  if (!res.ok) throw await toError(res);
  if (res.status === 204) return null;
  return (res.headers.get('content-type') || '').includes('application/json') ? res.json() : null;
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body ?? {}),
  patch: (path, body) => request('PATCH', path, body ?? {}),
  del: (path, body) => request('DELETE', path, body),
};

// query builds ?a=1&b=2 from the set values.
export function query(params) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== '') q.set(k, String(v));
  const s = q.toString();
  return s ? `?${s}` : '';
}
