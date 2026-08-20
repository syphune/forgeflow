"use client";

import { useRef, useSyncExternalStore } from "react";
import {
  parseThemePreference,
  THEME_PREFERENCE_KEY,
  type ThemePreference,
  translate as t,
} from "@forgeflow/ui";

type Props = { className?: string };
const THEME_OPTIONS: ThemePreference[] = ["system", "light", "dark"];
const THEME_CHANGE_EVENT = "forgeflow-theme-change";

function getThemeSnapshot(): ThemePreference {
  return typeof document === "undefined"
    ? "system"
    : parseThemePreference(document.documentElement.dataset.theme);
}

function subscribeToTheme(onChange: () => void) {
  if (typeof window === "undefined") return () => {};
  window.addEventListener(THEME_CHANGE_EVENT, onChange);
  return () => window.removeEventListener(THEME_CHANGE_EVENT, onChange);
}

function applyTheme(theme: ThemePreference) {
  document.documentElement.dataset.theme = theme;
  try {
    document.cookie = `${THEME_PREFERENCE_KEY}=${theme}; path=/; max-age=31536000; samesite=lax`;
  } catch {
    // Keep the current-page preference when cookies are unavailable.
  }
  window.dispatchEvent(new Event(THEME_CHANGE_EVENT));
}

function ThemeIcon({ theme }: { theme: ThemePreference }) {
  if (theme === "light") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42" />
      </svg>
    );
  }

  if (theme === "dark") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
        <path d="M20.5 14.5A8.5 8.5 0 0 1 9.5 3.5a8.5 8.5 0 1 0 11 11Z" />
      </svg>
    );
  }

  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </svg>
  );
}

export function ThemeControl({ className = "" }: Props) {
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const theme = useSyncExternalStore(subscribeToTheme, getThemeSnapshot, (): ThemePreference => "system");

  function changeTheme(value: string) {
    const nextTheme = parseThemePreference(value);
    applyTheme(nextTheme);
    if (detailsRef.current) detailsRef.current.open = false;
  }

  return (
    <details ref={detailsRef} className={`theme-control ${className}`.trim()}>
      <summary
        className="theme-control-trigger"
        aria-label={t("nav.theme")}
        aria-haspopup="menu"
        title={`${t("nav.theme")}: ${t(`theme.${theme}`)}`}
      >
        <span className="theme-icon">
          <ThemeIcon theme={theme} />
        </span>
      </summary>
      <div className="theme-control-menu" role="menu" aria-label={t("nav.theme")}>
        {THEME_OPTIONS.map((option) => (
          <button
            key={option}
            type="button"
            role="menuitemradio"
            aria-checked={theme === option}
            onClick={() => changeTheme(option)}
          >
            <span className="theme-icon">
              <ThemeIcon theme={option} />
            </span>
            <span>{t(`theme.${option}`)}</span>
          </button>
        ))}
      </div>
    </details>
  );
}
