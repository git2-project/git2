import { ApiError } from "@git2/api";

// authErrorMessage turns a failed sign-in or registration into text for the form.
export function authErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return "Incorrect email or password.";
      case 403:
        return "This account is disabled. Contact your administrator.";
      case 400:
      case 409:
        return capitalize(err.message);
      case 429:
        return "Too many attempts. Wait a minute and try again.";
    }
  }
  return "git2 is unavailable right now. Try again in a moment.";
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1) + (s.endsWith(".") ? "" : ".");
}
