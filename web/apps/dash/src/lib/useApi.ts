import { ApiError } from "@git2/api";
import { useEffect, useState } from "react";

export type ApiState<T> =
  | { status: "loading"; data?: undefined; error?: undefined }
  | { status: "ok"; data: T; error?: undefined }
  | { status: "error"; data?: undefined; error: ApiError };

// useApi runs load whenever deps change and aborts the previous call, so a
// slow answer for a page the user already left never replaces the current one.
export function useApi<T>(load: (signal: AbortSignal) => Promise<T>, deps: readonly unknown[]): ApiState<T> {
  const [state, setState] = useState<ApiState<T>>({ status: "loading" });

  useEffect(() => {
    const ctrl = new AbortController();
    setState({ status: "loading" });
    load(ctrl.signal).then(
      (data) => {
        if (!ctrl.signal.aborted) setState({ status: "ok", data });
      },
      (err: unknown) => {
        if (ctrl.signal.aborted) return;
        setState({ status: "error", error: err instanceof ApiError ? err : new ApiError(0, String(err)) });
      },
    );
    return () => ctrl.abort();
    // The caller lists what load depends on, as with useEffect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return state;
}
