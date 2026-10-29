import { AppShell, RegisterPage, RequireAuth, SignInPage, useAuth } from "@git2/ui";
import { Navigate, Route, Routes } from "react-router-dom";
import { dashIntro } from "./intro";

const adminPlane = { label: "Admin", href: "/admin/" };

export function App() {
  return (
    <Routes>
      <Route path="/sign-in" element={<SignInPage plane="Dash" intro={dashIntro} otherPlane={adminPlane} registerPath="/register" />} />
      <Route path="/register" element={<RegisterPage plane="Dash" intro={dashIntro} otherPlane={adminPlane} />} />
      <Route element={<RequireAuth />}>
        <Route element={<AppShell plane="Dash" otherPlane={adminPlane} />}>
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
      <h1>Welcome, {user?.username}</h1>
      <p className="g2-muted">Repositories, merge requests, issues and pipelines will appear here.</p>
    </>
  );
}
