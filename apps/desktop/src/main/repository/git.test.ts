import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import { promisify } from "node:util";
import { describe, expect, it } from "vitest";
import { assertForgeflowBranch, assertPushableBranch, assertWithinRoot, branchNameForWorktree, cloneRepository, findLocalRepositories, repositoryDiff, validateBranchName, validateCommitMessage, validateWorktreeName } from "./git";

const execFileAsync = promisify(execFile);

describe("managed worktree paths", () => {
  it("keeps paths inside the root", () => {
    expect(assertWithinRoot("/tmp/worktrees", "/tmp/worktrees/task")).toBe(
      "/tmp/worktrees/task",
    );
    expect(() =>
      assertWithinRoot("/tmp/worktrees", "/tmp/worktrees/../secrets"),
    ).toThrow();
  });

  it("accepts only bounded worktree names", () => {
    expect(validateWorktreeName("task-123")).toBe("task-123");
    expect(() => validateWorktreeName("../secrets")).toThrow();
    expect(() => validateWorktreeName("task/name")).toThrow();
  });

  it("keeps branches safe and blocks protected branches", () => {
    expect(validateBranchName("forgeflow/task-123")).toBe("forgeflow/task-123");
    expect(branchNameForWorktree("task-123")).toBe("forgeflow/task-123");
    expect(assertForgeflowBranch("forgeflow/task-123")).toBe("forgeflow/task-123");
    expect(assertPushableBranch("forgeflow/task-123")).toBe("forgeflow/task-123");
    expect(() => validateBranchName("../secrets")).toThrow();
    expect(() => assertForgeflowBranch("feature/task-123")).toThrow();
    expect(() => assertPushableBranch("main")).toThrow();
  });

  it("enforces the commit message rule", () => {
    expect(validateCommitMessage("fix: handle an empty invoice")).toBe("fix: handle an empty invoice");
    expect(() => validateCommitMessage("Implement approved change")).toThrow();
  });

  it("finds repositories directly inside a local code folder", async () => {
    const root = await fs.mkdtemp(`${os.tmpdir()}/forgeflow-projects-`);
    const repository = `${root}/billing-service`;
    try {
      await fs.mkdir(repository);
      await execFileAsync("git", ["-C", repository, "init", "-q"]);
      const found = await findLocalRepositories(root);
      expect(found.map((item) => item.root)).toEqual([await fs.realpath(repository)]);
    } finally {
      await fs.rm(root, { recursive: true, force: true });
    }
  });

  it("rejects clone credentials with an embedded URL token", async () => {
    const root = await fs.mkdtemp(`${os.tmpdir()}/forgeflow-clone-`);
    try {
      await expect(cloneRepository(root, "app", {
        cloneURL: "https://x-access-token:secret@github.com/acme/app.git",
        username: "x-access-token",
        token: "secret",
      })).rejects.toThrow(/clone URL is invalid/);
    } finally {
      await fs.rm(root, { recursive: true, force: true });
    }
  });

  it("rejects malformed credentials before invoking Git", async () => {
    const root = await fs.mkdtemp(`${os.tmpdir()}/forgeflow-clone-`);
    try {
      await expect(cloneRepository(root, "app", {
        cloneURL: "https://github.com/acme/app.git",
        username: undefined as never,
        token: "secret",
      })).rejects.toThrow(/credential is invalid/);
    } finally {
      await fs.rm(root, { recursive: true, force: true });
    }
  });

  it("includes untracked files in the review diff", async () => {
    const root = await fs.mkdtemp(`${os.tmpdir()}/forgeflow-diff-`);
    try {
      await execFileAsync("git", ["-C", root, "init", "-q"]);
      await execFileAsync("git", ["-C", root, "config", "user.email", "test@forgeflow.local"]);
      await execFileAsync("git", ["-C", root, "config", "user.name", "Forgeflow Test"]);
      await fs.writeFile(`${root}/tracked.txt`, "base\n");
      await execFileAsync("git", ["-C", root, "add", "tracked.txt"]);
      await execFileAsync("git", ["-C", root, "commit", "-qm", "base"]);
      await fs.writeFile(`${root}/new-file.txt`, "new content\n");

      const diff = await repositoryDiff(root);
      expect(diff.files).toContain("new-file.txt");
      expect(diff.patch).toContain("+++ b/new-file.txt");
      expect(diff.patch).toContain("+new content");
    } finally {
      await fs.rm(root, { recursive: true, force: true });
    }
  });
});
