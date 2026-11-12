import type { Blob, Branch, Commit, CommitDetail, Repository, Tree } from "@git2/api";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BlobPage } from "./pages/BlobPage";
import { CommitPage } from "./pages/CommitPage";
import { CommitsPage } from "./pages/CommitsPage";
import { RepoListPage } from "./pages/RepoListPage";
import { TreePage } from "./pages/TreePage";
import { RepoLayout } from "./repo/RepoLayout";

// Shiki loads grammars and WebAssembly; the tests check Dash's own output.
vi.mock("./lib/highlight", async (orig) => ({
  ...(await orig<typeof import("./lib/highlight")>()),
  highlight: () => Promise.resolve(null),
}));

vi.mock("@git2/ui", () => ({ useAuth: () => ({ user: { username: "ada" } }) }));

// ---------------------------------------------------------------- fixtures

const sha = (c: string) => c.repeat(40);
const when = "2026-11-07T10:00:00Z";
const sig = { name: "Ada Lovelace", email: "ada@git2.dev", date: when, user: null };

const repository: Repository = {
  id: "r1",
  owner: { id: "u1", name: "ada", kind: "user" },
  name: "billing",
  full_name: "ada/billing",
  description: "Invoices and payments",
  default_branch: "main",
  clone_url: "https://git2.example.com/ada/billing.git",
  created_at: when,
  updated_at: when,
};

const commit = (c: string, title: string): Commit => ({
  sha: sha(c),
  short_sha: sha(c).slice(0, 7),
  title,
  message: `${title}\n\nLonger explanation.`,
  author: sig,
  committer: sig,
  parent_shas: [sha("0")],
});

const rootTree: Tree = {
  ref: "main",
  commit_sha: sha("a"),
  path: "",
  empty: false,
  truncated: false,
  last_commit: { sha: sha("a"), title: "Add invoices", committed_at: when },
  entries: [
    { name: "src", path: "src", type: "dir", mode: "040000", sha: sha("1") },
    { name: "README.md", path: "README.md", type: "file", mode: "100644", sha: sha("2"), size: 1536 },
    { name: "vendor", path: "vendor", type: "submodule", mode: "160000", sha: sha("3") },
  ],
};

const textBlob: Blob = {
  ref: "main",
  commit_sha: sha("a"),
  path: "README.md",
  name: "README.md",
  size: 24,
  binary: false,
  lfs: false,
  truncated: false,
  content: "# Billing\n\nHello world\n",
  language: "markdown",
};

const branches: Branch[] = [
  { name: "main", commit: rootTree.last_commit!, default: true, protected: false },
  { name: "feature/login", commit: rootTree.last_commit!, default: false, protected: false },
];

// ------------------------------------------------------------ fake dash-api

type Handler = (url: URL) => Response | undefined;
let handlers: Handler[] = [];
const fetchMock = vi.fn<typeof fetch>();

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// on answers GET path (exact match) with body, or with what body(url) returns.
function on(path: string, body: object | ((url: URL) => Response)) {
  handlers.push((url) => {
    if (url.pathname !== path) return undefined;
    return typeof body === "function" ? body(url) : json(body);
  });
}

function requested(path: string): URL[] {
  return fetchMock.mock.calls
    .map(([input]) => new URL(String(input), "http://localhost"))
    .filter((u) => u.pathname === path);
}

