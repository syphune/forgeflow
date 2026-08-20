import { expect, test } from "@playwright/test";

test("the default root opens the new application shell", async ({ page }) => {
  await page.route("**/api/v1/me", async (route) => {
    await route.fulfill({ status: 401, json: { code: "UNAUTHORIZED", message: "sign in required" } });
  });

  await page.goto("/");

  await expect(page).toHaveURL(/\/app$/);
  await expect(page.locator(".app-v2-auth-gate")).toContainText("Đăng nhập để mở không gian làm việc");
  await expect(page.getByRole("link", { name: /Đăng nhập với GitHub/ })).toBeVisible();
});
