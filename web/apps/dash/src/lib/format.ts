const units = [
  { limit: 60, div: 1, unit: "second" },
  { limit: 3600, div: 60, unit: "minute" },
  { limit: 86400, div: 3600, unit: "hour" },
  { limit: 86400 * 30, div: 86400, unit: "day" },
  { limit: 86400 * 365, div: 86400 * 30, unit: "month" },
  { limit: Infinity, div: 86400 * 365, unit: "year" },
] as const;

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

// timeAgo formats an ISO date as "3 days ago", relative to now.
export function timeAgo(iso: string, now = Date.now()): string {
  const seconds = Math.round((new Date(iso).getTime() - now) / 1000);
  const abs = Math.abs(seconds);
  const u = units.find((x) => abs < x.limit) ?? units[units.length - 1];
  return rtf.format(Math.round(seconds / u.div), u.unit);
}

// fullDate is the tooltip for timeAgo: the date and time in the viewer's zone.
export function fullDate(iso: string): string {
  return new Date(iso).toLocaleString();
}

// fileSize formats bytes as "512 B", "1.4 KiB", "3.2 MiB".
export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
