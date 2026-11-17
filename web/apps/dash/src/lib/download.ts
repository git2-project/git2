import { getRaw, type RepoName } from "@git2/api";

// downloadFile fetches a file through /raw (which needs the bearer header)
// and saves it from a Blob URL, as agreed for v0 (contract, open question 6).
export async function downloadFile(name: RepoName, path: string, ref: string): Promise<void> {
  const file = await getRaw(name, path, { ref });
  const href = URL.createObjectURL(file.data);
  try {
    const a = document.createElement("a");
    a.href = href;
    a.download = file.name;
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    // The click has started the download; the URL is no longer needed.
    setTimeout(() => URL.revokeObjectURL(href), 0);
  }
}
