import { getBlob, type RepoName } from "@git2/api";
import { Link } from "react-router-dom";
import { CodeView } from "../components/CodeView";
import { guessLanguage } from "../lib/highlight";
import { blobLink } from "../lib/paths";
import { useApi } from "../lib/useApi";

// Readme shows a directory's README under its listing. It is highlighted
// source for now; rendering Markdown comes later.
export function Readme({ name, gitRef, path }: { name: RepoName; gitRef: string; path: string }) {
  const state = useApi((signal) => getBlob(name, path, { ref: gitRef, signal }), [name, gitRef, path]);
  // A README that cannot be read should not take the listing down with it.
  if (state.status !== "ok" || state.data.binary || !state.data.content) return null;
  const blob = state.data;
  return (
    <div className="dash-box dash-readme">
      <div className="dash-box-header">
        <Link to={blobLink(name, gitRef, path)}>
          <strong>{blob.name}</strong>
        </Link>
      </div>
      <CodeView code={blob.content ?? ""} language={blob.language ?? guessLanguage(blob.name)} />
    </div>
  );
}
