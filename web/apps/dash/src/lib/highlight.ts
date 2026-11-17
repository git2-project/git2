// Syntax highlighting with Shiki. Shiki and each language's grammar are loaded
// on first use, so pages without code do not pay for them.

// Above these sizes the file is shown as plain text: highlighting a 1 MiB
// file blocks the page for seconds.
export const MAX_HIGHLIGHT_BYTES = 256 * 1024;
export const MAX_HIGHLIGHT_LINES = 10_000;

// File names Shiki cannot guess from an extension.
const byName: Record<string, string> = {
  dockerfile: "docker",
  makefile: "make",
  "cmakelists.txt": "cmake",
  ".gitignore": "ini",
  ".env": "dotenv",
  "go.mod": "go",
};

// guessLanguage picks a Shiki language from a file name when dash-api sent
// none. Shiki's IDs and aliases cover most extensions (ts, py, go, yml, md).
export function guessLanguage(fileName: string): string | undefined {
  const lower = fileName.toLowerCase();
  if (byName[lower]) return byName[lower];
  const dot = lower.lastIndexOf(".");
  return dot > 0 ? lower.slice(dot + 1) : undefined;
}

// highlight returns Shiki's HTML for code, with light and dark colors as CSS
// variables (the theme switch picks one), or null when lang is unknown.
export async function highlight(code: string, lang: string): Promise<string | null> {
  const { codeToHtml, bundledLanguages } = await import("shiki");
  if (!(lang in bundledLanguages)) return null;
  return codeToHtml(code, {
    lang,
    themes: { light: "github-light", dark: "github-dark" },
    defaultColor: false,
  });
}
