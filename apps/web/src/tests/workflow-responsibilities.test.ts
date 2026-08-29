import { describe, expect, it } from "vitest";
import type { WorkflowResponse } from "@forgeflow/api-client";
import { specificationWorkflowPhase } from "../features/work-items/specification-editor";
import { rolesForCapabilities, transitionPermissions } from "../features/work-items/workflow-responsibilities";

describe("workflow responsibilities", () => {
  it("maps transition permissions to project roles", () => {
    expect(transitionPermissions()).toEqual(["work_item.transition"]);
    expect(rolesForCapabilities(["work_item.create"])).toEqual(["owner", "admin", "project_manager", "developer"]);
    expect(rolesForCapabilities(["specification.verify"])).toEqual(["owner", "admin", "project_manager", "qa"]);
  });

  it("derives specification phases from a custom workflow", () => {
    const workflow = {
      id: "custom",
      name: "Custom",
      statuses: [
        { key: "RAW", display_name: "Inbox", category: "TODO", position: 10, is_terminal: false },
        { key: "ANALYSIS", display_name: "Analysis", category: "TODO", position: 20, is_terminal: false },
        { key: "REVIEWING", display_name: "Reviewing", category: "TODO", position: 30, is_terminal: false },
        { key: "READY", display_name: "Ready", category: "TODO", position: 40, is_terminal: false },
      ],
      transitions: [
        { key: "begin", from_status: "RAW", to_status: "ANALYSIS", display_name: "Begin", required_rules: [], required_permissions: [] },
        { key: "submit", from_status: "ANALYSIS", to_status: "REVIEWING", display_name: "Submit", required_rules: [], required_permissions: [] },
        { key: "approve", from_status: "REVIEWING", to_status: "READY", display_name: "Approve", required_rules: ["require_specification_ready"], required_permissions: [] },
      ],
    } as WorkflowResponse;
    expect(specificationWorkflowPhase("RAW", workflow)).toBe("RAW");
    expect(specificationWorkflowPhase("ANALYSIS", workflow)).toBe("REFINING");
    expect(specificationWorkflowPhase("REVIEWING", workflow)).toBe("REVIEW");
    expect(specificationWorkflowPhase("READY", workflow)).toBe("LOCKED");
  });
});
