import { listBranches, listTags, type RepoName } from "@git2/api";
import { useEffect, useRef, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useApi } from "../lib/useApi";
import { Loading } from "./Status";

interface RefPickerProps {
  name: RepoName;
  current: string;
  // to builds the link for a ref, so the picker keeps the current page
  // (the same directory, file or history) on the other ref.
  to: (ref: string) => string;
}

const isSha = (ref: string) => /^[0-9a-f]{40}$/.test(ref);

// RefPicker switches between branches and tags. Branches are filtered on the
// server (the contract's q), tags in the loaded page.
export function RefPicker({ name, current, to }: RefPickerProps) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<"branches" | "tags">("branches");
  const [query, setQuery] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const location = useLocation();

  // Close after following a link and when clicking outside.
  useEffect(() => setOpen(false), [location.pathname]);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="dash-refpicker" ref={rootRef}>
      <button
        type="button"
        className="g2-button dash-refpicker-button"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="g2-muted">{isSha(current) ? "commit" : "ref"}:</span>{" "}
        <strong>{isSha(current) ? current.slice(0, 7) : current}</strong> ▾
      </button>
      {open && (
        <div className="dash-refpicker-panel" role="dialog" aria-label="Switch branch or tag">
          <input
            className="dash-refpicker-search"
            type="search"
            placeholder={tab === "branches" ? "Find a branch…" : "Filter tags…"}
            aria-label={tab === "branches" ? "Find a branch" : "Filter tags"}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
          />
          <div className="dash-refpicker-tabs" role="tablist">
            {(["branches", "tags"] as const).map((t) => (
              <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
                {t === "branches" ? "Branches" : "Tags"}
              </button>
            ))}
          </div>
          {tab === "branches" ? (
            <BranchList name={name} query={query} current={current} to={to} />
          ) : (
            <TagList name={name} query={query} current={current} to={to} />
          )}
        </div>
      )}
    </div>
  );
}

// useDebounced returns value once it has stopped changing for ms.
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return debounced;
}

type ListProps = Omit<RefPickerProps, "current"> & { query: string; current: string };

function BranchList({ name, query, current, to }: ListProps) {
  const q = useDebounced(query.trim(), 250);
  const state = useApi((signal) => listBranches(name, { query: q, pageSize: 100, signal }), [name, q]);
  if (state.status === "loading") return <Loading />;
  if (state.status === "error") return <p className="g2-error">Could not load branches.</p>;
  const items = state.data.branches.map((b) => ({ ref: b.name, badge: b.default ? "default" : undefined }));
  return <RefList items={items} current={current} to={to} empty="No branches match." />;
}

function TagList({ name, query, current, to }: ListProps) {
  const state = useApi((signal) => listTags(name, { pageSize: 100, signal }), [name]);
  if (state.status === "loading") return <Loading />;
  if (state.status === "error") return <p className="g2-error">Could not load tags.</p>;
  const q = query.trim().toLowerCase();
  const items = state.data.tags.filter((t) => t.name.toLowerCase().includes(q)).map((t) => ({ ref: t.name }));
  return <RefList items={items} current={current} to={to} empty="No tags match." />;
}

function RefList({
  items,
  current,
  to,
  empty,
}: {
  items: { ref: string; badge?: string }[];
  current: string;
  to: (ref: string) => string;
  empty: string;
}) {
  if (items.length === 0) return <p className="g2-muted dash-refpicker-empty">{empty}</p>;
  return (
    <ul className="dash-refpicker-list">
      {items.map(({ ref, badge }) => (
        <li key={ref}>
          <Link to={to(ref)} aria-current={ref === current ? "true" : undefined}>
            <span className="dash-refpicker-check" aria-hidden="true">
              {ref === current ? "✓" : ""}
            </span>
            {ref}
            {badge && <span className="dash-badge">{badge}</span>}
          </Link>
        </li>
      ))}
    </ul>
  );
}
