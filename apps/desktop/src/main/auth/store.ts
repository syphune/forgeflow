import { app, safeStorage } from "electron";
import fs from "node:fs/promises";
import path from "node:path";

export type DesktopAuth = {
  apiBaseURL: string;
  sessionCookie: string;
  csrfCookie: string;
};

function authPath(): string {
  return path.join(app.getPath("userData"), "desktop-auth.bin");
}

export async function saveDesktopAuth(auth: DesktopAuth): Promise<void> {
  if (!safeStorage.isEncryptionAvailable()) {
    throw new Error("OS credential storage is unavailable");
  }
  const payload = safeStorage.encryptString(JSON.stringify(auth));
  await fs.mkdir(path.dirname(authPath()), { recursive: true, mode: 0o700 });
  await fs.writeFile(authPath(), payload, { mode: 0o600 });
}

export async function loadDesktopAuth(): Promise<DesktopAuth | null> {
  if (!safeStorage.isEncryptionAvailable()) return null;
  try {
    const payload = await fs.readFile(authPath());
    const value: unknown = JSON.parse(safeStorage.decryptString(payload));
    if (!value || typeof value !== "object") return null;
    const auth = value as Partial<DesktopAuth>;
    if (
      typeof auth.apiBaseURL !== "string" ||
      typeof auth.sessionCookie !== "string" ||
      typeof auth.csrfCookie !== "string" ||
      !auth.apiBaseURL ||
      !auth.sessionCookie ||
      !auth.csrfCookie
    ) {
      return null;
    }
    return auth as DesktopAuth;
  } catch {
    return null;
  }
}

export async function clearDesktopAuth(): Promise<void> {
  await fs.rm(authPath(), { force: true });
}
