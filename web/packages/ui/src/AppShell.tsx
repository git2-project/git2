import type { ReactNode } from "react";
import { Outlet } from "react-router-dom";
import { useAuth } from "./AuthProvider";
import { ThemeToggle } from "./ThemeToggle";

interface AppShellProps {
  plane: string;
  // The other plane, linked with a full page load: Dash and Admin are separate
  // apps on the same origin, so the session carries over.
  otherPlane?: { label: string; href: string };
  children?: ReactNode;
}

export function AppShell({ plane, otherPlane, children }: AppShellProps) {
  const { user, signOut } = useAuth();

  return (
    <div className="g2-shell">
      <header className="g2-topbar">
        <span className="g2-brand">
          git2 <strong>{plane}</strong>
        </span>
        <nav className="g2-topbar-actions">
          {otherPlane && <a href={otherPlane.href}>{otherPlane.label}</a>}
          {user && <span className="g2-muted">{user.username}</span>}
          <ThemeToggle />
          <button type="button" className="g2-button" onClick={() => void signOut()}>
            Sign out
          </button>
        </nav>
      </header>
      <main className="g2-content">{children ?? <Outlet />}</main>
    </div>
  );
}
