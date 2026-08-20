import { describe, expect, it } from "vitest";
import { readinessLabel, statusLabel, translate } from "@forgeflow/ui";
import { uiTone } from "../features/app/types";

describe("statusLabel", () => {
  it("renders workflow keys for people", () => {
    expect(statusLabel("REVIEW_REQUIRED")).toBe("Chờ duyệt");
  });
});

describe("translate", () => {
  it("supports the Vietnamese default and English fallback catalog", () => {
    expect(translate("nav.menu", {}, "vi")).toBe("Trình đơn");
    expect(translate("nav.menu", {}, "en")).toBe("Menu");
    expect(translate("work.gaps", { count: 2 }, "vi")).toBe("2 thiếu sót");
  });
});

describe("readinessLabel", () => {
  it("localizes readiness gaps instead of exposing server keys", () => {
    expect(readinessLabel("HUMAN_VERIFIED_PROBLEM_STATEMENT", "vi")).toBe("Cần xác minh thủ công: Mô tả vấn đề.");
    expect(readinessLabel("HUMAN_VERIFIED_ACCEPTANCE_CRITERION_1", "en")).toBe("Acceptance criterion 1: human verification required.");
    expect(readinessLabel("REPRODUCTION_STEP_2_EXPECTED_RESULT", "vi")).toBe("Bước 2: cần bổ sung Kết quả mong đợi.");
  });
});

describe("uiTone", () => {
  it("keeps workflow and priority states visually semantic", () => {
    expect(uiTone("DONE")).toBe("success");
    expect(uiTone("HIGHEST")).toBe("danger");
    expect(uiTone("IN_PROGRESS")).toBe("warning");
    expect(uiTone("TASK")).toBe("info");
    expect(uiTone("custom_status")).toBe("neutral");
  });
});
