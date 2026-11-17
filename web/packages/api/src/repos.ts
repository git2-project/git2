import { request } from "./client";
import type { components, operations } from "./generated/dash-api";

// Client for dash-api's repository and code-browsing endpoints
// (contracts/dash-api/openapi.yaml). Every type comes from the generated
// file, so a contract change shows up here as a type error.

type Schemas = components["schemas"];

export type Repository = Schemas["Repository"];
export type Branch = Schemas["Branch"];
export type Tag = Schemas["Tag"];
export type Tree = Schemas["Tree"];
export type TreeEntry = Schemas["TreeEntry"];
export type Blob = Schemas["Blob"];
export type Commit = Schemas["Commit"];
export type CommitRef = Schemas["CommitRef"];
export type CommitDetail = Schemas["CommitDetail"];
export type FileDiff = Schemas["FileDiff"];
export type DiffStats = Schemas["DiffStats"];
export type Signature = Schemas["Signature"];

type Ok<Op extends keyof operations> = operations[Op] extends {
  responses: { 200: { content: { "application/json": infer T } } };
}
  ? T
  : never;

export type RepositoryPage = Ok<"listRepositories">;
export type BranchPage = Ok<"listBranches">;
export type TagPage = Ok<"listTags">;
export type CommitPage = Ok<"listCommits">;

export interface PageOptions {
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}

// RepoName names a repository by its owner and name, as in its URL.
export interface RepoName {
  owner: string;
  repo: string;
}

type Query = Record<string, string | number | undefined>;

// url builds an /api path. Owner and repository names never contain "/",
// but they are escaped anyway; ref and path go in the query because they do.
function url(path: string, query: Query = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const qs = params.toString();
  return `/api${path}${qs ? `?${qs}` : ""}`;
}

function repoPath({ owner, repo }: RepoName, rest = ""): string {
  return `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}${rest}`;
}

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await request(path, { auth: true, signal });
  return (await res.json()) as T;
}

function page(opts: PageOptions): Query {
  return { page_size: opts.pageSize, page_token: opts.pageToken };
}

// listRepositories lists one owner's repositories; the caller's when owner
// is left out.
export function listRepositories(opts: PageOptions & { owner?: string } = {}): Promise<RepositoryPage> {
  return getJSON(url("/repos", { owner: opts.owner, ...page(opts) }), opts.signal);
}

export function getRepository(name: RepoName, signal?: AbortSignal): Promise<Repository> {
  return getJSON(url(repoPath(name)), signal);
}

// listBranches lists branches, the default branch first. query filters by
// name (case-insensitive), for the branch picker.
export function listBranches(name: RepoName, opts: PageOptions & { query?: string } = {}): Promise<BranchPage> {
  return getJSON(url(repoPath(name, "/branches"), { q: opts.query, ...page(opts) }), opts.signal);
}

export function listTags(name: RepoName, opts: PageOptions = {}): Promise<TagPage> {
  return getJSON(url(repoPath(name, "/tags"), page(opts)), opts.signal);
}

// RefOptions picks what to read: ref defaults to the default branch.
export interface RefOptions {
  ref?: string;
  signal?: AbortSignal;
}

// getTree lists one directory; path "" is the root.
export function getTree(name: RepoName, path: string, opts: RefOptions = {}): Promise<Tree> {
  return getJSON(url(repoPath(name, "/tree"), { ref: opts.ref, path }), opts.signal);
}

export function getBlob(name: RepoName, path: string, opts: RefOptions = {}): Promise<Blob> {
  return getJSON(url(repoPath(name, "/blob"), { ref: opts.ref, path }), opts.signal);
}

// RawFile is a downloaded file. /raw needs the bearer header, so Dash fetches
// the bytes and hands them to the browser as a Blob URL (contract, open
// question 6). Files over 10 MiB answer 413.
export interface RawFile {
  name: string;
  data: globalThis.Blob;
}

export async function getRaw(name: RepoName, path: string, opts: RefOptions = {}): Promise<RawFile> {
  const res = await request(url(repoPath(name, "/raw"), { ref: opts.ref, path }), {
    auth: true,
    signal: opts.signal,
  });
  return { name: fileNameFrom(res) ?? path.split("/").pop() ?? path, data: await res.blob() };
}

// fileNameFrom reads the name from Content-Disposition: attachment; filename="x".
function fileNameFrom(res: Response): string | undefined {
  const header = res.headers.get("Content-Disposition") ?? "";
  const utf8 = /filename\*=UTF-8''([^;]+)/i.exec(header);
  if (utf8) {
    try {
      return decodeURIComponent(utf8[1]);
    } catch {
      // Malformed escape: fall back to the plain filename.
    }
  }
  return /filename="([^"]*)"/i.exec(header)?.[1];
}

// listCommits lists commits reachable from ref, newest first; with path,
// only those that changed it.
export function listCommits(
  name: RepoName,
  opts: PageOptions & RefOptions & { path?: string } = {},
): Promise<CommitPage> {
  return getJSON(url(repoPath(name, "/commits"), { ref: opts.ref, path: opts.path, ...page(opts) }), opts.signal);
}

// getCommit returns one commit with its diff against the first parent. sha
// is a full SHA or a prefix of at least 7 characters.
export function getCommit(name: RepoName, sha: string, signal?: AbortSignal): Promise<CommitDetail> {
  return getJSON(url(repoPath(name, `/commits/${encodeURIComponent(sha)}`)), signal);
}
