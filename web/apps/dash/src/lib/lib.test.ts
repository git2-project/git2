import { describe, expect, it } from "vitest";
import { parsePatch } from "./diff";
import { fileSize, timeAgo } from "./format";
import { guessLanguage } from "./highlight";
import { blobLink, commitsLink, repoHome, treeLink } from "./paths";

const repo = { owner: "ada", repo: "billing-service" };

describe("paths", () => {
  it("keeps a ref with slashes in one segment", () => {
    expect(treeLink(repo, "feature/login")).toBe("/ada/billing-service/tree/feature%2Flogin");
  });

  it("encodes each part of a file path but keeps the separators", () => {
    expect(blobLink(repo, "main", "docs/100% sure#1.md")).toBe("/ada/billing-service/blob/main/docs/100%25%20sure%231.md");
  });

  it("puts the history path in the query", () => {
    expect(commitsLink(repo, "main", "src/app.ts")).toBe("/ada/billing-service/commits/main?path=src%2Fapp.ts");
    expect(commitsLink(repo, "main")).toBe("/ada/billing-service/commits/main");
  });

  it("links the repository home", () => {
    expect(repoHome(repo)).toBe("/ada/billing-service");
  });
});

describe("parsePatch", () => {
  it("numbers old and new lines from the hunk header", () => {
    const lines = parsePatch("@@ -10,3 +10,4 @@ func main() {\n a\n-b\n+c\n+d\n e\n");
    expect(lines).toEqual([
      { kind: "hunk", text: "@@ -10,3 +10,4 @@ func main() {" },
      { kind: "context", text: "a", oldNo: 10, newNo: 10 },
      { kind: "del", text: "b", oldNo: 11 },
      { kind: "add", text: "c", newNo: 11 },
      { kind: "add", text: "d", newNo: 12 },
      { kind: "context", text: "e", oldNo: 12, newNo: 13 },
    ]);
  });

  it("handles single-line hunks and the missing-newline note", () => {
    const lines = parsePatch("@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n");
    expect(lines.map((l) => l.kind)).toEqual(["hunk", "del", "note", "add"]);
    expect(lines[2]).toEqual({ kind: "note", text: "No newline at end of file" });
  });

  it("restarts numbering at each hunk", () => {
    const lines = parsePatch("@@ -1,1 +1,1 @@\n-a\n+b\n@@ -50,1 +50,1 @@\n-y\n+z");
    expect(lines[4]).toEqual({ kind: "del", text: "y", oldNo: 50 });
  });
});

describe("format", () => {
  it("formats file sizes", () => {
    expect(fileSize(512)).toBe("512 B");
    expect(fileSize(1536)).toBe("1.5 KiB");
    expect(fileSize(3 * 1024 * 1024)).toBe("3.0 MiB");
  });

  it("formats relative times", () => {
    const now = Date.parse("2026-11-08T12:00:00Z");
    expect(timeAgo("2026-11-05T12:00:00Z", now)).toBe("3 days ago");
    expect(timeAgo("2026-11-08T11:59:30Z", now)).toBe("30 seconds ago");
  });
});

describe("guessLanguage", () => {
  it("uses the extension, or a known file name", () => {
    expect(guessLanguage("main.go")).toBe("go");
    expect(guessLanguage("Dockerfile")).toBe("docker");
    expect(guessLanguage("LICENSE")).toBeUndefined();
  });
});
