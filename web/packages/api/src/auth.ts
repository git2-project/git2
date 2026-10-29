import { request } from "./client";
import { ApiError } from "./errors";
import { clearTokens, loadTokens, storeTokensFrom } from "./tokens";

// User is auth-svc's UserResponse.
export interface User {
  id: string;
  email: string;
  username: string;
  created_at: string;
}

export function hasSession(): boolean {
  return loadTokens() !== null;
}

export async function login(email: string, password: string): Promise<User> {
  const res = await request("/api/auth/login", { method: "POST", body: { email, password } });
  if (!storeTokensFrom(res)) throw new ApiError(500, "sign-in response had no tokens");
  const body = (await res.json()) as { user: User };
  return body.user;
}

export async function register(email: string, username: string, password: string): Promise<User> {
  const res = await request("/api/auth/register", {
    method: "POST",
    body: { email, username, password },
  });
  return (await res.json()) as User;
}

export async function me(): Promise<User> {
  const res = await request("/api/auth/me", { auth: true });
  return (await res.json()) as User;
}

// logout revokes the session on the server and always clears it locally, even
// when the server cannot be reached.
export async function logout(): Promise<void> {
  const tokens = loadTokens();
  clearTokens();
  if (!tokens) return;
  try {
    await fetch("/api/auth/logout", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${tokens.access}`,
      },
      body: JSON.stringify({ refresh_token: tokens.refresh }),
    });
  } catch {
    // Local sign-out already happened; the tokens expire on their own.
  }
}
