import { clearSession, getAccessToken, setSession, tokenSecondsLeft } from '../lib/session';

// Same origin by default: the app is served from https://<shop>.<domain> and
// calls /api/... there. VITE_API_URL is only for unusual setups.
const API_BASE_URL = (import.meta.env?.VITE_API_URL || '').replace(/\/+$/, '');

// ApiError carries the server's {error, code, fields} body (API_CONTRACT §1).
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

const AUTH_PATHS = ['/api/auth/login', '/api/auth/refresh', '/api/auth/logout'];
const isAuthPath = (endpoint) => AUTH_PATHS.some((p) => endpoint.startsWith(p));

let refreshPromise = null;

// The refresh token rotates on every use and the server revokes the whole
// session if an old one is presented again, so two tabs must never refresh
// at the same moment: a Web Lock serialises refreshes across tabs.
function withRefreshLock(fn) {
  if (typeof navigator !== 'undefined' && navigator.locks?.request) {
    return navigator.locks.request('pos-auth-refresh', fn);
  }
  return fn();
}

// refreshAccessToken asks for a new access token with the refresh cookie.
// It resolves to the session body, or null when there is no valid session.
export function refreshAccessToken() {
  if (refreshPromise) return refreshPromise;
  refreshPromise = withRefreshLock(async () => {
    try {
      const res = await fetch(`${API_BASE_URL}/api/auth/refresh`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'X-Requested-With': 'fetch' },
      });
      if (!res.ok) return null;
      const body = await res.json();
      setSession({ token: body.token, user: body.user, permissions: body.permissions });
      return body;
    } catch {
      return null;
    }
  }).finally(() => {
    refreshPromise = null;
  });
  return refreshPromise;
}

async function errorFrom(response) {
  let payload = {};
  const type = response.headers.get('content-type') || '';
  if (type.includes('application/json')) {
    payload = await response.json().catch(() => ({}));
  } else {
    const text = await response.text().catch(() => '');
    if (text) payload = { error: text.slice(0, 300) };
  }
  const message = payload.error || `Request failed (${response.status})`;
  const retry = Number(response.headers.get('Retry-After'));
  return new ApiError(message, {
    status: response.status,
    code: payload.code || '',
    fields: payload.fields || null,
    details: payload.details || null,
    retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null,
  });
}

function announce(error) {
  window.dispatchEvent(new CustomEvent('apiError', { detail: error.message }));
}

// request sends one API call. Options: body (JSON), formData, headers,
// quiet (no global error toast: the caller shows the error), raw (return
// the Response, for downloads).
async function request(method, endpoint, { body, formData, headers = {}, quiet = false, raw = false } = {}, retried = false) {
  if (getAccessToken() && tokenSecondsLeft() < 30 && !isAuthPath(endpoint)) {
    await refreshAccessToken();
  }
  const h = { 'X-Requested-With': 'fetch', ...headers };
  const token = getAccessToken();
  if (token && !h.Authorization) h.Authorization = `Bearer ${token}`;
  let payload;
  if (formData) {
    payload = formData;
  } else if (body !== undefined) {
    h['Content-Type'] = 'application/json';
    payload = JSON.stringify(body);
  }

  let response;
  try {
    response = await fetch(`${API_BASE_URL}${endpoint}`, { method, headers: h, body: payload, credentials: 'include' });
  } catch {
    const err = new ApiError('Cannot reach the server. Check your connection and try again.', { code: 'NETWORK' });
    if (!quiet) announce(err);
    throw err;
  }

  if (response.status === 401 && !retried && !isAuthPath(endpoint) && token) {
    // The access token expired or was revoked: one refresh, then retry.
    const fresh = await refreshAccessToken();
    if (fresh) {
      const rest = { ...h };
      delete rest.Authorization;
      delete rest['Content-Type'];
      return request(method, endpoint, { body, formData, headers: rest, quiet, raw }, true);
    }
    clearSession();
    window.dispatchEvent(new Event('unauthorized'));
  }

  if (!response.ok) {
    const err = await errorFrom(response);
    if (err.code === 'TENANT_SUSPENDED') {
      clearSession();
      window.dispatchEvent(new Event('unauthorized'));
    }
    if (!quiet && response.status !== 401) announce(err);
    throw err;
  }
  if (raw) return response;
  if (response.status === 204) return null;
  const type = response.headers.get('content-type') || '';
  return type.includes('application/json') ? response.json() : response.text();
}

function filenameFrom(response, fallback) {
  const cd = response.headers.get('content-disposition') || '';
  const star = /filename\*=UTF-8''([^;]+)/i.exec(cd);
  if (star) {
    try {
      return decodeURIComponent(star[1]);
    } catch {
      /* fall through */
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(cd);
  return plain ? plain[1] : fallback;
}

export const api = {
  get: (endpoint, opts) => request('GET', endpoint, opts),
  post: (endpoint, data, opts = {}) => request('POST', endpoint, { ...opts, body: data ?? {} }),
  put: (endpoint, data, opts = {}) => request('PUT', endpoint, { ...opts, body: data ?? {} }),
  patch: (endpoint, data, opts = {}) => request('PATCH', endpoint, { ...opts, body: data ?? {} }),
  delete: (endpoint, opts) => request('DELETE', endpoint, opts),
  postFormData: (endpoint, formData, opts = {}) => request('POST', endpoint, { ...opts, formData }),

  // download fetches a file with the session and saves it in the browser.
  async download(endpoint, fallbackName = 'download', opts = {}) {
    const res = await request('GET', endpoint, { ...opts, raw: true });
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filenameFrom(res, fallbackName);
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  },

  // openBlob fetches a file with the session and opens it in a new tab
  // (receipt PDFs). The tab is opened first so pop-up blockers allow it.
  async openBlob(endpoint, opts = {}) {
    const win = window.open('', '_blank');
    try {
      const res = await request('GET', endpoint, { ...opts, raw: true });
      const url = URL.createObjectURL(await res.blob());
      if (win) win.location.href = url;
      else window.location.assign(url);
      setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err) {
      win?.close();
      throw err;
    }
  },
};

// Session calls used by the auth context.
export const authApi = {
  login: (email, password, remember) => request('POST', '/api/auth/login', { body: { email, password, remember }, quiet: true }),
  logout: () => request('POST', '/api/auth/logout', { quiet: true }).catch(() => null),
  me: () => request('GET', '/api/auth/me', { quiet: true }),
};

export function apiUrl(endpoint) {
  return `${API_BASE_URL}${endpoint}`;
}
