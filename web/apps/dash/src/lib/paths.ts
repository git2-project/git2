import type { RepoName } from "@git2/api";

// Links inside a repository. A ref can contain "/" (feature/login), so it
// travels as one encoded segment; react-router decodes it back into the
// :ref param. Each part of a file path is encoded on its own, so "/" stays a
// separator and names like "a#b" or "100%" survive.

function encodePath(path: string): string {
  return path.split("/").map(encodeURIComponent).join("/");
}

export function repoHome({ owner, repo }: RepoName): string {
  return `/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
}

export function treeLink(name: RepoName, ref: string, path = ""): string {
  const base = `${repoHome(name)}/tree/${encodeURIComponent(ref)}`;
  return path ? `${base}/${encodePath(path)}` : base;
}

export function blobLink(name: RepoName, ref: string, path: string): string {
  return `${repoHome(name)}/blob/${encodeURIComponent(ref)}/${encodePath(path)}`;
}

export function commitsLink(name: RepoName, ref: string, path?: string): string {
  const base = `${repoHome(name)}/commits/${encodeURIComponent(ref)}`;
  return path ? `${base}?path=${encodeURIComponent(path)}` : base;
}

export function commitLink(name: RepoName, sha: string): string {
  return `${repoHome(name)}/commit/${encodeURIComponent(sha)}`;
}

export function shortSha(sha: string): string {
  return sha.slice(0, 7);
}
