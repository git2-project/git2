import type { RepoName, Repository } from "@git2/api";
import { createContext, useContext } from "react";

export interface RepoContextValue {
  name: RepoName;
  repository: Repository;
}

export const RepoContext = createContext<RepoContextValue | null>(null);

// useRepo returns the repository of the page; only pages under RepoLayout
// may call it.
export function useRepo(): RepoContextValue {
  const value = useContext(RepoContext);
  if (!value) throw new Error("useRepo outside RepoLayout");
  return value;
}
