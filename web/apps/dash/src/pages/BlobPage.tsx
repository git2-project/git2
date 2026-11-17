import { ApiError, getBlob, type Blob, type RepoName } from "@git2/api";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Breadcrumb } from "../components/Breadcrumb";
import { CodeView } from "../components/CodeView";
import { RefPicker } from "../components/RefPicker";
import { Await } from "../components/Status";
import { downloadFile } from "../lib/download";
import { fileSize } from "../lib/format";
import { guessLanguage } from "../lib/highlight";
import { blobLink, commitsLink } from "../lib/paths";
import { useApi } from "../lib/useApi";
import { useRepo } from "../repo/RepoContext";

// BlobPage shows one file at a ref.
export function BlobPage() {
  const { name } = useRepo();
  const params = useParams();
  const gitRef = params.ref ?? "";
  const path = params["*"] ?? "";
  const state = useApi((signal) => getBlob(name, path, { ref: gitRef, signal }), [name, gitRef, path]);

  return (
    <section className="dash-page">
      <div className="dash-toolbar">
        <RefPicker name={name} current={gitRef} to={(ref) => blobLink(name, ref, path)} />
        <Breadcrumb name={name} gitRef={gitRef} path={path} />
      </div>
      <Await state={state} notFound={`This file does not exist on ${gitRef}.`}>
        {(blob) => <BlobView name={name} gitRef={gitRef} blob={blob} />}
      </Await>
    </section>
  );
}

export function BlobView({ name, gitRef, blob }: { name: RepoName; gitRef: string; blob: Blob }) {
  const content = blob.content ?? "";
  const lineCount = content ? content.split("\n").length - (content.endsWith("\n") ? 1 : 0) : 0;

  return (
    <div className="dash-box">
      <div className="dash-box-header">
        <span className="g2-muted">
          {!blob.binary && `${lineCount} ${lineCount === 1 ? "line" : "lines"} · `}
          {fileSize(blob.size)}
        </span>
        <span className="dash-actions">
          <Link to={commitsLink(name, gitRef, blob.path)}>History</Link>
          <Link to={blobLink(name, blob.commit_sha, blob.path)} title="Link to this version of the file">
            Permalink
          </Link>
          <DownloadButton name={name} gitRef={blob.commit_sha} path={blob.path} />
        </span>
      </div>
      <BlobBody blob={blob} name={name} />
    </div>
  );
}

function BlobBody({ blob, name }: { blob: Blob; name: RepoName }) {
  if (blob.binary) {
    return (
      <div className="dash-box-message">
        <p>Binary file not shown.</p>
        <DownloadButton name={name} gitRef={blob.commit_sha} path={blob.path} primary />
      </div>
    );
  }
  const content = blob.content ?? "";
  return (
    <>
      {blob.lfs && (
        <p className="g2-notice dash-box-note">
          Stored with Git LFS. This is the pointer file; the content lives in the LFS store.
        </p>
      )}
      {blob.truncated && (
        <p className="g2-notice dash-box-note">
          This file is larger than 1 MiB; only the first 1 MiB is shown. Download it to see all of it.
        </p>
      )}
      {content === "" ? (
        <p className="dash-box-message g2-muted">This file is empty.</p>
      ) : (
        <CodeView code={content} language={blob.lfs ? null : (blob.language ?? guessLanguage(blob.name))} />
      )}
    </>
  );
}

function DownloadButton({
  name,
  gitRef,
  path,
  primary,
}: {
  name: RepoName;
  gitRef: string;
  path: string;
  primary?: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onClick = () => {
    setBusy(true);
    setError(null);
    downloadFile(name, path, gitRef)
      .catch((err: unknown) => {
        const status = err instanceof ApiError ? err.status : 0;
        setError(status === 413 ? "Larger than 10 MiB: use git clone to get it." : "Download failed.");
      })
      .finally(() => setBusy(false));
  };

  return (
    <span className="dash-download">
      <button
        type="button"
        className={primary ? "g2-button g2-primary" : "dash-link-button"}
        onClick={onClick}
        disabled={busy}
      >
        {busy ? "Downloading…" : "Download"}
      </button>
      {error && (
        <span className="dash-inline-error" role="alert">
          {error}
        </span>
      )}
    </span>
  );
}
