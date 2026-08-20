import { describe, expect, it } from "vitest";
import { optimizeAttachment } from "../app/attachment-optimization";

describe("attachment optimization", () => {
  it("keeps non-image evidence byte-for-byte unchanged", async () => {
    const file = new Blob(["trace"], { type: "text/plain" }) as File;
    const prepared = await optimizeAttachment(file);

    expect(prepared.file).toBe(file);
    expect(prepared.optimized).toBe(false);
    expect(prepared.originalSize).toBe(file.size);
  });
});
