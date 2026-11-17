import type { TreeEntry } from "@git2/api";

const paths: Record<TreeEntry["type"], string> = {
  dir: "M3 6.5A1.5 1.5 0 0 1 4.5 5h4l1.5 2h5.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5z",
  file: "M6 3h5l4 4v9.5a.5.5 0 0 1-.5.5h-8.5a.5.5 0 0 1-.5-.5v-13A.5.5 0 0 1 6 3zM11 3v4h4",
  symlink: "M8 12l4-4M9 7h4v4M6 3h5l4 4v9.5a.5.5 0 0 1-.5.5h-8.5a.5.5 0 0 1-.5-.5v-13A.5.5 0 0 1 6 3z",
  submodule: "M4 5h12v10H4zM4 8h12M8 5v3",
};

const labels: Record<TreeEntry["type"], string> = {
  dir: "Directory",
  file: "File",
  symlink: "Symbolic link",
  submodule: "Submodule",
};

export function EntryIcon({ type }: { type: TreeEntry["type"] }) {
  return (
    <svg
      className={`dash-entry-icon dash-entry-${type}`}
      width="18"
      height="18"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinejoin="round"
      role="img"
      aria-label={labels[type]}
    >
      <path d={paths[type]} />
    </svg>
  );
}
