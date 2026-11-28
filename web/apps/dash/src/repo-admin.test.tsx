import type { Repository } from "@git2/api";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { NewRepositoryPage } from "./pages/NewRepositoryPage";
import { RepoSettingsPage } from "./pages/RepoSettingsPage";
import { RepoLayout } from "./repo/RepoLayout";

vi.mock("@git2/ui", () => ({ useAuth: () => ({ user: { username: "ada" } }) }));

const when = "2026-11-27T10:00:00Z";
const repository: Repository = {
  id: "r1",
  owner: { id: "u1", name: "ada", kind: "user" },
  name: "billing",
  full_name: "ada/billing",
  description: "",
  default_branch: "main",
  clone_url: "https://git2.example.com/ada/billing.git",
  created_at: when,
  updated_at: when,
};

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

// answer decides the response to each request by method and path.
let answer: (method: string, path: string, body: unknown) => Response;
const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (input, init) => {
    const url = new URL(String(input), "http://localhost");
    const body = init?.body ? JSON.parse(String(init.body)) : undefined;
    return answer(init?.method ?? "GET", url.pathname, body);
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.setItem(
    "git2.tokens",
    JSON.stringify({
      access: "a1",
      refresh: "r1",
      accessExpiresAt: Date.now() + 60_000,
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

function calls(method: string, path: string) {
  return fetchMock.mock.calls.filter(
    ([input, init]) =>
      (init?.method ?? "GET") === method && new URL(String(input), "http://localhost").pathname === path,
  );
}

function Where() {
  return <p data-testid="where">{useLocation().pathname}</p>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Routes>
        <Route path="/" element={<Where />} />
        <Route path="new" element={<NewRepositoryPage />} />
        <Route path=":owner/:repo" element={<RepoLayout />}>
          <Route index element={<Where />} />
          <Route path="settings" element={<RepoSettingsPage />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("new repository", () => {
  it("creates the repository and opens it", async () => {
    answer = (method, path) => {
      if (method === "POST" && path === "/api/repos") return json({ ...repository, name: "web-shop" }, 201);
      if (path === "/api/repos/ada/web-shop") return json({ ...repository, name: "web-shop" });
      return json({ error: "not found" }, 404);
    };
    renderAt("/new");

    const create = screen.getByRole("button", { name: "Create repository" });
    expect(create).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Name"), " web-shop ");
    await userEvent.type(screen.getByLabelText(/Description/), "Storefront");
    await userEvent.click(create);

    expect(await screen.findByTestId("where")).toHaveTextContent("/ada/web-shop");
    const [[, init]] = calls("POST", "/api/repos");
    expect(JSON.parse(String(init?.body))).toEqual({
      name: "web-shop",
      description: "Storefront",
    });
  });

  it("leaves an empty description out", async () => {
    answer = () => json(repository, 201);
    renderAt("/new");
    await userEvent.type(screen.getByLabelText("Name"), "billing");
    await userEvent.click(screen.getByRole("button", { name: "Create repository" }));

    await screen.findByTestId("where");
    const [[, init]] = calls("POST", "/api/repos");
    expect(JSON.parse(String(init?.body))).toEqual({ name: "billing" });
  });

  it("shows git-svc's message for a taken or invalid name", async () => {
    answer = (_, __, body) =>
      (body as { name: string }).name === "billing"
        ? json({ error: "repository billing already exists" }, 409)
        : json({ error: 'name must not end with ".git"' }, 400);
    renderAt("/new");
    const name = screen.getByLabelText("Name");

    await userEvent.type(name, "billing");
    await userEvent.click(screen.getByRole("button", { name: "Create repository" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Repository billing already exists.");

    await userEvent.clear(name);
    await userEvent.type(name, "app.git");
    await userEvent.click(screen.getByRole("button", { name: "Create repository" }));
    expect(await screen.findByRole("alert")).toHaveTextContent('Name must not end with ".git".');
    // The form stays, so the name can be fixed.
    expect(screen.getByRole("button", { name: "Create repository" })).toBeEnabled();
  });

  it("does not show a server fault's text", async () => {
    answer = () => json({ error: "git service unavailable" }, 503);
    renderAt("/new");
    await userEvent.type(screen.getByLabelText("Name"), "billing");
    await userEvent.click(screen.getByRole("button", { name: "Create repository" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Something went wrong on the server.");
  });
});

describe("repository settings", () => {
  it("deletes only after the name is typed back", async () => {
    answer = (method, path) => {
      if (path !== "/api/repos/ada/billing") return json({ error: "not found" }, 404);
      return method === "DELETE" ? new Response(null, { status: 204 }) : json(repository);
    };
    renderAt("/ada/billing/settings");

    const del = await screen.findByRole("button", {
      name: "Delete repository",
    });
    expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "page");
    expect(del).toBeDisabled();

    const confirm = screen.getByLabelText(/to confirm/);
    await userEvent.type(confirm, "Billing");
    expect(del).toBeDisabled();
    await userEvent.clear(confirm);
    await userEvent.type(confirm, "billing");
    expect(del).toBeEnabled();
    await userEvent.click(del);

    expect(await screen.findByTestId("where")).toHaveTextContent(/^\/$/);
    expect(calls("DELETE", "/api/repos/ada/billing")).toHaveLength(1);
  });

  it("explains a refusal and stays on the page", async () => {
    answer = (method) =>
      method === "DELETE" ? json({ error: "maintain access to the repository is required" }, 403) : json(repository);
    renderAt("/ada/billing/settings");

    await userEvent.type(await screen.findByLabelText(/to confirm/), "billing");
    await userEvent.click(screen.getByRole("button", { name: "Delete repository" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Maintain access to the repository is required.");
    expect(screen.getByRole("button", { name: "Delete repository" })).toBeEnabled();
  });
});
