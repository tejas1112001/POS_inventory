// The signed-in session lives in memory only (API_CONTRACT C-02): the
// access token, user and permissions are never written to localStorage or
// sessionStorage. A page reload gets a new access token from the refresh
// cookie (HttpOnly, set by the server), which scripts cannot read.

let state = { token: null, user: null, permissions: [] };
const listeners = new Set();

export function getSession() {
  return state;
}

export function getAccessToken() {
  return state.token;
}

export function setSession({ token, user, permissions }) {
  state = {
    token: token ?? null,
    user: user ?? null,
    permissions: Array.isArray(permissions) ? permissions : [],
  };
  listeners.forEach((fn) => fn(state));
}

export function updateSession(patch) {
  setSession({ ...state, ...patch });
}

export function clearSession() {
  setSession({ token: null, user: null, permissions: [] });
}

export function subscribeSession(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

// Seconds until the access token expires (from its exp claim). The claim is
// only used to schedule a refresh; the server checks the signature.
export function tokenSecondsLeft(token = state.token) {
  if (!token) return 0;
  try {
    const payload = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
    return payload.exp ? payload.exp - Date.now() / 1000 : 0;
  } catch {
    return 0;
  }
}
