export type Theme = "light" | "dark";

const THEME_KEY = "git2.theme";

// storedTheme is the viewer's explicit choice, shared by Dash and Admin (same
// origin). null means "follow the system setting".
export function storedTheme(): Theme | null {
  try {
    const v = localStorage.getItem(THEME_KEY);
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

export function systemTheme(): Theme {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

// setTheme saves the choice and applies it through <html data-theme>, which
// theme.css reads.
export function setTheme(theme: Theme): void {
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // Storage blocked: the choice lasts for this page only.
  }
  document.documentElement.dataset.theme = theme;
}

// initTheme applies a saved choice before the first render, so the page does
// not flash in the other theme.
export function initTheme(): void {
  const theme = storedTheme();
  if (theme) document.documentElement.dataset.theme = theme;
}
