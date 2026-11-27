import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { AppShell } from "./AppShell";

vi.mock("./AuthProvider", () => ({ useAuth: () => ({ user: { username: "ada" }, signOut: vi.fn() }) }));

describe("AppShell", () => {
  it("links the brand to the plane's home", () => {
    render(
      <MemoryRouter initialEntries={["/ada/billing"]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <AppShell plane="Dash">
          <p>page</p>
        </AppShell>
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "git2 Dash" })).toHaveAttribute("href", "/");
  });

  it("keeps the router's basename, so Admin's brand goes to /admin", () => {
    render(
      <MemoryRouter
        basename="/admin"
        initialEntries={["/admin/users"]}
        future={{ v7_startTransition: true, v7_relativeSplatPath: true }}
      >
        <AppShell plane="Admin">
          <p>page</p>
        </AppShell>
      </MemoryRouter>,
    );
    expect(screen.getByRole("link", { name: "git2 Admin" })).toHaveAttribute("href", "/admin");
  });
});
