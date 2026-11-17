// Parses the hunks dash-api returns in FileDiff.patch: unified diff text that
// starts at the first "@@" line, without the file header lines.

export type DiffLine =
  | { kind: "hunk"; text: string }
  | { kind: "context" | "add" | "del"; text: string; oldNo?: number; newNo?: number }
  | { kind: "note"; text: string }; // "\ No newline at end of file"

const hunkHeader = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

// parsePatch turns a patch into lines with their old and new line numbers.
// Text it does not understand is kept as context, so a surprise in the
// format shows up as plain lines instead of breaking the page.
export function parsePatch(patch: string): DiffLine[] {
  const out: DiffLine[] = [];
  let oldNo = 0;
  let newNo = 0;
  const lines = patch.split("\n");
  if (lines[lines.length - 1] === "") lines.pop(); // the patch's final newline

  for (const line of lines) {
    const m = hunkHeader.exec(line);
    if (m) {
      oldNo = Number(m[1]);
      newNo = Number(m[2]);
      out.push({ kind: "hunk", text: line });
    } else if (line.startsWith("+")) {
      out.push({ kind: "add", text: line.slice(1), newNo: newNo++ });
    } else if (line.startsWith("-")) {
      out.push({ kind: "del", text: line.slice(1), oldNo: oldNo++ });
    } else if (line.startsWith("\\")) {
      out.push({ kind: "note", text: line.slice(1).trim() });
    } else {
      out.push({ kind: "context", text: line.startsWith(" ") ? line.slice(1) : line, oldNo: oldNo++, newNo: newNo++ });
    }
  }
  return out;
}
