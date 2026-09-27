const TOKEN_KEY = 'ar.token';

let authToken: string | null = null;

function loadToken(): string | null {
  if (typeof window === 'undefined') return null;
  const fromHash = window.location.hash.match(/#token=([^&]+)/);
  if (fromHash && fromHash[1]) {
    const t = decodeURIComponent(fromHash[1]);
    try {
      sessionStorage.setItem(TOKEN_KEY, t);
    } catch {
    }
    const url = new URL(window.location.href);
    url.hash = '';
    window.history.replaceState(null, '', url.toString());
    return t;
  }
  try {
    return sessionStorage.getItem(TOKEN_KEY) ?? localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setAuthToken(token: string, remember = false): void {
  authToken = token;
  try {
    if (remember) localStorage.setItem(TOKEN_KEY, token);
    else sessionStorage.setItem(TOKEN_KEY, token);
  } catch {
  }
}

export function clearAuthToken(): void {
  authToken = null;
  try {
    sessionStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(TOKEN_KEY);
  } catch {
  }
}

export function getAuthToken(): string | null {
  if (authToken === null) authToken = loadToken();
  return authToken;
}

export function hasAuthToken(): boolean {
  return getAuthToken() !== null;
}
