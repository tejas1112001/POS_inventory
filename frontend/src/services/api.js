import {
  getToken,
  isTokenExpired,
} from '../lib/authStorage';

const isDev = import.meta.env?.DEV ||
  window.location.hostname === 'localhost' ||
  window.location.hostname === '127.0.0.1';

const normalizeBaseUrl = (value) => (value ? value.replace(/\/+$/, '') : '');
const API_BASE_URL = normalizeBaseUrl(
  import.meta.env?.VITE_API_URL || (isDev ? 'http://localhost:3000' : '')
);

let refreshPromise = null;

async function tryRefreshToken() {
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    const token = getToken();
    if (!token) return false;

    try {
      const response = await fetch(`${API_BASE_URL}/api/auth/refresh`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ remember: localStorage.getItem('rememberSession') === 'true' }),
      });

      if (!response.ok) return false;

      const data = await response.json();
      const storage = localStorage.getItem('rememberSession') === 'true' ? localStorage : sessionStorage;
      storage.setItem('token', data.token);
      storage.setItem('currentUser', JSON.stringify(data.user));
      return true;
    } catch {
      return false;
    } finally {
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

const handleResponse = async (response) => {
  if (!response.ok) {
    let errorPayload = { error: `HTTP error! status: ${response.status}` };
    const contentType = response.headers.get('content-type') || '';

    if (contentType.includes('application/json')) {
      errorPayload = await response.json().catch(() => errorPayload);
    } else {
      const text = await response.text().catch(() => '');
      if (text) errorPayload = { error: text };
    }

    if (response.status === 401) {
      window.dispatchEvent(new CustomEvent('apiError', { detail: errorPayload.error || 'Your session has expired. Please sign in again.' }));
      window.dispatchEvent(new Event('unauthorized'));
    } else {
      window.dispatchEvent(new CustomEvent('apiError', { detail: errorPayload.error || `HTTP error! status: ${response.status}` }));
    }
    throw new Error(errorPayload.error || `HTTP error! status: ${response.status}`);
  }

  if (response.status === 204) {
    return null;
  }

  const contentType = response.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return response.json();
  }
  return response.text();
};

const getAuthToken = () => getToken();

const getHeaders = () => {
  const headers = {
    'Content-Type': 'application/json',
  };
  const token = getAuthToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
};

async function fetchWithAuth(endpoint, options = {}, retried = false) {
  const token = getAuthToken();
  if (token && isTokenExpired(token, 120) && !endpoint.includes('/api/auth/')) {
    await tryRefreshToken();
  }

  const response = await fetch(`${API_BASE_URL}${endpoint}`, options);

  if (response.status === 401 && !retried &&
      !endpoint.includes('/api/auth/login') && !endpoint.includes('/api/auth/refresh')) {
    const refreshed = await tryRefreshToken();
    if (refreshed) {
      const newHeaders = { ...options.headers };
      const newToken = getAuthToken();
      if (newToken) {
        newHeaders['Authorization'] = `Bearer ${newToken}`;
      }
      return fetchWithAuth(endpoint, { ...options, headers: newHeaders }, true);
    }
  }

  return handleResponse(response);
}

export const api = {
  async get(endpoint) {
    try {
      return await fetchWithAuth(endpoint, { headers: getHeaders() });
    } catch (error) {
      console.error('API GET Error:', error);
      throw error;
    }
  },

  async post(endpoint, data) {
    try {
      return await fetchWithAuth(endpoint, {
        method: 'POST',
        headers: getHeaders(),
        body: JSON.stringify(data),
      });
    } catch (error) {
      console.error('API POST Error:', error);
      throw error;
    }
  },

  async put(endpoint, data) {
    try {
      return await fetchWithAuth(endpoint, {
        method: 'PUT',
        headers: getHeaders(),
        body: JSON.stringify(data),
      });
    } catch (error) {
      console.error('API PUT Error:', error);
      throw error;
    }
  },

  async postFormData(endpoint, data) {
    const headers = {};
    const token = getAuthToken();
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    try {
      return await fetchWithAuth(endpoint, {
        method: 'POST',
        headers,
        body: data,
      });
    } catch (error) {
      console.error('API POST FormData Error:', error);
      throw error;
    }
  },

  async delete(endpoint) {
    try {
      return await fetchWithAuth(endpoint, {
        method: 'DELETE',
        headers: getHeaders(),
      });
    } catch (error) {
      console.error('API DELETE Error:', error);
      throw error;
    }
  },
};
