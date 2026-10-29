import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "./AuthProvider";

// RequireAuth guards nested routes: it sends signed-out users to /sign-in and
// brings them back to the page they asked for after signing in.
export function RequireAuth() {
  const { status, error, retry } = useAuth();
  const location = useLocation();

  if (status === "loading") {
    return <p className="g2-center g2-muted">Loading…</p>;
  }
  if (status === "unavailable") {
    return (
      <div className="g2-center" role="alert">
        <p>git2 is unavailable right now{error ? `: ${error}` : ""}.</p>
        <button type="button" className="g2-button" onClick={retry}>
          Try again
        </button>
      </div>
    );
  }
  if (status === "signed-out") {
    return <Navigate to="/sign-in" replace state={{ from: location }} />;
  }
  return <Outlet />;
}
