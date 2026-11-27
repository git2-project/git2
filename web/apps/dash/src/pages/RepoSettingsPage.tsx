import { deleteRepository } from "@git2/api";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { formError } from "../lib/errors";
import { useRepo } from "../repo/RepoContext";

// RepoSettingsPage holds the repository's settings. For now only deletion,
// which needs the name typed back (FR-26); dash-api checks maintain access.
// Settings → Access comes with repository access (docs/design/repo-access-v0.md).
export function RepoSettingsPage() {
  const { name, repository } = useRepo();
  const navigate = useNavigate();
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const confirmed = typed === repository.name;

  async function onDelete(e: FormEvent) {
    e.preventDefault();
    if (!confirmed) return;
    setDeleting(true);
    setError(null);
    try {
      await deleteRepository(name);
      navigate("/", { replace: true });
    } catch (err) {
      setError(formError(err));
      setDeleting(false);
    }
  }

  return (
    <section className="dash-page dash-narrow">
      <form className="dash-form dash-danger-zone" onSubmit={onDelete} noValidate>
        <h2>Delete this repository</h2>
        <p>
          This deletes <strong>{repository.full_name}</strong> and all of its Git data. It cannot be undone, and the
          name becomes free for a new repository.
        </p>
        {error && (
          <p className="g2-error" role="alert">
            {error}
          </p>
        )}
        <div className="dash-field">
          <label htmlFor="confirm-name">
            Type <strong>{repository.name}</strong> to confirm
          </label>
          <input
            id="confirm-name"
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
        </div>
        <div className="dash-form-actions">
          <button type="submit" className="g2-button dash-danger" disabled={!confirmed || deleting}>
            {deleting ? "Deleting…" : "Delete repository"}
          </button>
        </div>
      </form>
    </section>
  );
}
