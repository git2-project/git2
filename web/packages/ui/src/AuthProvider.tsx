import {
  ApiError,
  TOKENS_STORAGE_KEY,
  hasSession,
  login,
  logout,
  me,
  onSessionEnd,
  type User,
} from "@git2/api";
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";

export type AuthStatus = "loading" | "signed-in" | "signed-out" | "unavailable";

interface AuthState {
  status: AuthStatus;
  user: User | null;
  // Set when status is "unavailable": the server could not confirm the session.
  error: string | null;
  // True when the server ended the session (expired or revoked), as opposed to
  // the user signing out. The sign-in page explains it.
  sessionExpired: boolean;
  signIn: (email: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
  retry: () => void;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>(() => (hasSession() ? "loading" : "signed-out"));
  const [user, setUser] = useState<User | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sessionExpired, setSessionExpired] = useState(false);
  const [attempt, setAttempt] = useState(0);

  // Confirm a stored session with the server. A 401 means it is gone; any other
  // failure keeps the tokens and shows "unavailable" so an outage never signs
  // the user out.
  useEffect(() => {
    if (!hasSession()) {
      setStatus("signed-out");
      return;
    }
    let cancelled = false;
    me()
      .then((u) => {
        if (cancelled) return;
        setUser(u);
        setStatus("signed-in");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          setUser(null);
          setSessionExpired(true);
          setStatus("signed-out");
          return;
        }
        setError(err instanceof Error ? err.message : String(err));
        setStatus("unavailable");
      });
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  useEffect(() => {
    const signedOut = () => {
      setUser(null);
      setStatus("signed-out");
    };
    const offSessionEnd = onSessionEnd(() => {
      setSessionExpired(true);
      signedOut();
    });
    // Sign-in or sign-out in another tab or in the other plane.
    const onStorage = (e: StorageEvent) => {
      if (e.key !== TOKENS_STORAGE_KEY) return;
      if (e.newValue === null) signedOut();
      else if (e.oldValue === null) {
        setStatus("loading");
        setAttempt((n) => n + 1);
      }
    };
    window.addEventListener("storage", onStorage);
    return () => {
      offSessionEnd();
      window.removeEventListener("storage", onStorage);
    };
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    const u = await login(email, password);
    setUser(u);
    setError(null);
    setSessionExpired(false);
    setStatus("signed-in");
  }, []);

  const signOut = useCallback(async () => {
    await logout();
    setUser(null);
    setSessionExpired(false);
    setStatus("signed-out");
  }, []);

  const retry = useCallback(() => {
    setStatus("loading");
    setAttempt((n) => n + 1);
  }, []);

  return (
    <AuthContext.Provider value={{ status, user, error, sessionExpired, signIn, signOut, retry }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
