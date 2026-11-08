import { useEffect, useState } from "react";
import { MAX_HIGHLIGHT_BYTES, MAX_HIGHLIGHT_LINES, highlight } from "../lib/highlight";

// CodeView shows text with line numbers, highlighted when the language is
// known and the file is small enough. The plain version shows at once and
// the highlighted one replaces it when Shiki is ready.
export function CodeView({ code, language }: { code: string; language?: string | null }) {
  const [html, setHtml] = useState<string | null>(null);
  const lines = code.endsWith("\n") ? code.slice(0, -1).split("\n") : code.split("\n");
  const tooBig = code.length > MAX_HIGHLIGHT_BYTES || lines.length > MAX_HIGHLIGHT_LINES;

  useEffect(() => {
    setHtml(null);
    if (!language || tooBig) return;
    let live = true;
    highlight(code, language).then(
      (out) => {
        if (live) setHtml(out);
      },
      () => {
        // A grammar that fails to load leaves the plain text in place.
      },
    );
    return () => {
      live = false;
    };
  }, [code, language, tooBig]);

  if (html) {
    // Shiki escapes the code; the HTML holds only its own spans.
    return <div className="dash-code" dangerouslySetInnerHTML={{ __html: html }} />;
  }
  return (
    <div className="dash-code">
      <pre className="shiki">
        <code>
          {lines.map((line, i) => (
            <span key={i} className="line">
              {line}
              {"\n"}
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}
