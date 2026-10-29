import { AppShell, RequireAuth, SignInPage, useAuth } from "@git2/ui";
import { Navigate, Route, Routes } from "react-router-dom";
import { adminIntro } from "./intro";

const dashPlane = { label: "Dash", href: "/" };

// Admin has no self-registration: accounts come from Dash sign-up for now and
// from the Admin user management screens later (WBS-3.1).
export function App() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignInPage plane="Admin" intro={adminIntro} otherPlane={dashPlane} />} />
      <Route element={<RequireAuth />}>
        <Route element={<AppShell plane="Admin" otherPlane={dashPlane} />}>
          <Route index element={<Home />} />
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function Home() {
  const { user } = useAuth();
  return (
    <>
      <h1>Instance administration</h1>
      <p className="g2-muted">
        Signed in as {user?.username}. Users, teams, single sign-on, runners and backups will appear here.
      </p>
    </>
  );
}
