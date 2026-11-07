// Browser storage keeps only two sign-in preferences: the saved email and
// "remember me". Tokens, the user and permissions stay in memory
// (lib/session.js; API_CONTRACT C-02).

const REMEMBER_KEY = 'rememberSession';
const SAVED_EMAIL_KEY = 'savedEmail';

// Keys the old frontend used for tokens and user data.
const LEGACY_KEYS = ['token', 'currentUser', 'userPermissions'];

function safe(fn, fallback) {
  try {
    return fn();
  } catch {
    return fallback;
  }
}

// purgeLegacyAuthData removes tokens an older version left in storage.
export function purgeLegacyAuthData() {
  safe(() => {
    for (const key of LEGACY_KEYS) {
      localStorage.removeItem(key);
      sessionStorage.removeItem(key);
    }
  });
}

export function getRememberSession() {
  return safe(() => localStorage.getItem(REMEMBER_KEY) !== 'false', true);
}

export function setRememberSession(remember) {
  safe(() => localStorage.setItem(REMEMBER_KEY, remember ? 'true' : 'false'));
}

export function getSavedEmail() {
  return safe(() => localStorage.getItem(SAVED_EMAIL_KEY) || '', '');
}

export function setSavedEmail(email) {
  safe(() => {
    if (email) localStorage.setItem(SAVED_EMAIL_KEY, email);
    else localStorage.removeItem(SAVED_EMAIL_KEY);
  });
}
