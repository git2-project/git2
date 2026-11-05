// ApiError is any non-2xx answer from the git2 API. status 0 means the request
// never got an answer (network down, gateway unreachable).
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export async function errorFrom(res: Response): Promise<ApiError> {
  let message = res.statusText || `HTTP ${res.status}`;
  try {
    const body = (await res.json()) as { error?: unknown };
    if (typeof body.error === "string") message = body.error;
  } catch {
    // Not a JSON error body; keep the status text.
  }
  return new ApiError(res.status, message);
}
