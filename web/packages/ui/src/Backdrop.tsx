import type { CSSProperties } from "react";

// BranchBackdrop draws a faint branch map behind the sign-in pages: lanes that
// fork and merge, commits on them, and a few commits travelling along. Purely
// decorative; the travelling dots stop when the viewer prefers reduced motion.

interface Lane {
  d: string;
  accent?: boolean;
  // Seconds for one trip of a travelling commit; no dot when absent.
  trip?: number;
  // Negative delay starts the dot part-way along, so they never line up.
  offset?: number;
}

const lanes: Lane[] = [
  { d: "M-20 520 H1460", trip: 26, offset: -6 },
  { d: "M120 520 C180 520 180 360 240 360 H620 C680 360 680 520 740 520", accent: true, trip: 14, offset: -3 },
  { d: "M380 360 C440 360 440 220 500 220 H900 C960 220 960 360 1020 360 H1460", trip: 30, offset: -18 },
  { d: "M-20 680 H260 C320 680 320 520 380 520" },
  { d: "M560 520 C620 520 620 680 680 680 H1460", trip: 22, offset: -11 },
  { d: "M-20 220 H180 C240 220 240 360 300 360" },
  { d: "M1060 680 C1120 680 1120 800 1180 800 H1460" },
];

const commits: [number, number, boolean?][] = [
  [60, 520], [460, 520], [860, 520], [1100, 520], [1320, 520],
  [300, 360, true], [440, 360, true], [560, 360, true],
  [620, 220], [780, 220], [1140, 360], [1320, 360],
  [90, 680], [190, 680],
  [800, 680], [960, 680], [1240, 800], [1380, 800],
  [60, 220], [130, 220],
];

export function BranchBackdrop() {
  return (
    <svg
      className="g2-backdrop"
      viewBox="0 0 1440 900"
      preserveAspectRatio="xMidYMid slice"
      aria-hidden="true"
      focusable="false"
    >
      {lanes.map((l) => (
        <path key={l.d} className={l.accent ? "g2-lane g2-lane-accent" : "g2-lane"} d={l.d} />
      ))}
      {commits.map(([x, y, accent]) => (
        <circle key={`${x}-${y}`} className={accent ? "g2-commit g2-commit-accent" : "g2-commit"} cx={x} cy={y} r="5" />
      ))}
      {lanes
        .filter((l) => l.trip)
        .map((l) => (
          <circle
            key={`pulse-${l.d}`}
            className="g2-pulse"
            r="4"
            style={
              {
                offsetPath: `path("${l.d}")`,
                animationDuration: `${l.trip}s`,
                animationDelay: `${l.offset ?? 0}s`,
              } as CSSProperties
            }
          />
        ))}
    </svg>
  );
}
