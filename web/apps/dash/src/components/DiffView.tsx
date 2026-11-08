import type { FileDiff, RepoName } from "@git2/api";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { parsePatch } from "../lib/diff";
import { blobLink } from "../lib/paths";

const statusLabel: Record<FileDiff["status"], string> = {
  added: "Added",
  modified: "Modified",
  deleted: "Deleted",
  renamed: "Renamed",
  copied: "Copied",
};

// FileDiffView shows one changed file: its header and its hunks.
export function FileDiffView({ name, sha, file }: { name: RepoName; sha: string; file: FileDiff }) {
  const [open, setOpen] = useState(true);
  const path = file.new_path ?? file.old_path ?? "";
  const moved = file.status === "renamed" && file.old_path && file.new_path;

  return (
    <div className="dash-box dash-file-diff" id={`diff-${path}`}>
      <div className="dash-box-header">
        <button
          type="button"
          className="dash-link-button dash-diff-toggle"
          aria-expanded={open}
          aria-label={open ? "Collapse file" : "Expand file"}
          onClick={() => setOpen((o) => !o)}
        >
          {open ? "▾" : "▸"}
        </button>
        <span className={`dash-status dash-status-${file.status}`}>{statusLabel[file.status]}</span>
        <span className="dash-diff-path">
          {moved ? (
            <>
              {file.old_path} → {file.new_path}
            </>
          ) : (
            path
          )}
        </span>
        <DiffCounts additions={file.additions} deletions={file.deletions} />
        {file.status !== "deleted" && (
          <Link to={blobLink(name, sha, path)} className="dash-nowrap">
            View file
          </Link>
        )}
      </div>
      {open && <DiffBody file={file} />}
    </div>
  );
}

function DiffBody({ file }: { file: FileDiff }) {
  const lines = useMemo(() => (file.patch ? parsePatch(file.patch) : []), [file.patch]);
  if (file.binary) return <p className="dash-box-message g2-muted">Binary file not shown.</p>;
  if (file.truncated) {
    return <p className="dash-box-message g2-muted">This diff is too large to show. Clone the repository to see it.</p>;
  }
  if (lines.length === 0) {
    return (
      <p className="dash-box-message g2-muted">
        {file.status === "renamed" ? "File renamed without changes." : "Only the file mode changed."}
      </p>
    );
  }
  return (
    <div className="dash-diff-scroll">
      <table className="dash-diff">
        <tbody>
          {lines.map((l, i) =>
            l.kind === "hunk" ? (
              <tr key={i} className="dash-diff-hunk">
                <td colSpan={3}>{l.text}</td>
              </tr>
            ) : l.kind === "note" ? (
              <tr key={i} className="dash-diff-note">
                <td />
                <td />
                <td>{l.text}</td>
              </tr>
            ) : (
              <tr key={i} className={`dash-diff-${l.kind}`}>
                <td className="dash-diff-no">{l.oldNo ?? ""}</td>
                <td className="dash-diff-no">{l.newNo ?? ""}</td>
                <td className="dash-diff-text">
                  <span className="dash-diff-sign" aria-hidden="true">
                    {l.kind === "add" ? "+" : l.kind === "del" ? "-" : " "}
                  </span>
                  {l.text}
                </td>
              </tr>
            ),
          )}
        </tbody>
      </table>
    </div>
  );
}

export function DiffCounts({ additions, deletions }: { additions: number; deletions: number }) {
  return (
    <span className="dash-diff-counts">
      <span className="dash-add-count">+{additions}</span> <span className="dash-del-count">−{deletions}</span>
    </span>
  );
}
