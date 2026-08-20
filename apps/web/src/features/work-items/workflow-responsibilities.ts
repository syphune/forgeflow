const allCapabilities = [
  "organization.read",
  "organization.manage",
  "workspace.read",
  "workspace.manage",
  "project.read",
  "project.manage",
  "work_item.create",
  "work_item.edit",
  "work_item.assign",
  "work_item.transition",
  "work_item.delete",
  "comment.create",
  "sprint.manage",
  "repository.read",
  "repository.manage",
  "specification.propose",
  "specification.verify",
  "agent.execute",
  "agent.approve",
  "audit.read",
] as const;

const roleCapabilities: Record<string, readonly string[]> = {
  owner: allCapabilities,
  admin: allCapabilities.filter((capability) => capability !== "organization.manage"),
  project_manager: [
    "work_item.create",
    "work_item.edit",
    "work_item.assign",
    "work_item.transition",
    "specification.verify",
    "agent.execute",
    "agent.approve",
    "project.manage",
  ],
  developer: [
    "work_item.create",
    "work_item.edit",
    "work_item.assign",
    "work_item.transition",
    "agent.execute",
  ],
  qa: ["work_item.edit", "work_item.transition", "specification.verify"],
  viewer: [],
};

export function transitionPermissions(requiredPermissions?: readonly string[]): string[] {
  const normalized = (requiredPermissions ?? []).map((permission) => permission.trim()).filter(Boolean);
  return normalized.length ? normalized : ["work_item.transition"];
}

export function rolesForCapabilities(capabilities: readonly string[]): string[] {
  const required = [...new Set(capabilities.map((capability) => capability.trim()).filter(Boolean))];
  return Object.keys(roleCapabilities).filter((role) => required.every((capability) => roleCapabilities[role].includes(capability)));
}
