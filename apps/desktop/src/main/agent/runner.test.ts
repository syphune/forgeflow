import { describe, expect, it } from "vitest";
import { allowlistedExecutable, cancelAgent, redact, runAgent } from "./runner";

describe("agent output safety", () => {
  it("only accepts provider-matching executables", () => {
    expect(allowlistedExecutable("codex", "/opt/bin/codex")).toBe("/opt/bin/codex");
    expect(() => allowlistedExecutable("claude", "/opt/bin/codex")).toThrow(/allowlisted/);
  });

  it("redacts common secret-shaped agent output", () => {
    const output = redact("Authorization: Bearer abc token=def password: ghi");
    expect(output).toBe(
      "Authorization: Bearer [REDACTED] token=[REDACTED] password: [REDACTED]",
    );
  });

  it("rejects malformed or unapproved requests before touching the filesystem", async () => {
    await expect(
      runAgent(
        { provider: "codex", worktree: "", prompt: "", approved: false },
        "/does-not-exist",
      ),
    ).rejects.toThrow();
  });

  it("reports cancellation for only an active run", () => {
    expect(cancelAgent("missing-run")).toBe(false);
  });
});
