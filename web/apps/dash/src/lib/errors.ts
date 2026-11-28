import { ApiError } from "@git2/api";

// formError turns a failed create or delete into text for a form. 400, 403
// and 409 carry a message meant for people (from git-svc or dash-api); a
// fault gets a generic line.
export function formError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 400 || err.status === 403 || err.status === 409) return sentence(err.message);
    if (err.status === 404) return "This repository does not exist, or you do not have access to it.";
    if (err.status === 0) return "git2 could not be reached. Check your connection and try again.";
  }
  return "Something went wrong on the server. Try again in a moment.";
}

function sentence(s: string): string {
  const t = s.trim();
  if (!t) return "The request was refused.";
  return t.charAt(0).toUpperCase() + t.slice(1) + (/[.!?]$/.test(t) ? "" : ".");
}
