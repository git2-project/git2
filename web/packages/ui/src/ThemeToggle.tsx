import { useState } from "react";
import { MoonIcon, SunIcon } from "./icons";
import { setTheme, storedTheme, systemTheme, type Theme } from "./theme";

export function ThemeToggle() {
  const [theme, setThemeState] = useState<Theme>(() => storedTheme() ?? systemTheme());
  const next: Theme = theme === "dark" ? "light" : "dark";

  return (
    <button
      type="button"
      className="g2-icon-button"
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
      onClick={() => {
        setTheme(next);
        setThemeState(next);
      }}
    >
      {theme === "dark" ? <SunIcon /> : <MoonIcon />}
    </button>
  );
}
