import { expect, test } from "@playwright/test";

test("theme follows the saved preference and system color scheme", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({
      status: 401,
      json: { code: "UNAUTHORIZED", message: "sign in required" },
    });
  });

  await page.goto("/app");

  const root = page.locator("html");
  const theme = page.locator(".app-v2-theme-switcher .theme-control-trigger");
  const menu = page.locator(".app-v2-theme-switcher .theme-control-menu");
  const option = (label: string) => menu.getByRole("menuitemradio", { name: label, exact: true });
  const canvas = () => root.evaluate((element) => getComputedStyle(element).getPropertyValue("--canvas").trim());

  await expect(theme).toHaveAttribute("aria-label", "Giao diện");
  await expect(theme).toHaveAttribute("title", "Giao diện: Theo hệ thống");
  await expect(root).toHaveAttribute("data-theme", "system");
  expect(await canvas()).toBe("#f5f7f5");
  await theme.click();
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitemradio")).toHaveText(["Theo hệ thống", "Sáng", "Tối"]);

  await theme.focus();
  await expect(theme).toBeFocused();

  await option("Tối").click();
  await expect(root).toHaveAttribute("data-theme", "dark");
  await expect(theme).toHaveAttribute("title", "Giao diện: Tối");
  expect(await canvas()).toBe("#0d0f10");

  await page.reload();
  await expect(theme).toHaveAttribute("title", "Giao diện: Tối");
  await expect(root).toHaveAttribute("data-theme", "dark");
  expect(await canvas()).toBe("#0d0f10");

  await theme.click();
  await option("Sáng").click();
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(root).toHaveAttribute("data-theme", "light");
  expect(await canvas()).toBe("#f5f7f5");

  await theme.click();
  await option("Theo hệ thống").click();
  expect(await canvas()).toBe("#0d0f10");
  await page.emulateMedia({ colorScheme: "light" });
  expect(await canvas()).toBe("#f5f7f5");
});
