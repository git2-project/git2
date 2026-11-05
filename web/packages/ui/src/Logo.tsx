// The git2 mark (a branch merging back) and wordmark. Drawn in currentColor so
// it works on the page background and on the primary-coloured panel.
export function Logo({ plane }: { plane?: string }) {
  return (
    <span className="g2-logo">
      <svg viewBox="0 0 28 28" width="28" height="28" aria-hidden="true" focusable="false">
        <g fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round">
          <path d="M8 6.5v15" />
          <path d="M8 11c0 4.5 4 5.5 11.5 5.5" />
        </g>
        <g fill="currentColor">
          <circle cx="8" cy="6.5" r="3" />
          <circle cx="8" cy="21.5" r="3" />
          <circle cx="20" cy="16.5" r="3" />
        </g>
      </svg>
      <span className="g2-logo-word">git2</span>
      {plane && <span className="g2-logo-plane">{plane}</span>}
    </span>
  );
}
