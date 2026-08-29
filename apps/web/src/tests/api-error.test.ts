import { describe, expect, it } from "vitest";
import { APIError } from "@forgeflow/api-client";
import { apiErrorMessage } from "../features/app/api";

describe("apiErrorMessage", () => {
  it("explains how to recover an empty repository", () => {
    const error = new APIError("internal server error", 409, {
      code: "REPOSITORY_EMPTY",
      message: "GitHub repository is empty",
      details: { action: "push_initial_commit" },
      request_id: "request-1",
    });

    expect(apiErrorMessage(error)).toContain("chưa có commit");
  });
});
