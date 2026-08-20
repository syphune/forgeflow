import { AppShell } from "@/features/app/app-shell";

export const dynamic = "force-dynamic";

export default function AppLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <AppShell>{children}</AppShell>;
}
