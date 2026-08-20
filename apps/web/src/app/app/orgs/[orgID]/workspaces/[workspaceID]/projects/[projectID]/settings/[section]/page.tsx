import { ProjectSettingsPage } from "@/features/settings/project-settings-page";

type Props = { params: Promise<{ orgID: string; workspaceID: string; projectID: string; section: string }> };

export default async function ProjectSettingsRoute({ params }: Props) {
  const { orgID, workspaceID, projectID, section } = await params;
  const basePath = `/app/orgs/${orgID}/workspaces/${workspaceID}/projects/${projectID}`;
  return <ProjectSettingsPage projectID={projectID} basePath={basePath} section={section} />;
}
