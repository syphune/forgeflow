import type { Metadata } from "next";
import { cookies } from "next/headers";
import { parseThemePreference, THEME_PREFERENCE_KEY } from "@forgeflow/ui";
import "./globals.css";

export const metadata: Metadata = {
  title: "Forgeflow",
  description: "Quản lý công việc kỹ thuật dựa trên bằng chứng",
};

export default async function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const cookieStore = await cookies();
  const savedLocale = cookieStore.get("forgeflow_locale")?.value;
  const locale = savedLocale === "en" || (savedLocale !== "vi" && process.env.NEXT_PUBLIC_FORGEFLOW_LOCALE === "en") ? "en" : "vi";
  const theme = parseThemePreference(cookieStore.get(THEME_PREFERENCE_KEY)?.value);
  return (
    <html lang={locale} data-theme={theme}>
      <body>{children}</body>
    </html>
  );
}
