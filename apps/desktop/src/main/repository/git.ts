import { execFile } from "node:child_process";
import { promisify } from "node:util";
import path from "node:path";
import fs from "node:fs/promises";

const execFileAsync = promisify(execFile);
const MAX_OUTPUT = 256 * 1024;

export type RepositoryStatus = {
  root: string;
  branch: string;
  clean: boolean;
  porcelain: string;
};

export type RepositoryDiff = {
  root: string;
  files: string[];
  patch: string;
};

export type RepositoryCommit = RepositoryStatus & { commitSHA: string };

export async function repositoryStatus(
  root: string,
): Promise<RepositoryStatus> {
  const safeRoot = await resolveDirectory(root);
  const result = await execFileAsync(
    "git",
    ["-C", safeRoot, "status", "--porcelain=v1", "--branch"],
    {
      shell: false,
      timeout: 10_000,
      maxBuffer: MAX_OUTPUT,
      windowsHide: true,
    },
  );
  const lines = result.stdout.split("\n");
  const branch =
    lines
      .find((line) => line.startsWith("## "))
      ?.slice(3)
      .split("...")[0] ?? "";
  const porcelain = lines
    .filter((line) => !line.startsWith("## ") && line.length > 0)
    .join("\n");
  return { root: safeRoot, branch, clean: porcelain.length === 0, porcelain };
}

export async function repositoryDiff(root: string): Promise<RepositoryDiff> {
  const safeRoot = await resolveDirectory(root);
  const [filesResult, patchResult, untrackedResult] = await Promise.all([
    execFileAsync("git", ["-C", safeRoot, "diff", "HEAD", "--name-only", "--no-ext-diff"], { shell: false, timeout: 10_000, maxBuffer: MAX_OUTPUT, windowsHide: true }),
    execFileAsync("git", ["-C", safeRoot, "diff", "HEAD", "--no-ext-diff", "--unified=3"], { shell: false, timeout: 10_000, maxBuffer: MAX_OUTPUT, windowsHide: true }),
    execFileAsync("git", ["-C", safeRoot, "ls-files", "--others", "--exclude-standard", "-z"], { shell: false, timeout: 10_000, maxBuffer: MAX_OUTPUT, windowsHide: true }),
  ]);
  const untrackedFiles = untrackedResult.stdout.split("\0").filter(Boolean);
  const files = [
    ...filesResult.stdout.split("\n").map((line) => line.trim()).filter(Boolean),
    ...untrackedFiles,
  ];
  let patch = patchResult.stdout.slice(0, MAX_OUTPUT);
  for (const file of untrackedFiles) {
    if (patch.length >= MAX_OUTPUT) break;
    const candidate = assertWithinRoot(safeRoot, path.join(safeRoot, file));
    const stat = await fs.lstat(candidate);
    if (!stat.isFile() || stat.size > 128 * 1024) {
      patch += `\n\nUntracked file omitted from textual diff: ${file}\n`;
      continue;
    }
    const content = await fs.readFile(candidate);
    const relative = file.replaceAll("\\", "/");
    if (content.includes(0)) {
      patch += `\n\ndiff --git a/${relative} b/${relative}\nnew file (binary)\n`;
      continue;
    }
    const text = content.toString("utf8").replace(/\r\n/g, "\n");
    const lines = text ? text.split("\n") : [];
    const body = lines.map((line) => `+${line}`).join("\n");
    patch += `\n\ndiff --git a/${relative} b/${relative}\nnew file mode 100644\n--- /dev/null\n+++ b/${relative}\n@@ -0,0 +${lines.length} @@\n${body}`;
  }
  return {
    root: safeRoot,
    files: [...new Set(files)],
    patch: patch.slice(0, MAX_OUTPUT),
  };
}

export async function resolveDirectory(candidate: string): Promise<string> {
  if (!candidate || !path.isAbsolute(candidate))
    throw new Error("repository path must be absolute");
  const resolved = await fs.realpath(candidate);
  const stat = await fs.stat(resolved);
  if (!stat.isDirectory())
    throw new Error("repository path must be a directory");
  return resolved;
}

export function assertWithinRoot(root: string, candidate: string): string {
  const resolvedRoot = path.resolve(root);
  const resolvedCandidate = path.resolve(candidate);
  const relative = path.relative(resolvedRoot, resolvedCandidate);
  if (
    relative === "" ||
    (!relative.startsWith(".." + path.sep) &&
      relative !== ".." &&
      !path.isAbsolute(relative))
  ) {
    return resolvedCandidate;
  }
  throw new Error("path is outside the managed worktree root");
}

export async function resolveManagedWorktree(
  root: string,
  candidate: string,
): Promise<string> {
  const safeRoot = await resolveDirectory(root);
  const safeCandidate = assertWithinRoot(safeRoot, candidate);
  const resolvedCandidate = await resolveDirectory(safeCandidate);
  assertWithinRoot(safeRoot, resolvedCandidate);
  return resolvedCandidate;
}

