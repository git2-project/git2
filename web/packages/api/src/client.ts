import { ApiError, errorFrom } from "./errors";
import { clearTokens, loadTokens, storeTokensFrom } from "./tokens";

type SessionEndListener = () => void;

const listeners = new Set<SessionEndListener>();

// onSessionEnd is called when the server rejects the session (a 401 that a
// refresh could not fix). Returns an unsubscribe function.
export function onSessionEnd(listener: SessionEndListener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function endSession(): void {
  clearTokens();
  listeners.forEach((l) => l());
}

async function send(path: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(path, init);
  } catch {
    throw new ApiError(0, "git2 could not be reached");
  }
}

let refreshing: Promise<boolean> | null = null;

export const REFRESH_LOCK_NAME = "git2.refresh";

// refreshSession swaps the refresh token for a new pair. auth-svc ends the
// session when an old refresh token is replayed, so no token may be sent
// twice: callers in this page share one in-flight request, and pages in other
// tabs (Dash and Admin share the tokens) take turns through a Web Lock.
export function refreshSession(): Promise<boolean> {
  if (!refreshing) {
    const seen = loadTokens()?.refresh;
    refreshing = withRefreshLock(() => doRefresh(seen)).finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}

function withRefreshLock(fn: () => Promise<boolean>): Promise<boolean> {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  // Without Web Locks (old browsers, jsdom) fall back to the in-page guard only.
  return locks ? locks.request(REFRESH_LOCK_NAME, fn) : fn();
}

// seen is the refresh token this page held before waiting for the lock.
async function doRefresh(seen: string | undefined): Promise<boolean> {
  const tokens = loadTokens();
  if (!tokens) return false;
  // Another tab refreshed while this one waited: its new pair is already
  // stored, and sending our old token would end the session.
  if (seen !== undefined && tokens.refresh !== seen) return true;
  const res = await send("/api/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: tokens.refresh }),
  });
  if (res.ok && storeTokensFrom(res)) return true;
  // Only a 401 means the session is gone. A 5xx is a server fault: keep the
  // tokens so the user is not signed out by an outage.
  if (res.status === 401) {
    endSession();
    return false;
  }
  throw await errorFrom(res);
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  // Send the access token and retry once through a refresh on 401.
  auth?: boolean;
}

// request sends one JSON request to the git2 API and returns the raw response
// when it is 2xx; any other status is thrown as an ApiError.
export async function request(path: string, opts: RequestOptions = {}): Promise<Response> {
  const { method = "GET", body, auth = false } = opts;

  const attempt = () => {
    const headers: Record<string, string> = {};
    if (body !== undefined) headers["Content-Type"] = "application/json";
    const tokens = auth ? loadTokens() : null;
    if (tokens) headers["Authorization"] = `Bearer ${tokens.access}`;
    return send(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  };

  let res = await attempt();
  if (auth && res.status === 401) {
    if (!(await refreshSession())) throw new ApiError(401, "session expired");
    res = await attempt();
    if (res.status === 401) {
      endSession();
      throw new ApiError(401, "session expired");
    }
  }
  if (!res.ok) throw await errorFrom(res);
  return res;
}