beforeEach(() => {
  handlers = [];
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (input) => {
    const url = new URL(String(input), "http://localhost");
    for (const h of handlers) {
      const res = h(url);
      if (res) return res;
    }
    return json({ error: "not found" }, 404);
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.setItem(
    "git2.tokens",
    JSON.stringify({ access: "a1", refresh: "r1", accessExpiresAt: Date.now() + 60_000 }),
  );
  on("/api/repos/ada/billing", repository);
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Routes>
        <Route path="/" element={<RepoListPage />} />
        <Route path=":owner/:repo" element={<RepoLayout />}>
          <Route index element={<TreePage />} />
          <Route path="tree/:ref/*" element={<TreePage />} />
          <Route path="blob/:ref/*" element={<BlobPage />} />
          <Route path="commits/:ref" element={<CommitsPage />} />
          <Route path="commit/:sha" element={<CommitPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

// ------------------------------------------------------------------- tests

describe("repository list", () => {
  it("lists repositories and loads the next page", async () => {
    on("/api/repos", (url) =>
      url.searchParams.get("page_token") === "p2"
        ? json({ repositories: [{ ...repository, id: "r2", name: "web", full_name: "ada/web" }] })
        : json({ repositories: [repository], next_page_token: "p2" }),
    );
    renderAt("/");

    const link = await screen.findByRole("link", { name: "ada/billing" });
    expect(link).toHaveAttribute("href", "/ada/billing");
    expect(screen.getByText("Invoices and payments")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: "ada/web" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "ada/billing" })).toBeInTheDocument();
  });

  it("explains an empty list", async () => {
    on("/api/repos", { repositories: [] });
    renderAt("/");
    expect(await screen.findByText("You have no repositories yet.")).toBeInTheDocument();
  });
});

describe("repository home and tree", () => {
  it("shows the default branch's root, the last commit and the README", async () => {
    on("/api/repos/ada/billing/tree", rootTree);
    on("/api/repos/ada/billing/blob", textBlob);
    renderAt("/ada/billing");

    const src = await screen.findByRole("link", { name: "src" });
    expect(src).toHaveAttribute("href", "/ada/billing/tree/main/src");
    expect(screen.getAllByRole("link", { name: "README.md" })[0]).toHaveAttribute(
      "href",
      "/ada/billing/blob/main/README.md",
    );
    expect(screen.getByText("1.5 KiB")).toBeInTheDocument();
    expect(screen.getByText("vendor")).toBeInTheDocument(); // a submodule is not a link
    expect(screen.queryByRole("link", { name: "vendor" })).not.toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: "Add invoices" })[0]).toHaveAttribute(
      "href",
      `/ada/billing/commit/${sha("a")}`,
    );
    expect(await screen.findByText("Hello world")).toBeInTheDocument();
    expect(requested("/api/repos/ada/billing/tree")[0].searchParams.get("ref")).toBe("main");
  });

  it("reads a ref with a slash and a nested path from the URL", async () => {
    on("/api/repos/ada/billing/tree", { ...rootTree, ref: "feature/login", path: "src/api", entries: [] });
    renderAt("/ada/billing/tree/feature%2Flogin/src/api");

    expect(await screen.findByRole("link", { name: "src" })).toHaveAttribute(
      "href",
      "/ada/billing/tree/feature%2Flogin/src",
    );
    const q = requested("/api/repos/ada/billing/tree")[0].searchParams;
    expect([q.get("ref"), q.get("path")]).toEqual(["feature/login", "src/api"]);
    expect(screen.getByRole("link", { name: "Parent directory" })).toHaveAttribute(
      "href",
      "/ada/billing/tree/feature%2Flogin/src",
    );
  });

  it("shows how to push the first commit to an empty repository", async () => {
    on("/api/repos/ada/billing/tree", { ...rootTree, empty: true, entries: [], commit_sha: "", last_commit: null });
    renderAt("/ada/billing");

    expect(await screen.findByRole("heading", { name: "This repository is empty" })).toBeInTheDocument();
    expect(screen.getByText(/git push -u origin main/)).toBeInTheDocument();
  });

  it("offers the other branches when the default branch has no commits", async () => {
    on("/api/repos/ada/billing/branches", { branches: [branches[1]] });
    renderAt("/ada/billing");

    expect(await screen.findByRole("link", { name: "feature/login" })).toHaveAttribute(
      "href",
      "/ada/billing/tree/feature%2Flogin",
    );
  });

  it("does not tell whether a repository the caller cannot read exists", async () => {
    renderAt("/ada/secret");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This repository does not exist, or you do not have access to it.",
    );
  });
});

describe("ref picker", () => {
  it("lists branches with the default marked and keeps the directory", async () => {
    on("/api/repos/ada/billing/tree", { ...rootTree, path: "src" });
    on("/api/repos/ada/billing/branches", { branches });
    renderAt("/ada/billing/tree/main/src");

    await userEvent.click(await screen.findByRole("button", { name: /ref:\s*main/ }));
    const panel = await screen.findByRole("dialog", { name: "Switch branch or tag" });
    const feature = await within(panel).findByRole("link", { name: "feature/login" });
    expect(feature).toHaveAttribute("href", "/ada/billing/tree/feature%2Flogin/src");
    expect(within(panel).getByText("default")).toBeInTheDocument();
  });
});

