import { ApiError, listCommits, type Commit, type RepoName, type Signature } from "@git2/api";
import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { RefPicker } from "../components/RefPicker";
import { ErrorView, Loading } from "../components/Status";
import { fullDate, timeAgo } from "../lib/format";
import { blobLink, commitLink, commitsLink, shortSha, treeLink } from "../lib/paths";
import { useRepo } from "../repo/RepoContext";

// CommitsPage is the history of a ref, optionally of one path, newest
// first, grouped by day.
export function CommitsPage() {
  const { name } = useRepo();
  const params = useParams();
  const [search] = useSearchParams();
  const gitRef = params.ref ?? "";
  const path = search.get("path") ?? "";

  const [commits, setCommits] = useState<Commit[]>([]);
  const [next, setNext] = useState<string | undefined>();
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);

  const load = (token: string | undefined, signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    listCommits(name, { ref: gitRef, path: path || undefined, pageToken: token, signal }).then(
      (page) => {
        setCommits((prev) => (token ? [...prev, ...page.commits] : page.commits));
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
    setCommits([]);
    load(undefined, ctrl.signal);
    return () => ctrl.abort();
    // load reads name, gitRef and path, listed here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [name, gitRef, path]);

  return (
    <section className="dash-page">
      <div className="dash-toolbar">
        <RefPicker name={name} current={gitRef} to={(ref) => commitsLink(name, ref, path || undefined)} />
        {path && (
          <span className="dash-history-path">
            History of <Link to={blobLinkOrTree(name, gitRef, path)}>{path}</Link>{" "}
            <Link to={commitsLink(name, gitRef)} className="g2-muted">
              (show all)
            </Link>
          </span>
        )}
      </div>

      {groupByDay(commits).map(([day, group]) => (
        <div key={day} className="dash-commit-group">
          <h2 className="dash-commit-day">Commits on {day}</h2>
          <ul className="dash-list">
            {group.map((c) => (
              <CommitRow key={c.sha} name={name} commit={c} />
            ))}
          </ul>
        </div>
      ))}

      {!loading && !error && commits.length === 0 && (
        <p className="dash-empty g2-muted">{path ? "No commits changed this path." : "No commits yet."}</p>
      )}
      {error && <ErrorView error={error} notFound={`The ref ${gitRef} does not exist.`} />}
      {loading && <Loading />}
      {!loading && next && (
        <button type="button" className="g2-button" onClick={() => load(next)}>
          Older commits
        </button>
      )}
    </section>
  );
}

// The history link of a file goes back to the file; we cannot tell a file
// from a directory by its path alone, so a path with an extension is
// treated as a file.
function blobLinkOrTree(name: RepoName, ref: string, path: string): string {
  const last = path.split("/").pop() ?? "";
  return last.includes(".") ? blobLink(name, ref, path) : treeLink(name, ref, path);
}

function CommitRow({ name, commit }: { name: RepoName; commit: Commit }) {
  return (
    <li className="dash-list-row dash-commit-row">
      <div className="dash-commit-main">
        <Link to={commitLink(name, commit.sha)} className="dash-commit-title">
          {commit.title}
        </Link>
        <span className="g2-muted">
          <Author signature={commit.author} /> committed{" "}
          <span title={fullDate(commit.committer.date)}>{timeAgo(commit.committer.date)}</span>
        </span>
      </div>
      <span className="dash-actions">
        <Link to={commitLink(name, commit.sha)} className="dash-sha">
          {shortSha(commit.sha)}
        </Link>
        <Link to={treeLink(name, commit.sha)} title="Browse the files at this commit">
          Browse
        </Link>
      </span>
    </li>
  );
}

export function Author({ signature }: { signature: Signature }) {
  return <strong title={signature.email}>{signature.user?.username ?? signature.name}</strong>;
}

// groupByDay keeps git's order and starts a group at each new day of the
// committer date, in the viewer's time zone.
function groupByDay(commits: Commit[]): [string, Commit[]][] {
  const groups: [string, Commit[]][] = [];
  for (const c of commits) {
    const day = new Date(c.committer.date).toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
    const last = groups[groups.length - 1];
    if (last && last[0] === day) last[1].push(c);
    else groups.push([day, [c]]);
  }
  return groups;
}
