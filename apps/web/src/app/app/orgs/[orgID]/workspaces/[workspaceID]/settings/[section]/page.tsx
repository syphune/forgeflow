import { WorkspaceSettingsPage } from "@/features/settings/tenant-settings-page";

export default async function WorkspaceSettingsRoute({ params }: { params: Promise<{ orgID: string; workspaceID: string; section: string }> }) {
  const { orgID, workspaceID, section } = await params;
  return <WorkspaceSettingsPage organizationID={orgID} workspaceID={workspaceID} section={section} />;
}
