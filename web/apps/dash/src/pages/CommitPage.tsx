import { getCommit, type CommitDetail, type RepoName } from "@git2/api";
import { Link, useParams } from "react-router-dom";
import { DiffCounts, FileDiffView } from "../components/DiffView";
import { Await } from "../components/Status";
import { fullDate, timeAgo } from "../lib/format";
import { commitLink, shortSha, treeLink } from "../lib/paths";
import { useApi } from "../lib/useApi";
import { useRepo } from "../repo/RepoContext";
import { Author } from "./CommitsPage";

// CommitPage shows one commit and its changes against the first parent.
export function CommitPage() {
  const { name } = useRepo();
  const { sha = "" } = useParams();
  const state = useApi((signal) => getCommit(name, sha, signal), [name, sha]);

  return (
    <Await state={state} notFound="This commit does not exist in the repository.">
      {(commit) => <CommitView name={name} commit={commit} />}
    </Await>
  );
}

function CommitView({ name, commit }: { name: RepoName; commit: CommitDetail }) {
  const body = commit.message.slice(commit.title.length).replace(/^\n+/, "");
  const sameCommitter =
    commit.author.email === commit.committer.email && commit.author.name === commit.committer.name;

  return (
    <section className="dash-page">
      <div className="dash-box dash-commit-header">
        <div className="dash-box-header">
          <h2 className="dash-commit-heading">{commit.title}</h2>
          <Link to={treeLink(name, commit.sha)} className="g2-button">
            Browse files
          </Link>
        </div>
        {body && <pre className="dash-commit-body">{body}</pre>}
        <div className="dash-commit-meta">
          <span>
            <Author signature={commit.author} /> authored{" "}
            <span title={fullDate(commit.author.date)}>{timeAgo(commit.author.date)}</span>
            {!sameCommitter && (
              <>
                {" · "}
                <Author signature={commit.committer} /> committed{" "}
                <span title={fullDate(commit.committer.date)}>{timeAgo(commit.committer.date)}</span>
              </>
            )}
          </span>
          <span className="dash-actions">
            {commit.parent_shas.length > 0 && (
              <span className="g2-muted">
                {commit.parent_shas.length === 1 ? "parent" : "parents"}{" "}
                {commit.parent_shas.map((p, i) => (
                  <span key={p}>
                    {i > 0 && " + "}
                    <Link to={commitLink(name, p)} className="dash-sha">
                      {shortSha(p)}
                    </Link>
                  </span>
                ))}
              </span>
            )}
            <span className="g2-muted">
              commit <code className="dash-sha">{commit.sha}</code>
            </span>
          </span>
        </div>
      </div>

      <p className="dash-diff-summary">
        <strong>
          {commit.stats.files_changed} {commit.stats.files_changed === 1 ? "file" : "files"} changed
        </strong>{" "}
        <DiffCounts additions={commit.stats.additions} deletions={commit.stats.deletions} />
        {commit.parent_shas.length > 1 && <span className="g2-muted"> · compared with the first parent</span>}
      </p>

      {commit.truncated && (
        <p className="g2-notice">
          More than 300 files changed; only the first 300 are shown. Clone the repository to see the rest.
        </p>
      )}

      {commit.files.map((f) => (
        <FileDiffView key={`${f.old_path}->${f.new_path}`} name={name} sha={commit.sha} file={f} />
      ))}
    </section>
  );
}
