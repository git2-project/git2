import { ApiError, listRepositories, type Repository } from "@git2/api";
import { useAuth } from "@git2/ui";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ErrorView, Loading } from "../components/Status";
import { fullDate, timeAgo } from "../lib/format";
import { repoHome } from "../lib/paths";

// RepoListPage lists the caller's repositories, a page at a time.
export function RepoListPage() {
  const { user } = useAuth();
  const [repos, setRepos] = useState<Repository[]>([]);
  const [next, setNext] = useState<string | undefined>();
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);

  const load = (token: string | undefined, signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    listRepositories({ pageToken: token, signal }).then(
      (page) => {
        setRepos((prev) => (token ? [...prev, ...page.repositories] : page.repositories));
        setNext(page.next_page_token ?? undefined);
        setLoading(false);
      },
      (err: unknown) => {
        if (signal?.aborted) return;
        setError(err instanceof ApiError ? err : new ApiError(0, String(err)));
        setLoading(false);
      },
    );
  };

  useEffect(() => {
    const ctrl = new AbortController();
    load(undefined, ctrl.signal);
    return () => ctrl.abort();
  }, []);

  return (
    <section className="dash-page">
      <header className="dash-page-header">
        <h1>Repositories</h1>
        {user && <p className="g2-muted">Signed in as {user.username}</p>}
      </header>

      {repos.length > 0 && (
        <ul className="dash-list">
          {repos.map((r) => (
            <li key={r.id} className="dash-list-row dash-repo-row">
              <div>
                <Link to={repoHome({ owner: r.owner.name, repo: r.name })} className="dash-repo-name">
                  {r.full_name}
                </Link>
                {r.description && <p className="g2-muted">{r.description}</p>}
              </div>
              <span className="g2-muted dash-nowrap" title={fullDate(r.created_at)}>
                created {timeAgo(r.created_at)}
              </span>
            </li>
          ))}
        </ul>
      )}

      {!loading && !error && repos.length === 0 && (
        <div className="dash-empty">
          <p>You have no repositories yet.</p>
          <p className="g2-muted">Repositories you own or can read will appear here.</p>
        </div>
      )}

      {error && <ErrorView error={error} />}
      {loading && <Loading />}
      {!loading && next && (
        <button type="button" className="g2-button" onClick={() => load(next)}>
          Load more
        </button>
      )}
    </section>
  );
}
