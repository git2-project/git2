import { getRepository } from "@git2/api";
import { useMemo, useState } from "react";
import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import { Await } from "../components/Status";
import { commitsLink, repoHome, settingsLink } from "../lib/paths";
import { useApi } from "../lib/useApi";
import { RepoContext } from "./RepoContext";

// RepoLayout loads the repository once for every page under /:owner/:repo
// and shows its header and tabs.
export function RepoLayout() {
  const { owner = "", repo = "" } = useParams();
  const name = useMemo(() => ({ owner, repo }), [owner, repo]);
  const state = useApi((signal) => getRepository(name, signal), [name]);

  return (
    <Await state={state} notFound="This repository does not exist, or you do not have access to it.">
      {(repository) => (
        <RepoContext.Provider value={{ name, repository }}>
          <header className="dash-repo-header">
            <div className="dash-repo-title">
              <h1>
                <span className="g2-muted">{owner}</span> / <Link to={repoHome(name)}>{repo}</Link>
              </h1>
              <CloneURL url={repository.clone_url} />
            </div>
            {repository.description && <p className="g2-muted">{repository.description}</p>}
            <RepoTabs
              home={repoHome(name)}
              commits={commitsLink(name, repository.default_branch)}
              settings={settingsLink(name)}
            />
          </header>
          <Outlet />
        </RepoContext.Provider>
      )}
    </Await>
  );
}

// RepoTabs: Code is active on the home, tree and blob pages, Commits on the
// history and on a single commit, Settings on the settings page. Settings
// shows for everyone until the repository carries the caller's permission;
// dash-api refuses what the caller may not do.
function RepoTabs({ home, commits, settings }: { home: string; commits: string; settings: string }) {
  const { pathname } = useLocation();
  const rest = pathname.slice(home.length);
  const tab =
    rest.startsWith("/commits/") || rest.startsWith("/commit/")
      ? "commits"
      : rest === "/settings" || rest.startsWith("/settings/")
        ? "settings"
        : "code";
  const current = (t: string) => (tab === t ? "page" : undefined);
  return (
    <nav className="dash-tabs" aria-label="Repository">
      <Link to={home} aria-current={current("code")}>
        Code
      </Link>
      <Link to={commits} aria-current={current("commits")}>
        Commits
      </Link>
      <Link to={settings} aria-current={current("settings")}>
        Settings
      </Link>
    </nav>
  );
}

function CloneURL({ url }: { url: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(url).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };
  return (
    <div className="dash-clone">
      <code>{url}</code>
      <button type="button" className="g2-button" onClick={copy}>
        {copied ? "Copied" : "Copy"}
      </button>
    </div>
  );
}
