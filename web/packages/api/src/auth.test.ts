import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, REFRESH_LOCK_NAME, login, me, onSessionEnd } from "./index";
import { loadTokens, saveTokens } from "./tokens";

const user = { id: "u1", email: "a@b.co", username: "ada", created_at: "2026-10-29T00:00:00Z" };

function json(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

function tokenHeaders(access: string, refresh: string) {
  return { "X-Access-Token": access, "X-Refresh-Token": refresh, "X-Access-Expires-In": "900" };
}

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("login", () => {
  it("stores the token pair from the response headers", async () => {
    fetchMock.mockResolvedValueOnce(json(200, { message: "ok", user }, tokenHeaders("a1", "r1")));

    await expect(login("a@b.co", "secret123")).resolves.toEqual(user);
    expect(loadTokens()).toMatchObject({ access: "a1", refresh: "r1" });
  });

  it("throws the server message on 401 and stores nothing", async () => {
    fetchMock.mockResolvedValueOnce(json(401, { error: "invalid credentials" }));

    await expect(login("a@b.co", "wrong")).rejects.toMatchObject({
      status: 401,
      message: "invalid credentials",
    });
    expect(loadTokens()).toBeNull();
  });
});

describe("authenticated requests", () => {
  it("refreshes once on 401 and retries with the new access token", async () => {
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    fetchMock
      .mockResolvedValueOnce(json(401, { error: "token expired" }))
      .mockResolvedValueOnce(json(200, { message: "token refreshed" }, tokenHeaders("new", "r2")))
      .mockResolvedValueOnce(json(200, user));

    await expect(me()).resolves.toEqual(user);

    const [, refreshInit] = fetchMock.mock.calls[1];
    expect(JSON.parse(refreshInit?.body as string)).toEqual({ refresh_token: "r1" });
    const [, retryInit] = fetchMock.mock.calls[2];
    expect((retryInit?.headers as Record<string, string>).Authorization).toBe("Bearer new");
    expect(loadTokens()).toMatchObject({ access: "new", refresh: "r2" });
  });

  it("ends the session when the refresh token is rejected", async () => {
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    const ended = vi.fn();
    const off = onSessionEnd(ended);
    fetchMock
      .mockResolvedValueOnce(json(401, { error: "token expired" }))
      .mockResolvedValueOnce(json(401, { error: "invalid or expired refresh token" }));

    await expect(me()).rejects.toMatchObject({ status: 401 });
    expect(loadTokens()).toBeNull();
    expect(ended).toHaveBeenCalledOnce();
    off();
  });

  it("keeps the tokens when the server fails with 5xx", async () => {
    saveTokens({ access: "a1", refresh: "r1", accessExpiresAt: 0 });
    fetchMock.mockResolvedValueOnce(json(503, { error: "could not sign in, try again" }));

    await expect(me()).rejects.toBeInstanceOf(ApiError);
    expect(loadTokens()).toMatchObject({ access: "a1" });
  });

  it("shares one refresh between concurrent requests", async () => {
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    fetchMock.mockImplementation(async (input, init) => {
      const path = String(input);
      if (path === "/api/auth/refresh") return json(200, {}, tokenHeaders("new", "r2"));
      const auth = (init?.headers as Record<string, string>).Authorization;
      return auth === "Bearer new" ? json(200, user) : json(401, { error: "token expired" });
    });

    await Promise.all([me(), me()]);

    const refreshes = fetchMock.mock.calls.filter(([p]) => p === "/api/auth/refresh");
    expect(refreshes).toHaveLength(1);
  });

  it("reports an unreachable server as status 0", async () => {
    saveTokens({ access: "a1", refresh: "r1", accessExpiresAt: 0 });
    fetchMock.mockRejectedValueOnce(new TypeError("Failed to fetch"));

    await expect(me()).rejects.toMatchObject({ status: 0 });
    expect(loadTokens()).not.toBeNull();
  });
});

describe("refresh across tabs", () => {
  // A Web Locks stand-in: runs the callback after `beforeGrant`, which plays
  // whatever another tab did while this one waited for the lock.
  function stubLocks(beforeGrant: () => void = () => {}) {
    const request = vi.fn(async (_name: string, cb: () => Promise<unknown>) => {
      beforeGrant();
      return cb();
    });
    vi.stubGlobal("navigator", { ...navigator, locks: { request } });
    return request;
  }

  it("refreshes inside the shared lock", async () => {
    const request = stubLocks();
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    fetchMock
      .mockResolvedValueOnce(json(401, { error: "token expired" }))
      .mockResolvedValueOnce(json(200, {}, tokenHeaders("new", "r2")))
      .mockResolvedValueOnce(json(200, user));

    await expect(me()).resolves.toEqual(user);
    expect(request).toHaveBeenCalledOnce();
    expect(request.mock.calls[0][0]).toBe(REFRESH_LOCK_NAME);
  });

  it("reuses the pair another tab stored instead of replaying the old token", async () => {
    stubLocks(() => saveTokens({ access: "from-other-tab", refresh: "r2", accessExpiresAt: 0 }));
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    fetchMock
      .mockResolvedValueOnce(json(401, { error: "token expired" }))
      .mockResolvedValueOnce(json(200, user));

    await expect(me()).resolves.toEqual(user);

    expect(fetchMock.mock.calls.map(([p]) => p)).not.toContain("/api/auth/refresh");
    const [, retryInit] = fetchMock.mock.calls[1];
    expect((retryInit?.headers as Record<string, string>).Authorization).toBe("Bearer from-other-tab");
  });

  it("ends the session when another tab signed out while this one waited", async () => {
    stubLocks(() => localStorage.removeItem("git2.tokens"));
    saveTokens({ access: "old", refresh: "r1", accessExpiresAt: 0 });
    fetchMock.mockResolvedValueOnce(json(401, { error: "token expired" }));

    await expect(me()).rejects.toMatchObject({ status: 401 });
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});
