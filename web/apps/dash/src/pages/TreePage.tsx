import { getTree, listBranches, type RepoName, type Tree } from "@git2/api";
import { Link, useParams } from "react-router-dom";
import { Breadcrumb } from "../components/Breadcrumb";
import { CommitLine } from "../components/CommitLine";
import { EntryIcon } from "../components/EntryIcon";
import { RefPicker } from "../components/RefPicker";
import { Await, ErrorView, Loading } from "../components/Status";
import { fileSize } from "../lib/format";
import { blobLink, commitsLink, treeLink } from "../lib/paths";
import { useApi } from "../lib/useApi";
import { useRepo } from "../repo/RepoContext";
import { Readme } from "./Readme";

// TreePage lists one directory. Without a ref in the URL (the repository's
// home) it reads the default branch.
export function TreePage() {
  const { name, repository } = useRepo();
  const params = useParams();
  const gitRef = params.ref ?? repository.default_branch;
  const path = params["*"] ?? "";
  const state = useApi((signal) => getTree(name, path, { ref: gitRef, signal }), [name, gitRef, path]);

  // A repository can have commits while its default branch has none (a push
  // to another branch): the home page then offers the branches instead.
  if (state.status === "error" && state.error.status === 404 && !params.ref && !path) {
    return <NoDefaultBranch name={name} defaultBranch={repository.default_branch} />;
  }

  return (
    <Await state={state} notFound={`Nothing at this path on ${gitRef}.`}>
      {(tree) =>
        tree.empty ? (
          <EmptyRepository cloneURL={repository.clone_url} defaultBranch={repository.default_branch} />
        ) : (
          <TreeView name={name} gitRef={gitRef} tree={tree} />
        )
      }
    </Await>
  );
}

function TreeView({ name, gitRef, tree }: { name: RepoName; gitRef: string; tree: Tree }) {
  const readme = tree.entries.find((e) => e.type === "file" && /^readme(\.(md|markdown|txt|rst))?$/i.test(e.name));
  const parent = tree.path.includes("/") ? tree.path.slice(0, tree.path.lastIndexOf("/")) : "";

  return (
    <section className="dash-page">
      <div className="dash-toolbar">
        <RefPicker name={name} current={gitRef} to={(ref) => treeLink(name, ref, tree.path)} />
        <Breadcrumb name={name} gitRef={gitRef} path={tree.path} />
      </div>

      <div className="dash-box">
        <div className="dash-box-header">
          {tree.last_commit ? <CommitLine name={name} commit={tree.last_commit} /> : <span />}
          <Link to={commitsLink(name, gitRef, tree.path || undefined)} className="dash-nowrap">
            History
          </Link>
        </div>
        <table className="dash-tree">
          <tbody>
            {tree.path && (
              <tr>
                <td colSpan={2}>
                  <Link to={treeLink(name, gitRef, parent)} aria-label="Parent directory">
                    ..
                  </Link>
                </td>
              </tr>
            )}
            {tree.entries.map((e) => (
              <tr key={e.path}>
                <td>
                  <div className="dash-tree-name">
                    <EntryIcon type={e.type} />
                    {e.type === "dir" ? (
                      <Link to={treeLink(name, gitRef, e.path)}>{e.name}</Link>
                    ) : e.type === "submodule" ? (
                      <span title="Submodules cannot be browsed">
                        {e.name} <span className="g2-muted">@ {e.sha.slice(0, 7)}</span>
                      </span>
                    ) : (
                      <Link to={blobLink(name, gitRef, e.path)}>{e.name}</Link>
                    )}
                  </div>
                </td>
                <td className="g2-muted dash-tree-size">{e.size != null ? fileSize(e.size) : ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {tree.truncated && (
          <p className="g2-notice dash-box-footer">
            This directory has more than 10,000 entries; only the first 10,000 are shown. Clone the
            repository to see all of them.
          </p>
        )}
      </div>

      {readme && <Readme name={name} gitRef={gitRef} path={readme.path} />}
    </section>
  );
}

function EmptyRepository({ cloneURL, defaultBranch }: { cloneURL: string; defaultBranch: string }) {
  return (
    <section className="dash-page dash-empty">
      <h2>This repository is empty</h2>
      <p className="g2-muted">Push your first commit to get started:</p>
      <pre className="dash-code-block">
        {`git clone ${cloneURL}
cd ${cloneURL.split("/").pop()?.replace(/\.git$/, "") ?? "repo"}
git switch -c ${defaultBranch}
echo "# Hello" > README.md
git add README.md
git commit -m "Initial commit"
git push -u origin ${defaultBranch}`}
      </pre>
    </section>
  );
}

function NoDefaultBranch({ name, defaultBranch }: { name: RepoName; defaultBranch: string }) {
  const state = useApi((signal) => listBranches(name, { signal }), [name]);
  if (state.status === "loading") return <Loading />;
  if (state.status === "error") return <ErrorView error={state.error} />;
  return (
    <section className="dash-page dash-empty">
      <h2>
        The default branch <code>{defaultBranch}</code> has no commits yet
      </h2>
      {state.data.branches.length > 0 ? (
        <>
          <p className="g2-muted">This repository has commits on other branches:</p>
          <ul className="dash-branch-list">
            {state.data.branches.map((b) => (
              <li key={b.name}>
                <Link to={treeLink(name, b.name)}>{b.name}</Link>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p className="g2-muted">No branches found.</p>
      )}
    </section>
  );
}
