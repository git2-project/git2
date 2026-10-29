import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "./AuthProvider";
import { RequireAuth } from "./RequireAuth";
import { SignInPage } from "./SignInPage";

const user = { id: "u1", email: "ada@git2.dev", username: "ada", created_at: "2026-10-29T00:00:00Z" };

function json(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
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

function renderApp(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <AuthProvider>
        <Routes>
          <Route path="/sign-in" element={<SignInPage plane="Dash" intro={{ eyebrow: "e", headline: "h", highlight: "x", text: "t", features: [] }} otherPlane={{ label: "Admin", href: "/admin/" }} />} />
          <Route element={<RequireAuth />}>
            <Route path="/" element={<p>home page</p>} />
            <Route path="/repos" element={<p>repos page</p>} />
          </Route>
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  );
}

async function fillAndSubmit(email: string, password: string) {
  const u = userEvent.setup();
  await u.type(screen.getByLabelText("Email"), email);
  await u.type(screen.getByLabelText("Password"), password);
  await u.click(screen.getByRole("button", { name: "Sign in" }));
}

describe("sign-in flow", () => {
  it("sends a signed-out visitor to the sign-in page", () => {
    renderApp("/repos");
    expect(screen.getByRole("heading", { name: "Sign in to git2 Dash" })).toBeInTheDocument();
    expect(screen.queryByText(/session has expired/)).not.toBeInTheDocument();
  });

  it("shows a clear message for wrong credentials", async () => {
    fetchMock.mockResolvedValueOnce(json(401, { error: "invalid credentials" }));
    renderApp("/sign-in");

    await fillAndSubmit("ada@git2.dev", "wrong-password");

    expect(await screen.findByRole("alert")).toHaveTextContent("Incorrect email or password.");
  });

  it("shows a disabled-account message on 403", async () => {
    fetchMock.mockResolvedValueOnce(json(403, { error: "account is disabled" }));
    renderApp("/sign-in");

    await fillAndSubmit("ada@git2.dev", "secret123");

    expect(await screen.findByRole("alert")).toHaveTextContent("This account is disabled");
  });

  it("signs in and returns to the page the user asked for", async () => {
    fetchMock.mockResolvedValueOnce(
      json(200, { message: "login successful", user }, {
        "X-Access-Token": "a1",
        "X-Refresh-Token": "r1",
        "X-Access-Expires-In": "900",
      }),
    );
    renderApp("/repos");

    await fillAndSubmit("ada@git2.dev", "secret123");

    expect(await screen.findByText("repos page")).toBeInTheDocument();
  });

  it("restores a stored session without asking to sign in again", async () => {
    localStorage.setItem(
      "git2.tokens",
      JSON.stringify({ access: "a1", refresh: "r1", accessExpiresAt: Date.now() + 60_000 }),
    );
    fetchMock.mockResolvedValueOnce(json(200, user));
    renderApp("/");

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  it("keeps the session and offers a retry when the server is down", async () => {
    localStorage.setItem(
      "git2.tokens",
      JSON.stringify({ access: "a1", refresh: "r1", accessExpiresAt: Date.now() + 60_000 }),
    );
    fetchMock.mockResolvedValueOnce(json(503, { error: "service unavailable" }));
    renderApp("/");

    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(localStorage.getItem("git2.tokens")).not.toBeNull();
  });

  it("explains that the session expired when the server rejects it", async () => {
    localStorage.setItem(
      "git2.tokens",
      JSON.stringify({ access: "a1", refresh: "r1", accessExpiresAt: 0 }),
    );
    fetchMock
      .mockResolvedValueOnce(json(401, { error: "token expired" }))
      .mockResolvedValueOnce(json(401, { error: "invalid or expired refresh token" }));
    renderApp("/repos");

    expect(await screen.findByRole("status")).toHaveTextContent("Your session has expired");
    expect(localStorage.getItem("git2.tokens")).toBeNull();
  });

  it("does not call a sign-out in another tab an expiry", async () => {
    localStorage.setItem(
      "git2.tokens",
      JSON.stringify({ access: "a1", refresh: "r1", accessExpiresAt: Date.now() + 60_000 }),
    );
    fetchMock.mockResolvedValueOnce(json(200, user));
    renderApp("/");
    expect(await screen.findByText("home page")).toBeInTheDocument();

    act(() => {
      localStorage.removeItem("git2.tokens");
      window.dispatchEvent(new StorageEvent("storage", { key: "git2.tokens", oldValue: "{}", newValue: null }));
    });

    expect(await screen.findByRole("heading", { name: "Sign in to git2 Dash" })).toBeInTheDocument();
    expect(screen.queryByText(/session has expired/)).not.toBeInTheDocument();
  });

  it("shows and hides the password", async () => {
    renderApp("/sign-in");
    const u = userEvent.setup();
    const input = screen.getByLabelText("Password");
    expect(input).toHaveAttribute("type", "password");

    await u.click(screen.getByRole("button", { name: "Show password" }));
    expect(input).toHaveAttribute("type", "text");

    await u.click(screen.getByRole("button", { name: "Hide password" }));
    expect(input).toHaveAttribute("type", "password");
  });

  it("links to the other plane from the top bar", () => {
    renderApp("/sign-in");
    expect(screen.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/admin/");
  });

  it("switches between light and dark and remembers the choice", async () => {
    document.documentElement.removeAttribute("data-theme");
    renderApp("/sign-in");
    const u = userEvent.setup();

    // jsdom has no matchMedia, so the starting theme is light.
    await u.click(screen.getByRole("button", { name: "Switch to dark theme" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(localStorage.getItem("git2.theme")).toBe("dark");

    await u.click(screen.getByRole("button", { name: "Switch to light theme" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem("git2.theme")).toBe("light");
  });
});
