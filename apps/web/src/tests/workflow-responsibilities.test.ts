import { describe, expect, it } from "vitest";
import { rolesForCapabilities, transitionPermissions } from "../features/work-items/workflow-responsibilities";

describe("workflow responsibilities", () => {
  it("maps transition permissions to project roles", () => {
    expect(transitionPermissions()).toEqual(["work_item.transition"]);
    expect(rolesForCapabilities(["work_item.create"])).toEqual(["owner", "admin", "project_manager", "developer"]);
    expect(rolesForCapabilities(["specification.verify"])).toEqual(["owner", "admin", "project_manager", "qa"]);
  });
});
