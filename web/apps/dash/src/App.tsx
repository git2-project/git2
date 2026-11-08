import { AppShell, RegisterPage, RequireAuth, SignInPage } from "@git2/ui";
import { Navigate, Route, Routes } from "react-router-dom";
import "./dash.css";
import { dashIntro } from "./intro";
import { BlobPage } from "./pages/BlobPage";
import { CommitPage } from "./pages/CommitPage";
import { CommitsPage } from "./pages/CommitsPage";
import { RepoListPage } from "./pages/RepoListPage";
import { TreePage } from "./pages/TreePage";
import { RepoLayout } from "./repo/RepoLayout";

// Dash does not link to Admin: users and owners sign in here, and admins open
// Admin at its own URL. The top-bar link can come back for role "admin" once
// the user response carries a role (WBS-3.1).
//
// Self-registration exists only in development; production builds drop the
// route and the links. Production accounts come from Admin (WBS-3.1).
//
// Code browser (WBS-6.1): a ref can contain "/", so it is one encoded path
// segment (see lib/paths.ts); the file path is the splat after it.
export function App() {
  return (
    <Routes>
      <Route
        path="/sign-in"
        element={
          <SignInPage
            plane="Dash"
            intro={dashIntro}
            registerPath={import.meta.env.DEV ? "/register" : undefined}
          />
        }
      />
      {import.meta.env.DEV && (
        <Route path="/register" element={<RegisterPage plane="Dash" intro={dashIntro} />} />
      )}
      <Route element={<RequireAuth />}>
        <Route element={<AppShell plane="Dash" />}>
          <Route index element={<RepoListPage />} />
          <Route path=":owner/:repo" element={<RepoLayout />}>
            <Route index element={<TreePage />} />
            <Route path="tree/:ref/*" element={<TreePage />} />
            <Route path="blob/:ref/*" element={<BlobPage />} />
            <Route path="commits/:ref" element={<CommitsPage />} />
            <Route path="commit/:sha" element={<CommitPage />} />
          </Route>
        </Route>
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
