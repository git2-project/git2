import { createRepository } from "@git2/api";
import { useAuth } from "@git2/ui";
import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { formError } from "../lib/errors";
import { repoHome } from "../lib/paths";

// NewRepositoryPage creates an empty repository under the caller's name and
// opens it, where the empty-repository screen shows how to push (FR-25).
// git-svc checks the name; its message is shown as it comes.
export function NewRepositoryPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    try {
      const repo = await createRepository({
        name: name.trim(),
        description: description.trim() || undefined,
      });
      navigate(repoHome({ owner: repo.owner.name, repo: repo.name }));
    } catch (err) {
      setError(formError(err));
      setSubmitting(false);
    }
  }

  return (
    <section className="dash-page dash-narrow">
      <header className="dash-page-header">
        <h1>New repository</h1>
        <p className="g2-muted">It starts empty; the next page shows how to push your code.</p>
      </header>

      <form className="dash-form" onSubmit={onSubmit} noValidate>
        {error && (
          <p className="g2-error" role="alert">
            {error}
          </p>
        )}
        <div className="dash-field">
          <label htmlFor="repo-name">Name</label>
          <div className="dash-name-input">
            {user && <span className="g2-muted">{user.username} /</span>}
            <input
              id="repo-name"
              aria-describedby="repo-name-hint"
              autoComplete="off"
              spellCheck={false}
              maxLength={100}
              required
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <p id="repo-name-hint" className="g2-muted dash-hint">
            Up to 100 letters, digits, <code>.</code>, <code>_</code> and <code>-</code>.
          </p>
        </div>
        <div className="dash-field">
          <label htmlFor="repo-description">
            Description <span className="g2-muted">(optional)</span>
          </label>
          <input
            id="repo-description"
            maxLength={2000}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div className="dash-form-actions">
          <button type="submit" className="g2-button g2-primary" disabled={submitting || !name.trim()}>
            {submitting ? "Creating…" : "Create repository"}
          </button>
          <Link to="/" className="g2-button">
            Cancel
          </Link>
        </div>
      </form>
    </section>
  );
}
