import type { CommitRef, RepoName } from "@git2/api";
import { Link } from "react-router-dom";
import { fullDate, timeAgo } from "../lib/format";
import { commitLink, shortSha } from "../lib/paths";

// CommitLine is the one-line summary of a commit: title, short SHA, age.
export function CommitLine({ name, commit }: { name: RepoName; commit: CommitRef }) {
  return (
    <span className="dash-commit-line">
      <Link to={commitLink(name, commit.sha)} className="dash-commit-title">
        {commit.title}
      </Link>
      <Link to={commitLink(name, commit.sha)} className="dash-sha">
        {shortSha(commit.sha)}
      </Link>
      <span className="g2-muted dash-nowrap" title={fullDate(commit.committed_at)}>
        {timeAgo(commit.committed_at)}
      </span>
    </span>
  );
}
