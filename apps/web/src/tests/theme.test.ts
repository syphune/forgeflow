import { describe, expect, it } from "vitest";
import { parseThemePreference } from "@forgeflow/ui";

describe("parseThemePreference", () => {
  it.each(["light", "dark", "system"] as const)("keeps %s", (theme) => {
    expect(parseThemePreference(theme)).toBe(theme);
  });

  it.each([undefined, null, "", "sepia"])("falls back to system for %s", (theme) => {
    expect(parseThemePreference(theme)).toBe("system");
  });
});
