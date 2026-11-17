import { getRepository } from "@git2/api";
import { useMemo, useState } from "react";
import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import { Await } from "../components/Status";
import { commitsLink, repoHome } from "../lib/paths";
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
            <RepoTabs home={repoHome(name)} commits={commitsLink(name, repository.default_branch)} />
          </header>
          <Outlet />
        </RepoContext.Provider>
      )}
    </Await>
  );
}

// RepoTabs: Code is active on the home, tree and blob pages, Commits on the
// history and on a single commit.
function RepoTabs({ home, commits }: { home: string; commits: string }) {
  const { pathname } = useLocation();
  const rest = pathname.slice(home.length);
  const onCommits = rest.startsWith("/commits/") || rest.startsWith("/commit/");
  return (
    <nav className="dash-tabs" aria-label="Repository">
      <Link to={home} aria-current={onCommits ? undefined : "page"}>
        Code
      </Link>
      <Link to={commits} aria-current={onCommits ? "page" : undefined}>
        Commits
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
