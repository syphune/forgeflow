#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

function localEnv() {
  try {
    return Object.fromEntries(readFileSync(".env.local", "utf8").split(/\r?\n/).flatMap((line) => {
      const match = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)\s*$/);
      if (!match) return [];
      let value = match[2].trim();
      if ((value.startsWith("\"") && value.endsWith("\"")) || (value.startsWith("'") && value.endsWith("'"))) value = value.slice(1, -1);
      return [[match[1], value]];
    }));
  } catch {
    return {};
  }
}

const command = process.argv[2] ?? "status";
if (!["status", "login", "logout", "switch"].includes(command)) {
  console.error("Usage: node scripts/codex-auth.mjs [status|login|logout|switch]");
  process.exit(2);
}

const envFile = localEnv();
const authPath = process.env.FORGEFLOW_CODEX_AUTH_HOST_PATH || envFile.FORGEFLOW_CODEX_AUTH_HOST_PATH || join(homedir(), ".codex", "auth.json");
if (!isAbsolute(authPath)) {
  console.error("FORGEFLOW_CODEX_AUTH_HOST_PATH must be an absolute path");
  process.exit(2);
}
const authFile = resolve(authPath);
const env = { ...process.env, CODEX_HOME: dirname(authFile) };

function run(args) {
  const result = spawnSync(process.env.CODEX_BIN || "codex", args, { env, stdio: "inherit" });
  if (result.error) {
    console.error(`could not run Codex CLI: ${result.error.message}`);
    return 1;
  }
  return result.status ?? 1;
}

if (command === "switch") {
  const logoutStatus = run(["logout"]);
  if (logoutStatus !== 0) process.exit(logoutStatus);
  process.exit(run(["login", "--device-auth"]));
}

process.exit(run({ status: ["login", "status"], login: ["login", "--device-auth"], logout: ["logout"] }[command]));
