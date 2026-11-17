import type { ApiError } from "@git2/api";
import type { ReactNode } from "react";
import type { ApiState } from "../lib/useApi";

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <p className="g2-muted dash-loading" role="status">
      {label}
    </p>
  );
}

// ErrorView explains an API error. A repository the caller cannot read is a
// 404 too, so "not found" never says whether it exists.
export function ErrorView({ error, notFound }: { error: ApiError; notFound?: string }) {
  let message = error.message;
  if (error.status === 404) message = notFound ?? "Not found.";
  else if (error.status === 0) message = "git2 could not be reached. Check your connection and try again.";
  else if (error.status >= 500) message = "Something went wrong on the server. Try again in a moment.";
  return (
    <p className="g2-error" role="alert">
      {message}
    </p>
  );
}

// Await renders children once state has data, and a loading line or the
// error before that.
export function Await<T>({
  state,
  notFound,
  children,
}: {
  state: ApiState<T>;
  notFound?: string;
  children: (data: T) => ReactNode;
}) {
  if (state.status === "loading") return <Loading />;
  if (state.status === "error") return <ErrorView error={state.error} notFound={notFound} />;
  return <>{children(state.data)}</>;
}
