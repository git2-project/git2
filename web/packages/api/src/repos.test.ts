import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getBlob, getCommit, getRaw, getTree, listBranches, listCommits, listRepositories } from "./index";
import { saveTokens } from "./tokens";

const fetchMock = vi.fn<typeof fetch>();
const repo = { owner: "ada", repo: "billing-service" };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// calledURL is the path and query of the nth fetch call.
function calledURL(n = 0): URL {
  return new URL(String(fetchMock.mock.calls[n][0]), "http://localhost");
}

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  saveTokens({ access: "a1", refresh: "r1", accessExpiresAt: Date.now() + 60_000 });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("repository client", () => {
  it("sends the bearer token and leaves empty options out of the query", async () => {
    fetchMock.mockResolvedValueOnce(json({ repositories: [] }));

    await listRepositories();

    expect(calledURL().pathname).toBe("/api/repos");
    expect(calledURL().search).toBe("");
    const init = fetchMock.mock.calls[0][1];
    expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer a1");
  });

  it("puts ref and path in the query, so slashes in them survive", async () => {
    fetchMock.mockResolvedValueOnce(json({ entries: [] }));

    await getTree(repo, "docs/api", { ref: "feature/login" });

    const u = calledURL();
    expect(u.pathname).toBe("/api/repos/ada/billing-service/tree");
    expect(u.searchParams.get("ref")).toBe("feature/login");
    expect(u.searchParams.get("path")).toBe("docs/api");
  });

  it("omits path for the root directory", async () => {
    fetchMock.mockResolvedValueOnce(json({ entries: [] }));

    await getTree(repo, "");

    expect(calledURL().searchParams.has("path")).toBe(false);
  });

  it("maps paging and the branch query to the contract's names", async () => {
    fetchMock.mockResolvedValueOnce(json({ branches: [] }));

    await listBranches(repo, { query: "feat", pageSize: 20, pageToken: "t1" });

    const q = calledURL().searchParams;
    expect([q.get("q"), q.get("page_size"), q.get("page_token")]).toEqual(["feat", "20", "t1"]);
  });

  it("passes path and ref to the commit history", async () => {
    fetchMock.mockResolvedValueOnce(json({ commits: [] }));

    await listCommits(repo, { ref: "main", path: "README.md" });

    const q = calledURL().searchParams;
    expect([q.get("ref"), q.get("path")]).toEqual(["main", "README.md"]);
  });

  it("returns the commit detail", async () => {
    const detail = { sha: "4bdc553", files: [], stats: { additions: 1, deletions: 0, files_changed: 1 } };
    fetchMock.mockResolvedValueOnce(json(detail));

    await expect(getCommit(repo, "4bdc553")).resolves.toEqual(detail);
    expect(calledURL().pathname).toBe("/api/repos/ada/billing-service/commits/4bdc553");
  });

  it("throws the server's message as an ApiError", async () => {
    fetchMock.mockResolvedValueOnce(json({ error: "path is a directory" }, 400));

    await expect(getBlob(repo, "src")).rejects.toMatchObject({ status: 400, message: "path is a directory" });
  });

  it("lets an abort through instead of reporting an outage", async () => {
    const ctrl = new AbortController();
    fetchMock.mockImplementationOnce(() => {
      ctrl.abort();
      return Promise.reject(new DOMException("aborted", "AbortError"));
    });

    await expect(getTree(repo, "", { signal: ctrl.signal })).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("getRaw", () => {
  it("returns the bytes with the name from Content-Disposition", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(new Uint8Array([1, 2, 3]), {
        headers: { "Content-Disposition": 'attachment; filename="logo.png"' },
      }),
    );

    const file = await getRaw(repo, "assets/logo.png");

    expect(file.name).toBe("logo.png");
    expect(file.data.size).toBe(3);
  });

  it("falls back to the last part of the path", async () => {
    fetchMock.mockResolvedValueOnce(new Response("x"));

    await expect(getRaw(repo, "a/b/notes.txt")).resolves.toMatchObject({ name: "notes.txt" });
  });

  it("reports a file over 10 MiB as 413", async () => {
    fetchMock.mockResolvedValueOnce(json({ error: "file is larger than 10 MiB; use git clone to get it" }, 413));

    await expect(getRaw(repo, "big.iso")).rejects.toMatchObject({ status: 413 });
  });
});
