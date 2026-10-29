// Both tokens live in localStorage so that Dash and Admin, served from the same
// origin, share one sign-in. If storage is blocked the session still works for
// the lifetime of the page through the in-memory copy.
export const TOKENS_STORAGE_KEY = "git2.tokens";

export interface Tokens {
  access: string;
  refresh: string;
  // Epoch milliseconds; informational only, the server is the authority.
  accessExpiresAt: number;
}

let memory: Tokens | null = null;

export function loadTokens(): Tokens | null {
  try {
    const raw = localStorage.getItem(TOKENS_STORAGE_KEY);
    if (raw !== null) return JSON.parse(raw) as Tokens;
    // Another tab signed out: storage is the shared truth when it is readable.
    memory = null;
    return null;
  } catch {
    return memory;
  }
}

export function saveTokens(tokens: Tokens): void {
  memory = tokens;
  try {
    localStorage.setItem(TOKENS_STORAGE_KEY, JSON.stringify(tokens));
  } catch {
    // Storage blocked: keep the in-memory copy only.
  }
}

export function clearTokens(): void {
  memory = null;
  try {
    localStorage.removeItem(TOKENS_STORAGE_KEY);
  } catch {
    // Nothing to clear.
  }
}

// Reads the token pair auth-svc sends in response headers (never in bodies).
// Returns false when the response carries no complete pair.
export function storeTokensFrom(res: Response): boolean {
  const access = res.headers.get("X-Access-Token");
  const refresh = res.headers.get("X-Refresh-Token");
  if (!access || !refresh) return false;
  const expiresIn = Number(res.headers.get("X-Access-Expires-In") ?? "0");
  saveTokens({ access, refresh, accessExpiresAt: Date.now() + expiresIn * 1000 });
  return true;
}