describe("file view", () => {
  it("shows a text file with its size and line count", async () => {
    on("/api/repos/ada/billing/blob", textBlob);
    on("/api/repos/ada/billing/branches", { branches });
    renderAt("/ada/billing/blob/main/README.md");

    expect(await screen.findByText("Hello world")).toBeInTheDocument();
    expect(screen.getByText(/3 lines · 24 B/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Permalink" })).toHaveAttribute(
      "href",
      `/ada/billing/blob/${sha("a")}/README.md`,
    );
  });

  it("offers a download for a binary file and explains a file over 10 MiB", async () => {
    on("/api/repos/ada/billing/blob", { ...textBlob, name: "logo.png", path: "logo.png", binary: true, content: null });
    on("/api/repos/ada/billing/raw", () => json({ error: "file is larger than 10 MiB; use git clone to get it" }, 413));
    renderAt("/ada/billing/blob/main/logo.png");

    expect(await screen.findByText("Binary file not shown.")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "Download" });
    await userEvent.click(buttons[buttons.length - 1]);
    expect(await screen.findByText("Larger than 10 MiB: use git clone to get it.")).toBeInTheDocument();
  });

  it("warns that a large file was cut", async () => {
    on("/api/repos/ada/billing/blob", { ...textBlob, truncated: true, size: 5 * 1024 * 1024 });
    renderAt("/ada/billing/blob/main/README.md");

    expect(await screen.findByText(/only the first 1 MiB is shown/)).toBeInTheDocument();
  });
});

describe("commits", () => {
  it("lists the history of a path and pages to older commits", async () => {
    on("/api/repos/ada/billing/commits", (url) =>
      url.searchParams.get("page_token") === "c2"
        ? json({ commits: [commit("c", "Initial commit")] })
        : json({ commits: [commit("b", "Fix totals")], next_page_token: "c2" }),
    );
    renderAt("/ada/billing/commits/main?path=src%2Finvoice.go");

    expect(await screen.findByRole("link", { name: "Fix totals" })).toHaveAttribute(
      "href",
      `/ada/billing/commit/${sha("b")}`,
    );
    expect(requested("/api/repos/ada/billing/commits")[0].searchParams.get("path")).toBe("src/invoice.go");
    expect(screen.getByText(/History of/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Older commits" }));
    expect(await screen.findByRole("link", { name: "Initial commit" })).toBeInTheDocument();
    expect(requested("/api/repos/ada/billing/commits")[1].searchParams.get("path")).toBe("src/invoice.go");
  });

  it("shows a commit with its stats and each kind of file diff", async () => {
    const detail: CommitDetail = {
      ...commit("b", "Fix totals"),
      stats: { files_changed: 4, additions: 1, deletions: 1 },
      truncated: false,
      files: [
        {
          status: "modified",
          old_path: "src/invoice.go",
          new_path: "src/invoice.go",
          additions: 1,
          deletions: 1,
          binary: false,
          truncated: false,
          patch: "@@ -1,2 +1,2 @@\n package billing\n-var total = 0\n+var total = 1\n",
        },
        {
          status: "renamed",
          old_path: "a.txt",
          new_path: "b.txt",
          additions: 0,
          deletions: 0,
          binary: false,
          truncated: false,
          patch: "",
        },
        {
          status: "added",
          old_path: null,
          new_path: "big.sql",
          additions: 9000,
          deletions: 0,
          binary: false,
          truncated: true,
          patch: null,
        },
        {
          status: "deleted",
          old_path: "logo.png",
          new_path: null,
          additions: 0,
          deletions: 0,
          binary: true,
          truncated: false,
          patch: null,
        },
      ],
    };
    on(`/api/repos/ada/billing/commits/${sha("b")}`, detail);
    renderAt(`/ada/billing/commit/${sha("b")}`);

    expect(await screen.findByRole("heading", { name: "Fix totals" })).toBeInTheDocument();
    expect(screen.getByText("Longer explanation.")).toBeInTheDocument();
    expect(screen.getByText("4 files changed")).toBeInTheDocument();
    expect(screen.getByText("var total = 0")).toBeInTheDocument();
    expect(screen.getByText("var total = 1")).toBeInTheDocument();
    expect(screen.getByText("File renamed without changes.")).toBeInTheDocument();
    expect(screen.getByText(/This diff is too large to show/)).toBeInTheDocument();
    expect(screen.getByText("Binary file not shown.")).toBeInTheDocument();
    expect(screen.getByText("a.txt → b.txt")).toBeInTheDocument();
    // A deleted file has nothing to view at this commit.
    expect(screen.getAllByRole("link", { name: "View file" })).toHaveLength(3);
  });

  it("warns when more than 300 files changed", async () => {
    on(`/api/repos/ada/billing/commits/${sha("b")}`, {
      ...commit("b", "Big change"),
      stats: { files_changed: 500, additions: 0, deletions: 0 },
      files: [],
      truncated: true,
    });
    renderAt(`/ada/billing/commit/${sha("b")}`);

    expect(await screen.findByText(/only the first 300 are shown/)).toBeInTheDocument();
  });
});
