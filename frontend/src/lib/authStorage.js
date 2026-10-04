const TOKEN_KEY = 'token';
const USER_KEY = 'currentUser';
const PERMISSIONS_KEY = 'userPermissions';
const REMEMBER_KEY = 'rememberSession';
const SAVED_EMAIL_KEY = 'savedEmail';

export function getRememberSession() {
  return localStorage.getItem(REMEMBER_KEY) === 'true';
}

export function setRememberSession(remember) {
  localStorage.setItem(REMEMBER_KEY, remember ? 'true' : 'false');
}

export function getAuthStorage() {
  return getRememberSession() ? localStorage : sessionStorage;
}

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || sessionStorage.getItem(TOKEN_KEY);
}

export function setAuthData({ user, token, permissions, remember }) {
  setRememberSession(remember);
  const storage = remember ? localStorage : sessionStorage;
  const other = remember ? sessionStorage : localStorage;

  storage.setItem(TOKEN_KEY, token);
  storage.setItem(USER_KEY, JSON.stringify(user));
  if (Array.isArray(permissions)) {
    storage.setItem(PERMISSIONS_KEY, JSON.stringify(permissions));
  }
  other.removeItem(TOKEN_KEY);
  other.removeItem(USER_KEY);
  other.removeItem(PERMISSIONS_KEY);
}

export function getStoredUser() {
  const raw =
    localStorage.getItem(USER_KEY) || sessionStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function getStoredPermissions() {
  const raw =
    localStorage.getItem(PERMISSIONS_KEY) || sessionStorage.getItem(PERMISSIONS_KEY);
  if (!raw) return [];
  try {
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

export function setStoredPermissions(permissions) {
  const storage = getAuthStorage();
  if (Array.isArray(permissions)) {
    storage.setItem(PERMISSIONS_KEY, JSON.stringify(permissions));
  }
}

export function clearAuthData() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  localStorage.removeItem(PERMISSIONS_KEY);
  sessionStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(USER_KEY);
  sessionStorage.removeItem(PERMISSIONS_KEY);
}

export function getSavedEmail() {
  return localStorage.getItem(SAVED_EMAIL_KEY) || '';
}

export function setSavedEmail(email) {
  if (email) {
    localStorage.setItem(SAVED_EMAIL_KEY, email);
  } else {
    localStorage.removeItem(SAVED_EMAIL_KEY);
  }
}

export function decodeJwtPayload(token) {
  if (!token) return null;
  try {
    const payload = token.split('.')[1];
    const decoded = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
    return JSON.parse(decoded);
  } catch {
    return null;
  }
}

export function isTokenExpired(token, bufferSeconds = 60) {
  const payload = decodeJwtPayload(token);
  if (!payload?.exp) return true;
  return Date.now() >= (payload.exp - bufferSeconds) * 1000;
}
