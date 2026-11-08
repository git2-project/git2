import type { RepoName } from "@git2/api";
import { Fragment } from "react";
import { Link } from "react-router-dom";
import { treeLink } from "../lib/paths";

// Breadcrumb shows repo / dir / dir / file; every part but the last links
// to its directory at the same ref.
export function Breadcrumb({ name, gitRef, path }: { name: RepoName; gitRef: string; path: string }) {
  const parts = path ? path.split("/") : [];
  return (
    <nav className="dash-breadcrumb" aria-label="Path">
      <Link to={treeLink(name, gitRef)}>{name.repo}</Link>
      {parts.map((part, i) => {
        const last = i === parts.length - 1;
        return (
          <Fragment key={i}>
            <span className="g2-muted" aria-hidden="true">
              /
            </span>
            {last ? (
              <strong aria-current="page">{part}</strong>
            ) : (
              <Link to={treeLink(name, gitRef, parts.slice(0, i + 1).join("/"))}>{part}</Link>
            )}
          </Fragment>
        );
      })}
    </nav>
  );
}