export function validateWorktreeName(name: string): string {
  const value = name.trim();
  if (
    !value ||
    value.length > 80 ||
    !/^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(value) ||
    value === "." ||
    value === ".."
  ) {
    throw new Error("worktree name is invalid");
  }
  return value;
}

export function validateBranchName(name: string): string {
  const value = name.trim();
  if (
    !value ||
    value.length > 128 ||
    value.startsWith("-") ||
    value.endsWith("/") ||
    value.endsWith(".") ||
    value.includes("..") ||
    value.includes("//") ||
    value.includes("@{") ||
    !/^[a-zA-Z0-9][a-zA-Z0-9._/-]*$/.test(value)
  ) {
    throw new Error("branch name is invalid");
  }
  return value;
}

export function assertPushableBranch(branch: string): string {
  const value = validateBranchName(branch);
  if (["main", "master", "develop", "production", "release"].includes(value.toLowerCase())) {
    throw new Error("protected branches cannot be pushed by Forgeflow");
  }
  return value;
}

export async function createManagedWorktree(
  managedRoot: string,
  sourceRoot: string,
  name: string,
  baseRef = "HEAD",
  branchName?: string,
): Promise<RepositoryStatus> {
  const safeRoot = await resolveDirectory(managedRoot);
  const source = await resolveDirectory(sourceRoot);
  const safeName = validateWorktreeName(name);
  const branch = validateBranchName(branchName?.trim() || `forgeflow/${safeName}`);
  const ref = baseRef.trim() || "HEAD";
  if (ref.length > 256 || ref.includes("\0") || ref.startsWith("-"))
    throw new Error("base ref is invalid");
  const target = assertWithinRoot(safeRoot, path.join(safeRoot, safeName));
  try {
    await fs.lstat(target);
    throw new Error("managed worktree already exists");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
  await execFileAsync(
    "git",
    ["-C", source, "worktree", "add", "-b", branch, target, ref],
    {
      shell: false,
      timeout: 60_000,
      maxBuffer: MAX_OUTPUT,
      windowsHide: true,
    },
  );
  return repositoryStatus(target);
}

export async function commitWorktree(
  candidate: string,
  message: string,
): Promise<RepositoryCommit> {
  const safeCandidate = await resolveDirectory(candidate);
  const safeMessage = message.trim();
  if (!safeMessage || safeMessage.length > 200 || safeMessage.includes("\0")) {
    throw new Error("commit message is required and bounded");
  }
  const status = await repositoryStatus(safeCandidate);
  if (status.clean) throw new Error("there are no changes to commit");
  await execFileAsync("git", ["-C", safeCandidate, "add", "--all", "--", "."], {
    shell: false,
    timeout: 60_000,
    maxBuffer: MAX_OUTPUT,
    windowsHide: true,
  });
  await execFileAsync("git", ["-C", safeCandidate, "commit", "-m", safeMessage], {
    shell: false,
    timeout: 120_000,
    maxBuffer: MAX_OUTPUT,
    windowsHide: true,
  });
  const commit = await execFileAsync("git", ["-C", safeCandidate, "rev-parse", "HEAD"], {
    shell: false,
    timeout: 10_000,
    maxBuffer: 4096,
    windowsHide: true,
  });
  return { ...(await repositoryStatus(safeCandidate)), commitSHA: commit.stdout.trim() };
}

export async function pushWorktree(candidate: string): Promise<RepositoryStatus> {
  const safeCandidate = await resolveDirectory(candidate);
  const branch = assertPushableBranch((await repositoryStatus(safeCandidate)).branch);
  await execFileAsync("git", ["-C", safeCandidate, "push", "--set-upstream", "origin", branch], {
    shell: false,
    timeout: 120_000,
    maxBuffer: MAX_OUTPUT,
    windowsHide: true,
  });
  return repositoryStatus(safeCandidate);
}

export async function removeManagedWorktree(
  managedRoot: string,
  candidate: string,
): Promise<void> {
  const safeCandidate = await resolveManagedWorktree(managedRoot, candidate);
  const commonDirResult = await execFileAsync(
    "git",
    ["-C", safeCandidate, "rev-parse", "--git-common-dir"],
    {
      shell: false,
      timeout: 10_000,
      maxBuffer: MAX_OUTPUT,
      windowsHide: true,
    },
  );
  const commonDir = path.resolve(safeCandidate, commonDirResult.stdout.trim());
  if (path.basename(commonDir) !== ".git")
    throw new Error("managed worktree has an unexpected Git layout");
  const sourceRoot = await resolveDirectory(path.dirname(commonDir));
  await execFileAsync(
    "git",
    ["-C", sourceRoot, "worktree", "remove", "--force", safeCandidate],
    {
      shell: false,
      timeout: 60_000,
      maxBuffer: MAX_OUTPUT,
      windowsHide: true,
    },
  );
}
